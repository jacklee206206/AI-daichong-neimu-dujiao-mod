#!/usr/bin/env python3
"""Process explicit, DNS-verified reseller TLS requests; default is read-only preview.

Only --apply writes the queue and invokes the root certificate provisioner. A
successful certificate is marked provisioned, never active/ready: the Go service
must independently verify trusted TLS and the ownership probe before activation.
No DNS-provider credentials, payment tables or API keys are read by this tool.
"""
from __future__ import annotations

import argparse
from contextlib import contextmanager
from datetime import datetime, timezone
import fcntl
import ipaddress
import json
import logging
from logging.handlers import RotatingFileHandler
import os
from pathlib import Path
import re
import signal
import sqlite3
import stat
import subprocess
import sys
import tempfile

DEFAULT_DB = Path('/opt/dujiao-next/db/dujiao.db')
DEFAULT_PROVISIONER = Path('/usr/local/lib/dujiao-next/provision-reseller-domain.py')
DEFAULT_LOG = Path('/var/log/dujiao-next/reseller-auto-provision.log')
LOCK = Path('/run/lock/dujiao-reseller-auto-provision.lock')
MAX_ATTEMPTS = 5
MAX_BATCH = 4
COOLDOWN_SECONDS = 300
STALE_SECONDS = 900
CHILD_TIMEOUT_SECONDS = 300
FAILURES = {
    'failed': '证书签发未成功；系统将按重试间隔再次尝试，达到 5 次后请联系管理员检查后重试。',
    'timeout': '证书签发超时；系统将按重试间隔再次尝试。',
    'interrupted': '上次证书签发进程中断，已恢复为可重试状态。',
    'invalid': '域名或验证信息不符合签发要求，请重新检测后申请接入。',
}
ELIGIBLE = """d.type='custom' AND d.status='pending_review'
 AND d.verification_status='verified' AND d.auto_connect_requested_at IS NOT NULL
 AND julianday(d.auto_connect_requested_at) IS NOT NULL
 AND d.deleted_at IS NULL AND EXISTS (
  SELECT 1 FROM reseller_profiles p WHERE p.id=d.reseller_id
  AND p.status='active' AND p.deleted_at IS NULL)
 AND (COALESCE(d.connect_mode,'') IN ('','legacy','direct') OR
      (d.connect_mode='cloudflare_saas' AND COALESCE(d.cloudflare_hostname_id,'')<>''
       AND d.cloudflare_hostname_status='active' AND d.cloudflare_ssl_status='active'))"""
CLAIMABLE = ELIGIBLE + """
 AND COALESCE(d.tls_status,'') IN ('','pending','failed')
 AND COALESCE(d.auto_connect_attempts,0) < :max_attempts
 AND (d.auto_connect_last_attempt_at IS NULL
      OR julianday(d.auto_connect_last_attempt_at) <= julianday(:now) - :cooldown / 86400.0)"""
IDENTITY = """d.id=:id AND d.domain=:domain AND d.reseller_id=:reseller_id
 AND d.verification_token=:verification_token
 AND COALESCE(d.connect_mode,'')=:connect_mode
 AND COALESCE(d.cloudflare_hostname_id,'')=:cloudflare_hostname_id
 AND julianday(d.auto_connect_requested_at)=julianday(:auto_connect_requested_at)
 AND julianday(d.auto_connect_last_attempt_at)=julianday(:auto_connect_last_attempt_at)
 AND d.auto_connect_attempts=:auto_connect_attempts"""


def timestamp() -> str:
    # Both SQLite julianday() and GORM's SQLite datetime scanner accept this.
    return datetime.now(timezone.utc).strftime('%Y-%m-%d %H:%M:%S.%f+00:00')


