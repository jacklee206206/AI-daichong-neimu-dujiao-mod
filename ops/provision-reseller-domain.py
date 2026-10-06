#!/usr/bin/env python3
"""Provision one verified reseller host. Read-only/dry-run unless --apply.
Run on a Linux host after the Go application and database migration are installed.
No database writes, DNS provider credentials, API tokens or shell interpolation.
"""
from __future__ import annotations
import argparse
from contextlib import contextmanager
import fcntl
import ipaddress
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import socket
import sqlite3
import stat
import subprocess
import tempfile
import time
import uuid
from datetime import datetime, timezone

PROBE = '/.well-known/opengpt-reseller-verification'
MARKER = '# Managed by dujiao reseller domain provisioner v1\n'
WEBROOT = Path('/var/www/letsencrypt')


def assert_transport_ready(row: dict) -> None:
    mode = row.get('connect_mode') or ''
    if mode in ('', 'legacy', 'direct'):
        return
    if (mode != 'cloudflare_saas'
            or not re.fullmatch(r'[0-9a-fA-F-]{32,36}', row.get('cloudflare_hostname_id') or '')
            or row.get('cloudflare_hostname_status') != 'active'
            or row.get('cloudflare_ssl_status') != 'active'):
        raise ValueError('统一入口的域名及边缘证书仍未就绪，暂不申请源站证书。')


def valid_host(host: str) -> bool:
    if len(host) > 253 or '.' not in host:
        return False
    try:
        ipaddress.ip_address(host)
        return False
    except ValueError:
        pass
    return all(re.fullmatch(r'[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?', label) for label in host.split('.'))



@contextmanager
def readonly_db(db_path: Path):
    """Read as the DB file owner, including any SQLite WAL/SHM side effects."""
    info = db_path.lstat()
    if not stat.S_ISREG(info.st_mode):
        raise ValueError('Database must be an existing regular file, not a symlink.')
    old_uid, old_gid = os.geteuid(), os.getegid()
    changed = old_uid == 0 and info.st_uid != 0
    db = None
    try:
        if changed:
            os.setegid(info.st_gid)
            os.seteuid(info.st_uid)
        db = sqlite3.connect(db_path.resolve().as_uri() + '?mode=ro', uri=True, timeout=15)
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


def load_domain(db_path: Path, domain_id: int) -> dict:
    with readonly_db(db_path) as db:
        db.row_factory = sqlite3.Row
        row = db.execute('''SELECT d.*,p.status profile_status
            FROM reseller_domains d JOIN reseller_profiles p ON p.id=d.reseller_id
            WHERE d.id=? AND d.deleted_at IS NULL AND p.deleted_at IS NULL''', (domain_id,)).fetchone()
    if row is None:
        raise ValueError('域名申请不存在。')
    result = dict(row)
    if result['type'] != 'custom' or result['status'] not in ('pending_review', 'active') or result['profile_status'] != 'active':
        raise ValueError('仅处理已审核代理的待接入/已启用自定义域名。')
    if result['verification_status'] != 'verified':
        raise ValueError('请先在代理中心检测通过 TXT 与网站 DNS。')
    if not valid_host(result['domain']) or not re.fullmatch(r'opengpt-[0-9a-f]{48}', result['verification_token']):
        raise ValueError('域名或验证值格式不安全，拒绝生成配置。')
    assert_transport_ready(result)
    return result


def public_addresses(host: str) -> list[str]:
    addresses = {item[4][0] for item in socket.getaddrinfo(host, 80, type=socket.SOCK_STREAM)}
    if not addresses or len(addresses) > 16 or any(not ipaddress.ip_address(ip).is_global for ip in addresses):
        raise ValueError('域名必须仅解析到公网地址。')
    return sorted(addresses)


def check_dns(row: dict, entry_ip: str) -> None:
    assert_transport_ready(row)
    addresses = {item[4][0] for item in socket.getaddrinfo(row['domain'], 443, type=socket.SOCK_STREAM)}
    if not addresses or any(not ipaddress.ip_address(ip).is_global for ip in addresses):
        raise ValueError('域名必须仅解析到公网地址。')
    if row.get('connect_mode') != 'cloudflare_saas' and addresses != {entry_ip}:
        raise ValueError('DNS 尚未全部指向入口 IP。首次接入请仅 DNS/灰色云朵，并移除其他 A、AAAA。')
    result = subprocess.run(['dig', '+short', '+time=4', '+tries=1', 'TXT', '_opengpt-verification.' + row['domain']], check=True, text=True, capture_output=True, timeout=8)
    records = [''.join(shlex.split(line)) for line in result.stdout.splitlines() if line.strip()]
    if row['verification_token'] not in records:
        raise ValueError('公网 TXT 所有权验证未通过。')


