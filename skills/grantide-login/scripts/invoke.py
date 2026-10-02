#!/usr/bin/env python3
"""Run Grantide with the calling chat's connection, without printing credentials."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
from urllib.parse import urlsplit


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true')
    parser.add_argument('--config', type=Path, default=Path.home() / '.config/grantide/codex-connections.json')
    parser.add_argument('--binary', type=Path)
    parser.add_argument('command', nargs=argparse.REMAINDER)
    args = parser.parse_args()
    binary = (args.binary or Path(__file__).resolve().parents[3] / 'bin/grantide').resolve()
    env = dict(os.environ)
    thread = env.get('CODEX_THREAD_ID')
    entry = {}
    if thread and args.config.is_file():
        entry = json.loads(args.config.read_text()).get('threads', {}).get(thread, {})
    url = env.get('GRANTIDE_URL') or entry.get('url')
    if not url and entry.get('port_file'):
        port = Path(entry['port_file']).read_text().strip()
        if not port.isdecimal() or not 0 < int(port) < 65536:
            raise ValueError('Invalid port')
        url = 'http://127.0.0.1:' + port
    token = env.get('GRANTIDE_TOKEN')
    token_file = env.get('GRANTIDE_AGENT_TOKEN_FILE') or entry.get('token_file')
    if not token and token_file:
        token = Path(token_file).read_text().strip()
    if not url or not token:
        raise ValueError('Missing calling-chat connection')
    u = urlsplit(url)
    if (u.scheme != 'http' or u.hostname != '127.0.0.1' or u.username or u.password
            or u.path or u.query or u.fragment):
        raise ValueError('Invalid loopback origin')
    if len(token) != 64 or any(c not in '0123456789abcdef' for c in token):
        raise ValueError('Invalid agent token format')
    if not binary.is_file() or not os.access(binary, os.X_OK):
        raise ValueError('Missing executable')
    if args.check:
        print(json.dumps({'configured': True, 'url': url, 'binary': str(binary),
                          'chrome_user_data_dir': entry.get('chrome_user_data_dir')}, ensure_ascii=False))
        return 0
    if not args.command or args.command[0] not in ('web-login', 'browser-login', 'credential', 'call'):
        raise ValueError('Unsupported command')
    env['GRANTIDE_URL'] = url
    env['GRANTIDE_TOKEN'] = token
    return subprocess.run([str(binary), *args.command], env=env).returncode


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (OSError, ValueError, TypeError, AttributeError):
        print('Grantide connection missing or invalid. Follow references/chat-connection.md for this chat; do not borrow another chat token.', file=sys.stderr)
        sys.exit(2)