def valid_identity(row: dict) -> bool:
    host, token = row.get('domain', ''), row.get('verification_token', '')
    if not isinstance(host, str) or not isinstance(token, str) or len(host) > 253 or '.' not in host:
        return False
    try:
        ipaddress.ip_address(host)
        return False
    except ValueError:
        pass
    mode = row.get('connect_mode') or ''
    if mode not in ('', 'legacy', 'direct', 'cloudflare_saas'):
        return False
    if mode == 'cloudflare_saas' and not re.fullmatch(r'[0-9a-fA-F-]{32,36}', row.get('cloudflare_hostname_id') or ''):
        return False
    return bool(all(re.fullmatch(r'[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?', label)
                    for label in host.split('.'))
                and re.fullmatch(r'opengpt-[0-9a-f]{48}', token)
                and isinstance(row.get('reseller_id'), int) and row['reseller_id'] > 0)


@contextmanager
def connect_db(path: Path, writable: bool = True):
    """Do SQLite I/O as its owner, so WAL/SHM never become root-owned."""
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode):
        raise ValueError('Database must be an existing regular file, not a symlink.')
    old_uid, old_gid = os.geteuid(), os.getegid()
    changed = old_uid == 0 and info.st_uid != 0
    db = None
    try:
        if changed:
            os.setegid(info.st_gid)
            os.seteuid(info.st_uid)
        db = sqlite3.connect(path.resolve().as_uri() + ('?mode=rw' if writable else '?mode=ro'),
                             uri=True, timeout=15)
        db.row_factory = sqlite3.Row
        db.execute('PRAGMA busy_timeout=15000')
        yield db
    finally:
        try:
            if db is not None:
                db.close()
        finally:
            if changed:
                os.seteuid(old_uid)
                os.setegid(old_gid)


def candidates(db: sqlite3.Connection, now: str) -> list[dict]:
    return [dict(dict(row), connect_mode=row['connect_mode'] or '',
                 cloudflare_hostname_id=row['cloudflare_hostname_id'] or '') for row in db.execute(f"""SELECT d.* FROM reseller_domains d
        WHERE {CLAIMABLE} ORDER BY julianday(d.auto_connect_requested_at), d.id LIMIT :limit""",
        dict(now=now, max_attempts=MAX_ATTEMPTS, cooldown=COOLDOWN_SECONDS, limit=MAX_BATCH))]


def recover_stale(db: sqlite3.Connection, now: str) -> int:
    """Atomic recovery; a terminated worker consumes its already-counted attempt."""
    with db:
        return db.execute(f"""UPDATE reseller_domains AS d SET tls_status='failed',
            last_tls_error=:error, updated_at=:now WHERE {ELIGIBLE}
            AND d.tls_status='provisioning'
            AND (julianday(d.auto_connect_last_attempt_at) IS NULL
              OR julianday(d.auto_connect_last_attempt_at) <= julianday(:now) - :stale / 86400.0)""",
            dict(now=now, stale=STALE_SECONDS, error=FAILURES['interrupted'])).rowcount


def claim(db: sqlite3.Connection, now: str) -> dict | None:
    db.execute('BEGIN IMMEDIATE')
    try:
        rows = candidates(db, now)
        if not rows:
            db.commit()
            return None
        row = rows[0]
        params = dict(row, now=now, max_attempts=MAX_ATTEMPTS, cooldown=COOLDOWN_SECONDS,
                      invalid_error=FAILURES['invalid'])
        # Invalid data fails closed and exhausts attempts instead of consuming the
        # first batch slot forever. No host/token ever becomes shell text.
        valid = valid_identity(row)
        result = db.execute(f"""UPDATE reseller_domains AS d SET
            tls_status=:next_status, auto_connect_last_attempt_at=:now,
            auto_connect_attempts=:next_attempts, last_tls_error=:error, updated_at=:now
            WHERE d.id=:id AND d.domain=:domain AND d.reseller_id=:reseller_id
              AND d.verification_token=:verification_token
              AND COALESCE(d.connect_mode,'')=:connect_mode
              AND COALESCE(d.cloudflare_hostname_id,'')=:cloudflare_hostname_id
              AND d.auto_connect_requested_at=:auto_connect_requested_at AND {CLAIMABLE}""",
            dict(params, next_status='provisioning' if valid else 'failed',
                 next_attempts=int(row.get('auto_connect_attempts') or 0) + 1 if valid else MAX_ATTEMPTS,
                 error='' if valid else FAILURES['invalid']))
        db.commit()
        if result.rowcount != 1:
            return None
        row['auto_connect_last_attempt_at'] = now
        row['auto_connect_attempts'] = int(row.get('auto_connect_attempts') or 0) + 1 if valid else MAX_ATTEMPTS
        row['valid'] = valid
        return row
    except Exception:
        db.rollback()
        raise