def prepare_webroot() -> Path:
    # The timer uses umask 077. Nginx still needs traverse/read permission for
    # public ACME tokens, without making any directory writable by its uid.
    challenge_dir = WEBROOT / '.well-known' / 'acme-challenge'
    for directory in (WEBROOT, WEBROOT / '.well-known', challenge_dir):
        try:
            directory.mkdir(mode=0o755)
            directory.chmod(0o755)
        except FileExistsError:
            pass
        info = directory.lstat()
        if (not stat.S_ISDIR(info.st_mode) or info.st_uid != 0
                or info.st_mode & 0o022 or info.st_mode & 0o005 != 0o005):
            raise ValueError('ACME 目录必须由 root 管理并允许 Nginx 读取。')
    return challenge_dir


def wait_for_probe(command: list[str], expected: str, error: str, *, strip: bool = False) -> None:
    """Reload is asynchronous; accept only a matching response from the new vhost.

    Nginx may still serve its old/default vhost immediately after systemctl
    reload exits. A short bounded retry also covers old Cloudflare keepalives.
    Redirect, certificate and content checks remain enforced on every attempt.
    """
    for attempt in range(4):
        if attempt:
            time.sleep(1)
        try:
            result = subprocess.run(command, check=True, capture_output=True, text=True, timeout=15)
            actual = result.stdout.strip() if strip else result.stdout
            if actual == expected:
                return
        except (subprocess.CalledProcessError, subprocess.TimeoutExpired):
            pass
    raise ValueError(error)


def check_public_http_challenge(host: str, challenge_dir: Path) -> None:
    """Prove HTTP reaches this exact host before spending a CA request.

    Pin only prevalidated public addresses and never follow redirects. This also
    detects an edge HTTPS redirect/WAF/DCV interception without downgrading TLS.
    The fresh opaque marker is not the ownership token and is removed on failure.
    """
    name = 'dujiao-preflight-' + uuid.uuid4().hex
    body = uuid.uuid4().hex
    marker = challenge_dir / name
    fd = os.open(marker, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o644)
    try:
        os.fchmod(fd, 0o644)
        with os.fdopen(fd, 'w') as file:
            file.write(body)
        # Some origins have IPv4 egress only, even when Cloudflare publishes
        # AAAA records. Validate every resolved IP, then probe one IPv4 first.
        addresses = public_addresses(host)
        addresses.sort(key=lambda item: ipaddress.ip_address(item).version)
        for address in addresses[:1]:
            pinned = '[' + address + ']' if ':' in address else address
            wait_for_probe([
                'curl', '--silent', '--show-error', '--fail', '--max-time', '12',
                '--noproxy', '*', '--proto', '=http', '--max-redirs', '0',
                '--max-filesize', '4096', '--resolve', host + ':80:' + pinned,
                '--write-out', '\n%{http_code}',
                'http://' + host + '/.well-known/acme-challenge/' + name,
            ], body + '\n200', '公网 HTTP 证书验证路径无法直达源站；请检查 HTTPS 重定向、缓存或防火墙规则。')
    finally:
        marker.unlink(missing_ok=True)


def nginx_config(host: str, token: str, tls: bool) -> str:
    if not valid_host(host) or not re.fullmatch(r'opengpt-[0-9a-f]{48}', token):
        raise ValueError('Invalid host/token')
    config = MARKER + f'''server {{
    listen 80;
    listen [::]:80;
    server_name {host};
    if ($host != {host}) {{ return 421; }}
    server_tokens off;
    access_log /var/log/nginx/dujiao-reseller-access.log dujiao_safe;
    location ^~ /.well-known/acme-challenge/ {{ root /var/www/letsencrypt; }}
    location = {PROBE} {{ default_type text/plain; return 200 "{token}"; }}
    location / {{ return {'301 https://' + host + '$request_uri' if tls else '404'}; }}
}}
'''
    if tls:
        config += f'''server {{
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name {host};
    if ($host != {host}) {{ return 421; }}
    ssl_certificate /etc/letsencrypt/live/dujiao-reseller-{host}/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/dujiao-reseller-{host}/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    server_tokens off;
    access_log /var/log/nginx/dujiao-reseller-access.log dujiao_safe;
    client_max_body_size 50m;
    add_header X-Content-Type-Options nosniff always;
    add_header Referrer-Policy strict-origin-when-cross-origin always;
    add_header X-Robots-Tag "noindex, nofollow" always;
    location = {PROBE} {{ default_type text/plain; return 200 "{token}"; }}
    location = /admin {{ return 404; }}
    location ^~ /admin/ {{ return 404; }}
    location ^~ /api/v1/admin/ {{ return 404; }}
    location = /api/v1/admin {{ return 404; }}
    location / {{
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 120s;
    }}
}}
'''
    return config


