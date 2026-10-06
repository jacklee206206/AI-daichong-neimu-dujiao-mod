#!/usr/bin/env python3
"""Check publication contents without printing any suspected secret values."""
from __future__ import annotations

import json
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]
PRIVATE_NAMES = {'config.yml', '.env', '.DS_Store'}
PRIVATE_DIRS = {'node_modules', 'dist', '__pycache__', '.codex-test-output', 'db', 'logs', 'uploads', 'state', 'secrets'}
PRIVATE_SUFFIXES = {'.db', '.sqlite', '.sqlite3', '.pem', '.key', '.p12', '.pfx', '.log', '.pyc'}
PATTERNS = {
    'card inventory': re.compile(r'\bPH(?:5)?-[A-Z0-9]{24,}\b'),
    'private key': re.compile(r'^-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----', re.M),
    'GitHub token': re.compile(r'\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{50,})\b'),
    'AWS access key': re.compile(r'\bAKIA[A-Z0-9]{16}\b'),
}
PRODUCTION_HOSTS = re.compile(r'evanaishop\.com|openanthroai\.com|openanthropic\.xyz|43\.165\.183\.203')


def main() -> int:
    issues = []
    count = 0
    for path in ROOT.rglob('*'):
        rel = path.relative_to(ROOT)
        if '.git' in rel.parts:
            continue
        if path.is_symlink():
            issues.append((str(rel), 'symbolic link'))
            continue
        if not path.is_file():
            continue
        count += 1
        if (path.name in PRIVATE_NAMES or set(rel.parts) & PRIVATE_DIRS
                or path.suffix in PRIVATE_SUFFIXES
                or (path.name.startswith('.env.') and path.name != '.env.example')
                or (path.name.endswith('.env') and not path.name.endswith('.env.example'))):
            issues.append((str(rel), 'runtime or private file'))
        try:
            content = path.read_text()
        except UnicodeError:
            continue
        for label, pattern in PATTERNS.items():
            if pattern.search(content):
                issues.append((str(rel), label))
        if rel.parts[0] in {'app', 'ops'} and PRODUCTION_HOSTS.search(content):
            issues.append((str(rel), 'production domain or IP in runnable source'))
        if path.suffix == '.md' and (rel.parts[0] == 'docs' or rel == Path('README.md')):
            for target in re.findall(r'\]\(([^)]+)\)', content):
                target = target.split('#', 1)[0]
                if not target or target.startswith(('http:', 'https:', 'mailto:')):
                    continue
                if not (path.parent / target).exists():
                    issues.append((str(rel), 'missing relative link: ' + target))
    print(json.dumps({'files_checked': count, 'issues': [dict(path=p, reason=r) for p, r in issues]},
                     ensure_ascii=False, indent=2))
    if issues:
        print('公开包检查未通过；以上仅列文件名与类别，不输出疑似凭据。', file=sys.stderr)
        return 1
    print('公开包检查通过。此工具是有限模式检查，不能代替人工审阅新增文件。')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