def current_claim(db: sqlite3.Connection, row: dict) -> bool:
    return db.execute(f"SELECT 1 FROM reseller_domains d WHERE {IDENTITY} AND {ELIGIBLE}"
                      " AND d.tls_status='provisioning'", row).fetchone() is not None


def finish(db: sqlite3.Connection, row: dict, outcome: str, now: str) -> bool:
    if outcome not in ('provisioned', 'failed', 'timeout'):
        raise ValueError('Unexpected job outcome')
    with db:
        return db.execute(f"""UPDATE reseller_domains AS d SET tls_status=:tls_status,
            last_tls_error=:error, updated_at=:now WHERE {IDENTITY} AND {ELIGIBLE}
            AND d.tls_status='provisioning'""",
            dict(row, now=now, tls_status='provisioned' if outcome == 'provisioned' else 'failed',
                 error='' if outcome == 'provisioned' else FAILURES[outcome])).rowcount == 1


def trusted_provisioner(path: Path) -> None:
    # A protected leaf is insufficient: a writable ancestor can be renamed and
    # replaced. Check the lexical path without resolving away any symlinks.
    if not path.is_absolute() or '..' in path.parts:
        raise ValueError('Provisioner requires an absolute path without parent traversal.')
    for index, item in enumerate((path, *path.parents)):
        info = item.lstat()
        if stat.S_ISLNK(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
            raise ValueError('Provisioner and every ancestor must be root-owned and not group/world writable.')
        expected_type = stat.S_ISREG if index == 0 else stat.S_ISDIR
        if not expected_type(info.st_mode):
            raise ValueError('Provisioner must be a regular file under regular directories.')


def private_logger(path: Path) -> logging.Logger:
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    info = path.parent.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o077:
        raise ValueError('Log directory must be root-owned with mode 0700.')
    fd = os.open(path, os.O_APPEND | os.O_CREAT | os.O_WRONLY | os.O_NOFOLLOW, 0o600)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o077:
            raise ValueError('Log file must be root-owned with mode 0600.')
    finally:
        os.close(fd)
    logger = logging.getLogger('dujiao-reseller-auto-provision')
    logger.setLevel(logging.INFO)
    logger.propagate = False
    logger.handlers.clear()
    handler = RotatingFileHandler(path, maxBytes=5 * 1024 * 1024, backupCount=4)
    handler.setFormatter(logging.Formatter('%(asctime)s %(levelname)s %(message)s'))
    logger.addHandler(handler)
    return logger


def command_for(row: dict, provisioner: Path, db: Path, entry_ip: str, email: str) -> list[str]:
    if not valid_identity(row):
        raise ValueError('Unsafe domain identity')
    return [sys.executable, str(provisioner), '--domain-id', str(row['id']), '--db', str(db),
            '--entry-ip', entry_ip, '--email', email, '--expected-domain', row['domain'],
            '--expected-token', row['verification_token'], '--expected-reseller-id', str(row['reseller_id']),
            '--expected-attempt-at', row['auto_connect_last_attempt_at'],
            '--expected-attempts', str(row['auto_connect_attempts']),
            '--expected-connect-mode', row.get('connect_mode') or '',
            '--expected-cloudflare-hostname-id', row.get('cloudflare_hostname_id') or '',
            '--expected-requested-at', row['auto_connect_requested_at'], '--apply']


def run_provisioner(command: list[str], logger: logging.Logger, domain_id: int) -> str:
    # Child output is held in a private, unlinked tempfile and copied only to a
    # root-owned log. Database-visible errors never contain command output.
    with tempfile.TemporaryFile() as output:
        process = subprocess.Popen(command, stdin=subprocess.DEVNULL, stdout=output,
                                   stderr=subprocess.STDOUT, shell=False, start_new_session=True)
        outcome = 'failed'
        try:
            outcome = 'provisioned' if process.wait(timeout=CHILD_TIMEOUT_SECONDS) == 0 else 'failed'
        except subprocess.TimeoutExpired:
            outcome = 'timeout'
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=5)
        output.seek(0, os.SEEK_END)
        output.seek(max(0, output.tell() - 16384))
        detail = output.read().decode('utf-8', errors='replace')
        logger.info('domain_id=%s outcome=%s child_output=%r', domain_id, outcome, detail)
        return outcome


