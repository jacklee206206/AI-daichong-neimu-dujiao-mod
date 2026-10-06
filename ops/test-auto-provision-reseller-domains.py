#!/usr/bin/env python3
"""Offline queue tests: temporary SQLite only, no DNS/ACME or production writes."""
from concurrent.futures import ThreadPoolExecutor
import importlib.util
import json
import logging
from pathlib import Path
import sqlite3
import stat
from types import SimpleNamespace
import subprocess
import tempfile
import unittest
from unittest.mock import Mock, patch

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('auto_provision', HERE / 'auto-provision-reseller-domains.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
provision_spec = importlib.util.spec_from_file_location('provision', HERE / 'provision-reseller-domain.py')
provision = importlib.util.module_from_spec(provision_spec)
provision_spec.loader.exec_module(provision)
TOKEN = 'opengpt-' + 'a' * 48
NOW = '2026-10-01 10:00:00.000000+00:00'
SCHEMA = '''
CREATE TABLE reseller_profiles (id INTEGER PRIMARY KEY, status TEXT, deleted_at TEXT);
CREATE TABLE reseller_domains (
 id INTEGER PRIMARY KEY, reseller_id INTEGER, domain TEXT, type TEXT,
 status TEXT, verification_status TEXT, verification_token TEXT, deleted_at TEXT,
 tls_status TEXT, tls_ready_at TEXT, auto_connect_requested_at TEXT,
 auto_connect_attempts INTEGER DEFAULT 0, auto_connect_last_attempt_at TEXT,
 last_tls_error TEXT, dns_provider TEXT, updated_at TEXT,
 connect_mode TEXT DEFAULT '', cloudflare_hostname_id TEXT DEFAULT '',
 cloudflare_hostname_status TEXT DEFAULT '', cloudflare_ssl_status TEXT DEFAULT ''
);
INSERT INTO reseller_profiles VALUES (1,'active',NULL);
INSERT INTO reseller_profiles VALUES (2,'disabled',NULL);
'''


class QueueTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.path = Path(self.tmp.name) / 'dujiao.db'
        self.db = sqlite3.connect(self.path)
        self.db.row_factory = sqlite3.Row
        self.db.executescript(SCHEMA)
        self.addCleanup(self.db.close)

    def add(self, ident=1, **overrides):
        fields = dict(id=ident, reseller_id=1, domain=f'shop{ident}.example.com', type='custom',
                      status='pending_review', verification_status='verified', verification_token=TOKEN,
                      tls_status='pending', auto_connect_requested_at='2026-10-01T09:00:00Z',
                      auto_connect_attempts=0)
        fields.update(overrides)
        self.db.execute('INSERT INTO reseller_domains (' + ','.join(fields) + ') VALUES (' +
                        ','.join('?' for _ in fields) + ')', tuple(fields.values()))
        self.db.commit()

    def get(self, ident=1):
        return dict(self.db.execute('SELECT * FROM reseller_domains WHERE id=?', (ident,)).fetchone())

    def test_explicit_verified_pending_active_owner_is_required(self):
        variants = [dict(auto_connect_requested_at=None), dict(verification_status='pending'),
                    dict(status='active'), dict(status='rejected'), dict(reseller_id=2),
                    dict(deleted_at=NOW), dict(type='subdomain'), dict(tls_status='ready'),
                    dict(tls_status='provisioned'), dict(tls_status='provisioning'),
                    dict(auto_connect_requested_at='invalid')]
        for ident, variant in enumerate(variants, 1):
            self.add(ident, **variant)
        self.add(99)
        rows = module.candidates(self.db, NOW)
        self.assertEqual([row['id'] for row in rows], [99])
        self.db.execute('UPDATE reseller_profiles SET deleted_at=? WHERE id=1', (NOW,))
        self.db.commit()
        self.assertEqual(module.candidates(self.db, NOW), [])

    def test_claim_increments_once_and_preserves_web_state(self):
        self.add()
        row = module.claim(self.db, NOW)
        self.assertTrue(row['valid'])
        actual = self.get()
        self.assertEqual(actual['tls_status'], 'provisioning')
        self.assertEqual(actual['auto_connect_attempts'], 1)
        self.assertEqual(actual['status'], 'pending_review')
        self.assertIsNone(actual['tls_ready_at'])
        self.assertIsNone(module.claim(self.db, NOW))
        provision.assert_expected_claim(self.path, row)

    def test_saas_waits_for_both_edge_statuses_without_spending_attempts(self):
        common = dict(connect_mode='cloudflare_saas', cloudflare_hostname_id='a' * 32)
        self.add(1, **common, cloudflare_hostname_status='pending', cloudflare_ssl_status='active')
        self.add(2, **common, cloudflare_hostname_status='active', cloudflare_ssl_status='pending_validation')
        self.add(3, **common, cloudflare_hostname_status='active', cloudflare_ssl_status='active')
        self.add(4, connect_mode='unknown')
        self.assertEqual([r['id'] for r in module.candidates(self.db, NOW)], [3])
        row = module.claim(self.db, NOW)
        self.assertEqual(row['id'], 3)
        provision.assert_expected_claim(self.path, row)
        self.assertEqual(self.get(1)['auto_connect_attempts'], 0)
        self.assertEqual(self.get(2)['auto_connect_attempts'], 0)
        self.db.execute("UPDATE reseller_domains SET cloudflare_ssl_status='expired' WHERE id=3")
        self.db.commit()
        self.assertFalse(module.current_claim(self.db, row))
        self.assertFalse(module.finish(self.db, row, 'provisioned', NOW))
        with self.assertRaises(ValueError):
            provision.assert_expected_claim(self.path, row)

    def test_saas_claim_cannot_be_rebound_to_another_edge_hostname(self):
        self.add(connect_mode='cloudflare_saas', cloudflare_hostname_id='a' * 32,
                 cloudflare_hostname_status='active', cloudflare_ssl_status='active')
        row = module.claim(self.db, NOW)
        self.db.execute("UPDATE reseller_domains SET cloudflare_hostname_id=? WHERE id=1", ('b' * 32,))
        self.db.commit()
        self.assertFalse(module.current_claim(self.db, row))
        with self.assertRaises(ValueError):
            provision.assert_expected_claim(self.path, row)

    def test_competing_workers_cannot_claim_same_domain(self):
        self.add()
        def worker():
            with module.connect_db(self.path) as db:
                return module.claim(db, NOW)
        with ThreadPoolExecutor(max_workers=2) as pool:
            rows = list(pool.map(lambda _: worker(), range(2)))
        self.assertEqual(sum(row is not None for row in rows), 1)
        self.assertEqual(self.get()['auto_connect_attempts'], 1)

    def test_cooldown_handles_rfc3339_gorm_and_timezone_offsets(self):
        self.add(1, tls_status='failed', auto_connect_last_attempt_at='2026-10-01T09:55:01Z')
        self.add(2, tls_status='failed', auto_connect_last_attempt_at='2026-10-01 17:55:01.123456789+08:00')
        self.add(3, tls_status='failed', auto_connect_last_attempt_at='2026-10-01 09:54:59.000000+00:00')
        self.add(4, tls_status='failed', auto_connect_attempts=5)
        self.assertEqual([r['id'] for r in module.candidates(self.db, NOW)], [3])

    def test_success_only_marks_provisioned(self):
        self.add()
        row = module.claim(self.db, NOW)
        self.assertTrue(module.finish(self.db, row, 'provisioned', NOW))
        actual = self.get()
        self.assertEqual(actual['tls_status'], 'provisioned')
        self.assertEqual(actual['status'], 'pending_review')
        self.assertIsNone(actual['tls_ready_at'])
        self.assertIsNone(module.claim(self.db, NOW))

    def test_failure_safe_error_and_maximum_retry(self):
        self.add(auto_connect_attempts=4)
        row = module.claim(self.db, NOW)
        self.assertTrue(module.finish(self.db, row, 'failed', NOW))
        self.assertEqual(self.get()['auto_connect_attempts'], 5)
        self.assertEqual(self.get()['last_tls_error'], module.FAILURES['failed'])
        self.assertIsNone(module.claim(self.db, '2026-10-01 11:00:00+00:00'))
        with self.assertRaises(ValueError):
            module.finish(self.db, row, 'secret command diagnostic', NOW)

    def test_completion_and_child_guard_reject_rebind_revoke_or_new_claim(self):
        changes = [dict(verification_token='opengpt-' + 'b' * 48), dict(domain='new.example.com'),
                   dict(reseller_id=2), dict(status='rejected'), dict(verification_status='pending'),
                   dict(auto_connect_requested_at=None), dict(deleted_at=NOW),
                   dict(auto_connect_last_attempt_at='2026-10-01 10:01:00+00:00')]
        for ident, change in enumerate(changes, 1):
            with self.subTest(change=change):
                self.add(ident)
                row = module.claim(self.db, NOW)
                self.db.execute('UPDATE reseller_domains SET ' + ','.join(key + '=?' for key in change) + ' WHERE id=?',
                                (*change.values(), ident))
                self.db.commit()
                self.assertFalse(module.current_claim(self.db, row))
                self.assertFalse(module.finish(self.db, row, 'provisioned', NOW))
                with self.assertRaises(ValueError):
                    provision.assert_expected_claim(self.path, row)

    def test_same_instants_survive_gorm_timestamp_reserialization(self):
        self.add()
        row = module.claim(self.db, NOW)
        self.db.execute("""UPDATE reseller_domains SET auto_connect_last_attempt_at=?,
            auto_connect_requested_at=? WHERE id=1""",
            ('2026-10-01T18:00:00+08:00', '2026-10-01 09:00:00.000000000+00:00'))
        self.db.commit()
        self.assertTrue(module.current_claim(self.db, row))
        provision.assert_expected_claim(self.path, row)
        self.assertTrue(module.finish(self.db, row, 'provisioned', NOW))
        self.assertEqual(self.get()['tls_status'], 'provisioned')

    def test_equal_timestamp_with_new_attempt_is_a_different_claim(self):
        self.add()
        row = module.claim(self.db, NOW)
        self.db.execute("UPDATE reseller_domains SET auto_connect_attempts=2 WHERE id=1")
        self.db.commit()
        self.assertFalse(module.current_claim(self.db, row))
        self.assertFalse(module.finish(self.db, row, 'provisioned', NOW))
        with self.assertRaises(ValueError):
            provision.assert_expected_claim(self.path, row)

    def test_profile_revocation_blocks_finish_and_child_guard(self):
        self.add()
        row = module.claim(self.db, NOW)
        self.db.execute("UPDATE reseller_profiles SET status='disabled' WHERE id=1")
        self.db.commit()
        self.assertFalse(module.finish(self.db, row, 'provisioned', NOW))
        with self.assertRaises(ValueError):
            provision.assert_expected_claim(self.path, row)

    def test_stale_recovery_keeps_attempt_count_and_excludes_inflight(self):
        self.add(1, tls_status='provisioning', auto_connect_attempts=1,
                 auto_connect_last_attempt_at='2026-10-01T09:44:00Z')
        self.add(2, tls_status='provisioning', auto_connect_attempts=1,
                 auto_connect_last_attempt_at='2026-10-01T09:59:00Z')
        self.add(3, tls_status='provisioning', auto_connect_attempts=5,
                 auto_connect_last_attempt_at='2026-10-01T09:44:00Z')
        self.add(4, tls_status='provisioning', auto_connect_attempts=1,
                 auto_connect_last_attempt_at='2026-10-01T09:44:00Z', auto_connect_requested_at=None)
        self.assertEqual(module.recover_stale(self.db, NOW), 2)
        self.assertEqual(self.get(1)['auto_connect_attempts'], 1)
        self.assertEqual(self.get(1)['last_tls_error'], module.FAILURES['interrupted'])
        self.assertEqual(self.get(2)['tls_status'], 'provisioning')
        self.assertEqual(self.get(4)['tls_status'], 'provisioning')
        self.assertEqual([r['id'] for r in module.candidates(self.db, NOW)], [1])

    def test_batch_limit_and_no_secret_command_error_in_database(self):
        for ident in range(1, 7):
            self.add(ident)
        logger = logging.getLogger('offline-test')
        with patch.object(module, 'timestamp', return_value=NOW), patch.object(module, 'run_provisioner', return_value='failed') as run:
            summary = module.process_batch(self.path, Path('/unused/script.py'), '8.8.4.4', 'ops@example.com', logger)
        self.assertEqual(summary['claimed'], 4)
        self.assertEqual(summary['failed'], 4)
        self.assertEqual(run.call_count, 4)
        self.assertEqual(self.get(5)['tls_status'], 'pending')
        self.assertNotIn('unused', self.get(1)['last_tls_error'])

    def test_command_uses_separate_arguments_and_complete_claim_identity(self):
        self.add()
        row = module.claim(self.db, NOW)
        command = module.command_for(row, Path('/opt/ops/script.py'), self.path, '8.8.4.4', 'ops@example.com')
        for flag, value in [('--expected-domain', row['domain']), ('--expected-token', TOKEN),
                            ('--expected-reseller-id', '1'), ('--expected-attempt-at', NOW),
                            ('--expected-attempts', '1'),
                            ('--expected-requested-at', row['auto_connect_requested_at'])]:
            self.assertEqual(command[command.index(flag) + 1], value)
        self.assertEqual(command[-1], '--apply')

    def test_unsafe_host_token_never_launches_and_does_not_starve_queue(self):
        self.add(1, domain='foo.com; include evil;')
        self.add(2, verification_token=TOKEN + '\n')
        self.add(3)
        with patch.object(module, 'timestamp', return_value=NOW), patch.object(module, 'run_provisioner', return_value='provisioned') as run:
            result = module.process_batch(self.path, Path('/unused/script.py'), '8.8.4.4', 'ops@example.com', logging.getLogger('offline-test'))
        self.assertEqual(run.call_count, 1)
        self.assertEqual(result['failed'], 2)
        self.assertEqual(result['provisioned'], 1)
        self.assertEqual(self.get(1)['auto_connect_attempts'], 5)

    def test_timeout_terminates_child_process_group_and_keeps_output_private(self):
        process = Mock(pid=7654321)
        process.wait.side_effect = [subprocess.TimeoutExpired('child', 300), 0]
        logger = Mock()
        def start(command, **kwargs):
            self.assertIs(kwargs['shell'], False)
            self.assertIs(kwargs['start_new_session'], True)
            kwargs['stdout'].write(b'private certbot diagnostic with ownership token')
            return process
        with patch.object(module.subprocess, 'Popen', side_effect=start), patch.object(module.os, 'killpg') as kill:
            result = module.run_provisioner(['python3', '/private/script.py'], logger, 123)
        self.assertEqual(result, 'timeout')
        kill.assert_called_once_with(process.pid, module.signal.SIGTERM)
        self.assertIn('private certbot diagnostic', logger.info.call_args.args[-1])

    def test_trusted_provisioner_checks_every_ancestor_to_root(self):
        path = Path('/usr/local/lib/dujiao-next/provision-reseller-domain.py')
        items = (path, *path.parents)
        nodes = {item: SimpleNamespace(st_uid=0, st_mode=(stat.S_IFREG | 0o700) if item == path
                                     else (stat.S_IFDIR | 0o755)) for item in items}
        visited = []
        def lstat(item):
            visited.append(item)
            return nodes[item]
        with patch.object(Path, 'lstat', lstat):
            module.trusted_provisioner(path)
        self.assertEqual(visited, list(items))
        self.assertEqual(visited[-1], Path('/'))
        for unsafe in items:
            for owner, mode in [(1000, nodes[unsafe].st_mode), (0, nodes[unsafe].st_mode | 0o020),
                                (0, nodes[unsafe].st_mode | 0o002), (0, stat.S_IFLNK | 0o755)]:
                with self.subTest(unsafe=str(unsafe), owner=owner, mode=mode):
                    original = nodes[unsafe]
                    nodes[unsafe] = SimpleNamespace(st_uid=owner, st_mode=mode)
                    with patch.object(Path, 'lstat', lstat), self.assertRaises(ValueError):
                        module.trusted_provisioner(path)
                    nodes[unsafe] = original

    def test_trusted_provisioner_rejects_ambiguous_paths(self):
        for path in [Path('script.py'), Path('/root/../tmp/script.py')]:
            with self.subTest(path=str(path)), self.assertRaises(ValueError):
                module.trusted_provisioner(path)

    def test_default_cli_is_read_only(self):
        self.add()
        before = self.path.read_bytes()
        result = subprocess.run(['python3', str(HERE / 'auto-provision-reseller-domains.py'), '--db', str(self.path), '--entry-ip', '8.8.4.4', '--email', 'ops@example.com'],
                                text=True, capture_output=True, check=True)
        data = json.loads(result.stdout)
        self.assertFalse(data['database_changed'])
        self.assertEqual(data['mode'], 'dry-run')
        self.assertEqual(data['candidates'][0]['id'], 1)
        self.assertEqual(before, self.path.read_bytes())
        self.assertEqual(self.get()['auto_connect_attempts'], 0)


if __name__ == '__main__':
    unittest.main()
