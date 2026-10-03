#!/usr/bin/env python3
"""Invoke Grantide with automatically held, per-chat credentials. Never print secrets."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import secrets
import stat
import subprocess
import sys
import time
import tempfile
from urllib.error import HTTPError
from urllib.parse import urlsplit
from urllib.request import Request, build_opener, ProxyHandler, HTTPRedirectHandler


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def read_private(path):
    fd = os.open(path, os.O_RDONLY | getattr(os, 'O_NOFOLLOW', 0))
    with os.fdopen(fd) as source:
        info = os.fstat(source.fileno())
        if not stat.S_ISREG(info.st_mode) or (os.name != 'nt' and (info.st_mode & 0o077 or info.st_uid != os.getuid())):
            raise ValueError('Agent credential file must be owned by you with mode 0600')
        return source.read().strip()


def identity_file(path):
    path.parent.mkdir(parents=True, mode=0o700, exist_ok=True)
    fd, temporary = tempfile.mkstemp(prefix='.identity-', dir=path.parent)
    try:
        with os.fdopen(fd, 'w') as out:
            token = secrets.token_hex(32)
            out.write(token + '\n')
            out.flush()
            os.fsync(out.fileno())
        try:
            os.link(temporary, path)
        except FileExistsError:
            return read_private(path)
        return token
    finally:
        os.unlink(temporary)



def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true')
    parser.add_argument('--config', type=Path, default=Path.home() / '.config/grantide/codex-connections.json')
    parser.add_argument('--discovery', type=Path, default=Path.home() / '.config/grantide/local-service.json')
    parser.add_argument('--binary', type=Path)
    parser.add_argument('command', nargs=argparse.REMAINDER)
    args = parser.parse_args()
    env = dict(os.environ)
    thread = env.get('CODEX_THREAD_ID')
    if not thread:
        raise ValueError('CODEX_THREAD_ID is required; run from the requesting Codex chat')
    entry = {}
    if args.discovery.is_file():
        entry.update(json.loads(args.discovery.read_text()))
    if args.config.is_file():
        entry.update(json.loads(args.config.read_text()).get('threads', {}).get(thread, {}))
    binary = (args.binary or Path(entry.get('binary') or Path(__file__).resolve().parents[3] / 'bin/grantide')).resolve()
    url = env.get('GRANTIDE_URL') or entry.get('url')
    if not url and entry.get('port_file'):
        port = Path(entry['port_file']).read_text().strip()
        if not port.isdecimal() or not 0 < int(port) < 65536:
            raise ValueError('Invalid local service port')
        url = 'http://127.0.0.1:' + port
    u = urlsplit(url or '')
    if (u.scheme != 'http' or u.hostname != '127.0.0.1' or u.username or u.password
            or u.path or u.query or u.fragment):
        raise ValueError('Grantide local service is not configured; start the installed Grantide service')
    token = env.get('GRANTIDE_TOKEN')
    token_file = env.get('GRANTIDE_AGENT_TOKEN_FILE') or entry.get('token_file')
    if not token:
        # Hashing the untrusted thread label prevents path traversal. No registry mutation
        # or other-chat fallback is needed, including simultaneous first invocations.
        token = (read_private(Path(token_file)) if token_file else identity_file(
            args.config.parent / 'agent-tokens' / (hashlib.sha256(thread.encode()).hexdigest() + '.token')))
    if len(token) != 64 or any(c not in '0123456789abcdef' for c in token):
        raise ValueError('Invalid agent credential file')
    opener = build_opener(ProxyHandler({}), NoRedirect())

    def request(path, body=None):
        data = None if body is None else json.dumps(body).encode()
        req = Request(url + path, data=data, headers={'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json'})
        try:
            with opener.open(req, timeout=15) as response:
                return json.load(response)
        except HTTPError as exc:
            # Server errors contain fixed validation messages, never request headers.
            try:
                message = json.load(exc).get('error', 'Request failed')
            except (ValueError, AttributeError):
                message = 'Request failed'
            raise ValueError(message) from None

    if args.check:
        with opener.open(url + '/healthz', timeout=5) as response:
            if response.status != 200:
                raise ValueError('Local service unavailable')
        print(json.dumps({'configured': True, 'identity_stored': True, 'url': url,
                          'approval_url': url + '/?view=approvals',
                          'chrome_user_data_dir': entry.get('chrome_user_data_dir')}, ensure_ascii=False))
        return 0
    if args.command and args.command[0] == 'login':
        login = argparse.ArgumentParser(description='Request login; approval automatically starts the paired extension')
        login.add_argument('--url')
        login.add_argument('--name', default='Codex · ' + thread[:8])
        login.add_argument('--account-label', default='')
        login.add_argument('--id')
        login.add_argument('--status', action='store_true', help='read the latest request owned by this identity')
        login.add_argument('--wait', type=float, default=30, help='seconds to wait; timeout does not cancel')
        opts = login.parse_args(args.command[1:])
        if opts.wait < 0 or opts.wait > 1200:
            raise ValueError('Wait must be between 0 and 1200 seconds')
        if opts.status:
            out = request('/v1/connections/latest')
        elif opts.id:
            if not opts.id.startswith('connect_') or len(opts.id) != 24 or any(c not in '0123456789abcdef' for c in opts.id[8:]):
                raise ValueError('Invalid connection ID')
            out = request('/v1/connections/' + opts.id)
        else:
            if not opts.url:
                raise ValueError('login requires --url or --id')
            out = request('/v1/connections', {'name': opts.name, 'conversation': thread, 'target': opts.url, 'account_label': opts.account_label})
        deadline = time.monotonic() + opts.wait
        last = None
        while True:
            result = dict(out, approval_url=url+'/?view=approvals', chrome_user_data_dir=entry.get('chrome_user_data_dir'))
            encoded = json.dumps(result, ensure_ascii=False)
            if encoded != last:
                print(encoded, flush=True)
                last = encoded
            if out['status'] not in ('pending', 'executing'):
                return 0 if out['status'] == 'completed' else 1
            if time.monotonic() >= deadline:
                return 0
            time.sleep(min(2, max(0, deadline-time.monotonic())))
            out = request('/v1/connections/' + out['id'])
    if not args.command or args.command[0] not in ('web-login', 'browser-login', 'credential', 'call'):
        raise ValueError('Use login --url TARGET for automatic connection and approval')
    if not binary.is_file() or not os.access(binary, os.X_OK):
        raise ValueError('Missing Grantide executable')
    env['GRANTIDE_URL'], env['GRANTIDE_TOKEN'] = url, token
    return subprocess.run([str(binary), *args.command], env=env).returncode


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (OSError, ValueError, TypeError, AttributeError) as error:
        print('Grantide: ' + str(error), file=sys.stderr)
        sys.exit(2)