def atomic_write(path: Path, content: str) -> None:
    fd, tmp = tempfile.mkstemp(prefix=path.name + '.', dir=path.parent)
    try:
        with os.fdopen(fd, 'w') as file:
            file.write(content)
            file.flush()
            os.fsync(file.fileno())
        os.chmod(tmp, 0o644)
        os.replace(tmp, path)
    finally:
        if os.path.exists(tmp):
            os.unlink(tmp)


def assert_expected_claim(db_path: Path, expected: dict) -> None:
    """Fail closed if an automatic job was revoked, rebound or reclaimed."""
    with readonly_db(db_path) as db:
        db.row_factory = sqlite3.Row
        row = db.execute("""SELECT d.* FROM reseller_domains d
            JOIN reseller_profiles p ON p.id=d.reseller_id
            WHERE d.id=? AND d.domain=? AND d.verification_token=? AND d.reseller_id=?
              AND julianday(d.auto_connect_last_attempt_at)=julianday(?)
              AND julianday(d.auto_connect_requested_at)=julianday(?) AND d.auto_connect_attempts=?
              AND d.type='custom' AND d.status='pending_review'
              AND d.verification_status='verified' AND d.tls_status='provisioning'
              AND d.deleted_at IS NULL AND p.deleted_at IS NULL AND p.status='active'""",
            (expected['id'], expected['domain'], expected['verification_token'], expected['reseller_id'],
             expected['auto_connect_last_attempt_at'], expected['auto_connect_requested_at'],
             expected['auto_connect_attempts'])).fetchone()
    if row is None:
        raise ValueError('自动接入申请已变化或撤销，本次签证已停止。')
    actual = dict(row)
    if ((actual.get('connect_mode') or '') != (expected.get('connect_mode') or '')
            or (actual.get('cloudflare_hostname_id') or '') != (expected.get('cloudflare_hostname_id') or '')):
        raise ValueError('接入方式或边缘域名绑定已变化，本次签证已停止。')
    assert_transport_ready(actual)


