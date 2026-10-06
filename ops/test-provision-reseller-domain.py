#!/usr/bin/env python3
"""Offline checks only; never contacts a provider or edits /etc."""
import importlib.util
from contextlib import closing
import json
from pathlib import Path
import sqlite3
import stat
from types import SimpleNamespace
import subprocess
import tempfile
import unittest
from unittest.mock import Mock, patch

spec = importlib.util.spec_from_file_location('provision', Path(__file__).with_name('provision-reseller-domain.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
TOKEN = 'opengpt-' + 'a' * 48

class ProvisionTests(unittest.TestCase):
    def test_config_keeps_tenant_host_and_uses_verified_probe(self):
        result = module.nginx_config('shop.example.com', TOKEN, True)
        self.assertIn('proxy_set_header Host $host;', result)
        self.assertIn('proxy_pass http://127.0.0.1:8080;', result)
        self.assertIn('location = ' + module.PROBE, result)
        self.assertIn('ssl_certificate /etc/letsencrypt/live/dujiao-reseller-shop.example.com/fullchain.pem;', result)
        self.assertIn('location ^~ /admin/ { return 404; }', result)
        self.assertNotIn('default_server', result)
        self.assertIn('if ($host != shop.example.com) { return 421; }', result)
        self.assertIn('access_log /var/log/nginx/dujiao-reseller-access.log dujiao_safe;', result)
    def test_challenge_does_not_expose_unapproved_shop(self):
        result = module.nginx_config('shop.example.com', TOKEN, False)
        self.assertIn('location / { return 404; }', result)
        self.assertNotIn('proxy_pass', result)
    def test_rejects_template_injection(self):
        for host in ('foo.com; include x;', '*.example.com', '127.0.0.1', 'x\n.example.com', '-bad.example.com'):
            with self.subTest(host=host), self.assertRaises(ValueError):
                module.nginx_config(host, TOKEN, True)
        with self.assertRaises(ValueError):
            module.nginx_config('shop.example.com', TOKEN + '\n', True)
    def test_dns_requires_current_txt_and_all_addresses(self):
        row = {'domain': 'shop.example.com', 'verification_token': TOKEN}
        answer = [(2, 1, 6, '', ('8.8.4.4', 443))]
        with patch.object(module.socket, 'getaddrinfo', return_value=answer), patch.object(module.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, '"' + TOKEN + '"\n')):
            module.check_dns(row, '8.8.4.4')
        with patch.object(module.socket, 'getaddrinfo', return_value=answer + [(2, 1, 6, '', ('1.1.1.1', 443))]), self.assertRaises(ValueError):
            module.check_dns(row, '8.8.4.4')
        with patch.object(module.socket, 'getaddrinfo', return_value=answer), patch.object(module.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, '"wrong"\n')), self.assertRaises(ValueError):
            module.check_dns(row, '8.8.4.4')

    def test_saas_dns_accepts_edge_only_after_binding_and_txt_verification(self):
        row = dict(domain='shop.example.com', verification_token=TOKEN, connect_mode='cloudflare_saas',
                   cloudflare_hostname_id='a' * 32, cloudflare_hostname_status='active', cloudflare_ssl_status='active')
        edge = [(2, 1, 6, '', ('104.16.1.1', 443))]
        with patch.object(module.socket, 'getaddrinfo', return_value=edge), \
             patch.object(module.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, '"' + TOKEN + '"\n')):
            module.check_dns(row, '8.8.4.4')
        for overrides in [dict(cloudflare_ssl_status='pending_validation'), dict(cloudflare_hostname_id=''),
                          dict(connect_mode='unknown')]:
            with self.subTest(overrides=overrides), self.assertRaises(ValueError):
                module.check_dns(dict(row, **overrides), '8.8.4.4')
        with patch.object(module.socket, 'getaddrinfo', return_value=[(2, 1, 6, '', ('127.0.0.1', 443))]), self.assertRaises(ValueError):
            module.check_dns(row, '8.8.4.4')

    def test_acme_preflight_is_public_pinned_no_redirect_and_cleans_up(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            def response(command, **kwargs):
                self.assertIn('--resolve', command)
                self.assertNotIn('--location', command)
                self.assertEqual(command[command.index('--proto') + 1], '=http')
                self.assertEqual(command[command.index('--max-redirs') + 1], '0')
                self.assertEqual(command[command.index('--resolve') + 1], 'shop.example.com:80:104.16.1.1')
                marker = next(directory.iterdir())
                self.assertEqual(stat.S_IMODE(marker.stat().st_mode), 0o644)
                return subprocess.CompletedProcess(command, 0, marker.read_text() + '\n200')
            with patch.object(module, 'public_addresses', return_value=['104.16.1.1']), \
                 patch.object(module.subprocess, 'run', side_effect=response):
                module.check_public_http_challenge('shop.example.com', directory)
            self.assertEqual(list(directory.iterdir()), [])
            with patch.object(module, 'public_addresses', return_value=['104.16.1.1']), \
                 patch.object(module.time, 'sleep'), \
                 patch.object(module.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, 'redirect\n301')), self.assertRaises(ValueError):
                module.check_public_http_challenge('shop.example.com', directory)
            self.assertEqual(list(directory.iterdir()), [])

    def test_probe_waits_for_nginx_reload_without_relaxing_verification(self):
        command = ['curl', '--fail', '--resolve', 'shop.example.com:80:104.16.1.1']
        attempts = [subprocess.CalledProcessError(22, command),
                    subprocess.CompletedProcess(command, 0, 'old vhost\n404'),
                    subprocess.CompletedProcess(command, 0, 'expected\n200')]
        with patch.object(module.time, 'sleep') as sleep, \
             patch.object(module.subprocess, 'run', side_effect=attempts) as run:
            module.wait_for_probe(command, 'expected\n200', 'not ready')
        self.assertEqual(run.call_count, 3)
        self.assertEqual(sleep.call_count, 2)
        for call in run.call_args_list:
            self.assertEqual(call.args[0], command)
            self.assertTrue(call.kwargs['check'])

    def test_probe_retry_is_bounded_and_never_accepts_wrong_body_or_redirect(self):
        for response in ['wrong\n200', 'expected\n301']:
            with self.subTest(response=response), patch.object(module.time, 'sleep') as sleep, \
                 patch.object(module.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, response)) as run, \
                 self.assertRaisesRegex(ValueError, 'not ready'):
                module.wait_for_probe(['curl'], 'expected\n200', 'not ready')
            self.assertEqual(run.call_count, 4)
            self.assertEqual(sleep.call_count, 3)

    def test_https_probe_retries_reload_handshake_failure_and_requires_token(self):
        command = ['curl', '--resolve', 'shop.example.com:443:8.8.4.4', 'https://shop.example.com' + module.PROBE]
        with patch.object(module.time, 'sleep'), patch.object(module.subprocess, 'run', side_effect=[
                subprocess.CalledProcessError(35, command),
                subprocess.CompletedProcess(command, 0, TOKEN + '\n')]) as run:
            module.wait_for_probe(command, TOKEN, 'bad https', strip=True)
        self.assertEqual(run.call_count, 2)
        self.assertNotIn('-k', command)

    def test_public_addresses_reject_mixed_private_destination(self):
        with patch.object(module.socket, 'getaddrinfo', return_value=[
                (2, 1, 6, '', ('104.16.1.1', 80)), (2, 1, 6, '', ('169.254.169.254', 80))]), self.assertRaises(ValueError):
            module.public_addresses('shop.example.com')
    def test_readonly_connection_uses_database_owner_and_closes_before_root_restore(self):
        events = []
        db = Mock()
        db.close.side_effect = lambda: events.append('close')
        info = SimpleNamespace(st_mode=stat.S_IFREG | 0o600, st_uid=1001, st_gid=1002)
        def connect(*args, **kwargs):
            events.append('connect')
            self.assertIn('?mode=ro', args[0])
            return db
        with patch.object(Path, 'lstat', return_value=info), patch.object(module.os, 'geteuid', return_value=0), \
             patch.object(module.os, 'getegid', return_value=0), \
             patch.object(module.os, 'seteuid', side_effect=lambda uid: events.append(('uid', uid))), \
             patch.object(module.os, 'setegid', side_effect=lambda gid: events.append(('gid', gid))), \
             patch.object(module.sqlite3, 'connect', side_effect=connect):
            with module.readonly_db(Path('/tmp/test-dujiao.db')) as active:
                self.assertIs(active, db)
                events.append('read')
        self.assertEqual(events, [('gid', 1002), ('uid', 1001), 'connect', 'read', 'close', ('uid', 0), ('gid', 0)])

    def test_readonly_connection_restores_root_when_open_fails(self):
        events = []
        info = SimpleNamespace(st_mode=stat.S_IFREG | 0o600, st_uid=1001, st_gid=1002)
        with patch.object(Path, 'lstat', return_value=info), patch.object(module.os, 'geteuid', return_value=0), \
             patch.object(module.os, 'getegid', return_value=0), \
             patch.object(module.os, 'seteuid', side_effect=lambda uid: events.append(('uid', uid))), \
             patch.object(module.os, 'setegid', side_effect=lambda gid: events.append(('gid', gid))), \
             patch.object(module.sqlite3, 'connect', side_effect=sqlite3.OperationalError('test failure')):
            with self.assertRaises(sqlite3.OperationalError), module.readonly_db(Path('/tmp/test-dujiao.db')):
                self.fail('failed connection cannot be entered')
        self.assertEqual(events, [('gid', 1002), ('uid', 1001), ('uid', 0), ('gid', 0)])

    def test_dry_run_reads_verified_row_without_writes(self):
        with tempfile.TemporaryDirectory() as tmp:
            db_path = Path(tmp) / 'db.sqlite'
            with closing(sqlite3.connect(db_path)) as db, db:
                db.executescript('CREATE TABLE reseller_profiles(id INTEGER,status TEXT,deleted_at TEXT); CREATE TABLE reseller_domains(id INTEGER,reseller_id INTEGER,domain TEXT,type TEXT,status TEXT,verification_status TEXT,verification_token TEXT,deleted_at TEXT);')
                db.execute('INSERT INTO reseller_profiles VALUES(1,?,NULL)', ('active',))
                db.execute('INSERT INTO reseller_domains VALUES(1,1,?,?,?,?,?,NULL)', ('shop.example.com', 'custom', 'pending_review', 'verified', TOKEN))
            before = db_path.read_bytes()
            result = subprocess.run(['python3', str(Path(__file__).with_name('provision-reseller-domain.py')), '--domain-id', '1', '--db', str(db_path), '--entry-ip', '8.8.4.4', '--email', 'ops@example.com'], text=True, capture_output=True, check=True)
            data = json.loads(result.stdout)
            self.assertEqual(data['mode'], 'dry-run')
            self.assertFalse(data['database_changed'])
            self.assertEqual(before, db_path.read_bytes())
            with closing(sqlite3.connect(db_path)) as db, db:
                db.execute("UPDATE reseller_profiles SET status='disabled'")
            with self.assertRaises(ValueError):
                module.load_domain(db_path, 1)

if __name__ == '__main__':
    unittest.main()
