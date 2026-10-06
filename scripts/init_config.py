#!/usr/bin/env python3
"""Create a private local config from the public template; never overwrite."""
from __future__ import annotations

import argparse
import getpass
import json
import os
from pathlib import Path
import re
import secrets


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--docker', action='store_true', help='Use Compose service redis')
    parser.add_argument('--output', type=Path, help='Explicit destination config path')
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    output = args.output or root / 'app' / 'config.yml'
    if output.exists():
        parser.error('Config already exists; it has not been changed.')
    username = input('管理员用户名：').strip()
    if not username or len(username) > 64:
        parser.error('管理员用户名不能为空或超过 64 字符。')
    password = getpass.getpass('管理员密码（至少 12 位，包含大小写字母与数字）：')
    if (len(password) < 12 or not re.search(r'[A-Z]', password)
            or not re.search(r'[a-z]', password) or not re.search(r'[0-9]', password)):
        parser.error('密码需至少 12 位并包含大小写字母与数字。')
    if getpass.getpass('再次输入密码：') != password:
        parser.error('两次密码不一致。')
    content = (root / 'app' / 'config.yml.example').read_text()
    marker = 'your-secret-key-change-in-production-please'
    content = content.replace(marker, secrets.token_hex(32), 1)
    content = content.replace(marker, secrets.token_hex(32), 1)
    content = content.replace('user-secret-key-change-in-production-please', secrets.token_hex(32), 1)
    content = content.replace('default_admin_username: ""', 'default_admin_username: ' + json.dumps(username, ensure_ascii=False), 1)
    content = content.replace('default_admin_password: ""', 'default_admin_password: ' + json.dumps(password), 1)
    content = content.replace('mode: debug', 'mode: release', 1)
    content = content.replace('email:\n  enabled: true', 'email:\n  enabled: false', 1)
    if args.docker:
        content = content.replace('redis:\n  enabled: true\n  host: 127.0.0.1', 'redis:\n  enabled: true\n  host: redis', 1)
        content = content.replace('queue:\n  enabled: true\n  host: 127.0.0.1', 'queue:\n  enabled: true\n  host: redis', 1)
    output.parent.mkdir(parents=True, exist_ok=True)
    fd = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as file:
        file.write(content)
    print('已生成受限运行配置：' + str(output))
    print('尚未配置支付、供应商、邮件和分销域名；请按部署说明逐项设置。')


if __name__ == '__main__':
    main()