def apply(row: dict, entry_ip: str, email: str, guard=None) -> None:
    if os.geteuid() != 0:
        raise ValueError('--apply 需要在服务器以 sudo 执行。')
    for command in ('nginx', 'certbot', 'dig', 'systemctl', 'curl'):
        if not shutil.which(command):
            raise ValueError('缺少系统工具: ' + command)
    host, token = row['domain'], row['verification_token']
    available = Path('/etc/nginx/sites-available') / ('dujiao-reseller-' + host)
    enabled = Path('/etc/nginx/sites-enabled') / available.name
    before = available.read_text() if available.exists() else None
    if before is not None and not before.startswith(MARKER):
        raise ValueError('存在非本工具创建的同名配置，拒绝覆盖。')
    if enabled.is_symlink() and enabled.resolve() != available:
        raise ValueError('已有不匹配的启用配置，拒绝覆盖。')
    if enabled.exists() and not enabled.is_symlink():
        raise ValueError('已有非符号链接的启用配置，拒绝覆盖。')
    for file in Path('/etc/nginx/sites-enabled').iterdir():
        if file == enabled or not file.is_file():
            continue
        content = file.read_text()
        for value in re.findall(r'\bserver_name\s+([^;]+);', content):
            if host in value.split():
                raise ValueError('其他 Nginx 站点已占用该域名，拒绝覆盖。')
    if guard is not None:
        guard()
    check_dns(row, entry_ip)
    snapshot = Path('/var/backups/dujiao') / ('domain-' + host + '-' + datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ'))
    snapshot.mkdir(parents=True, mode=0o700)
    if before is not None:
        (snapshot / 'nginx-before.conf').write_text(before)
        os.chmod(snapshot / 'nginx-before.conf', 0o600)
    challenge_dir = prepare_webroot()
    was_enabled = enabled.is_symlink()
    try:
        # Existing valid vhosts retain TLS during renewal. New hosts get only ACME/probe HTTP.
        if before is None:
            atomic_write(available, nginx_config(host, token, False))
        if not was_enabled:
            enabled.symlink_to(available)
        subprocess.run(['nginx', '-t'], check=True)
        subprocess.run(['systemctl', 'reload', 'nginx'], check=True)
        if guard is not None:
            guard()
        check_public_http_challenge(host, challenge_dir)
        if guard is not None:
            guard()
        subprocess.run(['certbot', 'certonly', '--webroot', '-w', '/var/www/letsencrypt', '--cert-name', 'dujiao-reseller-' + host,
                        '-d', host, '--non-interactive', '--agree-tos', '--email', email, '--keep-until-expiring'], check=True, timeout=180)
        if guard is not None:
            guard()
        atomic_write(available, nginx_config(host, token, True))
        subprocess.run(['nginx', '-t'], check=True)
        subprocess.run(['systemctl', 'reload', 'nginx'], check=True)
        # Normal CA and hostname verification; never curl -k. DNS cannot choose another endpoint.
        wait_for_probe(['curl', '--silent', '--show-error', '--fail', '--max-time', '10', '--noproxy', '*',
                        '--resolve', host + ':443:' + entry_ip, 'https://' + host + PROBE], token,
                       'HTTPS 探针不匹配或源站证书尚未就绪。', strip=True)
        subprocess.run(['systemctl', 'enable', '--now', 'certbot.timer'], check=True)
    except Exception:
        if before is None:
            enabled.unlink(missing_ok=True)
            available.unlink(missing_ok=True)
        else:
            atomic_write(available, before)
            if not was_enabled:
                enabled.unlink(missing_ok=True)
        subprocess.run(['nginx', '-t'], check=True)
        subprocess.run(['systemctl', 'reload', 'nginx'], check=True)
        raise
    print(json.dumps({'domain': host, 'https_verified': True, 'database_changed': False,
                      'next_step': '源站证书已签发；后端仍须核验域名归属、边缘状态和公网店铺探针才能启用。',
                      'backup': str(snapshot)}, ensure_ascii=False))


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--domain-id', type=int, required=True)
    parser.add_argument('--db', type=Path, default=Path('/opt/dujiao-next/db/dujiao.db'))
    parser.add_argument('--entry-ip', required=True, help='Platform origin public IPv4')
    parser.add_argument('--email', required=True, help='证书续期通知收件地址')
    parser.add_argument('--apply', action='store_true')
    parser.add_argument('--expected-domain')
    parser.add_argument('--expected-token')
    parser.add_argument('--expected-reseller-id', type=int)
    parser.add_argument('--expected-attempt-at')
    parser.add_argument('--expected-attempts', type=int)
    parser.add_argument('--expected-requested-at')
    parser.add_argument('--expected-connect-mode', default='')
    parser.add_argument('--expected-cloudflare-hostname-id', default='')
    args = parser.parse_args()
    ip = ipaddress.ip_address(args.entry_ip)
    if ip.version != 4 or not ip.is_global or args.domain_id <= 0 or not re.fullmatch(r'[^\s@]+@[^\s@]+\.[^\s@]+', args.email):
        raise ValueError('域名 ID、公网 IPv4 或通知邮箱无效。')
    row = load_domain(args.db, args.domain_id)
    expected_values = [args.expected_domain, args.expected_token, args.expected_reseller_id,
                       args.expected_attempt_at, args.expected_requested_at, args.expected_attempts]
    expected = None
    if any(value is not None for value in expected_values):
        if not all(value is not None and value != '' for value in expected_values):
            raise ValueError('自动签证必须提供完整的队列领取标识。')
        if not 1 <= args.expected_attempts <= 5:
            raise ValueError('自动签证尝试次数不在有效范围内。')
        expected = dict(id=args.domain_id, domain=args.expected_domain, verification_token=args.expected_token,
                        reseller_id=args.expected_reseller_id, auto_connect_last_attempt_at=args.expected_attempt_at,
                        connect_mode=args.expected_connect_mode, cloudflare_hostname_id=args.expected_cloudflare_hostname_id,
                        auto_connect_requested_at=args.expected_requested_at, auto_connect_attempts=args.expected_attempts)
        assert_expected_claim(args.db, expected)
        if row['domain'] != expected['domain'] or row['verification_token'] != expected['verification_token']:
            raise ValueError('申请域名或验证值已经变化。')
    if not args.apply:
        print(json.dumps({'mode': 'dry-run', 'domain': row['domain'], 'entry_ip': args.entry_ip,
                          'needs': 'live DNS check, Certbot issuance, HTTPS probe and admin approval',
                          'database_changed': False, 'nginx_config': nginx_config(row['domain'], row['verification_token'], True)}, ensure_ascii=False, indent=2))
        return
    # Serialize provisioning processes; DB remains exclusively owned by the Go application.
    with open('/run/lock/dujiao-reseller-domain.lock', 'w') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        apply(row, args.entry_ip, args.email,
              (lambda: assert_expected_claim(args.db, expected)) if expected is not None else None)

if __name__ == '__main__':
    main()