def process_batch(db_path: Path, provisioner: Path, entry_ip: str, email: str,
                  logger: logging.Logger) -> dict:
    summary = dict(recovered=0, claimed=0, provisioned=0, failed=0, skipped=0)
    with connect_db(db_path) as db:
        summary['recovered'] = recover_stale(db, timestamp())
    for _ in range(MAX_BATCH):
        with connect_db(db_path) as db:
            row = claim(db, timestamp())
        if row is None:
            break
        summary['claimed'] += 1
        if not row['valid']:
            summary['failed'] += 1
            logger.warning('domain_id=%s rejected unsafe identity', row['id'])
            continue
        with connect_db(db_path, writable=False) as db:
            eligible = current_claim(db, row)
        if not eligible:
            summary['skipped'] += 1
            continue
        try:
            outcome = run_provisioner(command_for(row, provisioner, db_path, entry_ip, email), logger, row['id'])
        except Exception:
            logger.exception('domain_id=%s provisioner launch failed', row['id'])
            outcome = 'failed'
        with connect_db(db_path) as db:
            saved = finish(db, row, outcome, timestamp())
        if not saved:
            summary['skipped'] += 1
            logger.info('domain_id=%s completion discarded because application changed', row['id'])
        else:
            summary['provisioned' if outcome == 'provisioned' else 'failed'] += 1
    return summary


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--db', type=Path, default=DEFAULT_DB)
    parser.add_argument('--provisioner', type=Path, default=DEFAULT_PROVISIONER)
    parser.add_argument('--entry-ip', required=True, help='Platform origin public IPv4')
    parser.add_argument('--email', required=True, help='Certificate notification email')
    parser.add_argument('--log', type=Path, default=DEFAULT_LOG)
    parser.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    ip = ipaddress.ip_address(args.entry_ip)
    if ip.version != 4 or not ip.is_global or not re.fullmatch(r'[^\s@]+@[^\s@]+\.[^\s@]+', args.email):
        parser.error('A public IPv4 and valid certificate notification email are required.')
    os.umask(0o077)
    if not args.apply:
        with connect_db(args.db, writable=False) as db:
            rows = candidates(db, timestamp())
        print(json.dumps(dict(mode='dry-run', database_changed=False,
                             candidates=[dict(id=row['id'], domain=row['domain'], valid=valid_identity(row)) for row in rows]),
                         ensure_ascii=False))
        return 0
    if os.geteuid() != 0:
        parser.error('--apply must run as root on the server.')
    trusted_provisioner(args.provisioner)
    logger = private_logger(args.log)
    fd = os.open(LOCK, os.O_WRONLY | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as lock:
        if os.fstat(lock.fileno()).st_uid != 0 or not stat.S_ISREG(os.fstat(lock.fileno()).st_mode):
            raise ValueError('Unsafe queue lock file.')
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            logger.info('Another queue worker is already running; skip.')
            return 0
        try:
            summary = process_batch(args.db, args.provisioner, args.entry_ip, args.email, logger)
            logger.info('batch=%s', summary)
            print(json.dumps(summary))
            return 0
        except Exception:
            logger.exception('Queue run failed; details remain in root-only log.')
            print('Reseller certificate queue failed; see root-only operation log.', file=sys.stderr)
            return 1


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except (ValueError, OSError, sqlite3.Error):
        # Fail closed on migration/configuration/permissions errors without leaking
        # paths, database internals, tokens or child diagnostics to the web UI.
        print('Reseller certificate queue configuration is not ready.', file=sys.stderr)
        raise SystemExit(1)
