#!/usr/bin/env python3
"""Install the local Grantide Chrome native host for one unpacked extension ID."""
import argparse
import json
import os
import pathlib
import platform
import re
import shlex

p = argparse.ArgumentParser()
p.add_argument('--extension-id', required=True)
p.add_argument('--binary', type=pathlib.Path, required=True)
p.add_argument('--data-dir', type=pathlib.Path, required=True)
args = p.parse_args()
if not re.fullmatch('[a-p]{32}', args.extension_id):
    p.error('extension ID must be 32 Chrome ID characters')
binary = args.binary.resolve(strict=True)
data_dir = args.data_dir.resolve(strict=True)
if not binary.is_file() or not os.access(binary, os.X_OK):
    p.error('Grantide binary must be executable')
if platform.system() == 'Darwin':
    host_dir = pathlib.Path.home() / 'Library/Application Support/Google/Chrome/NativeMessagingHosts'
elif platform.system() == 'Linux':
    host_dir = pathlib.Path.home() / '.config/google-chrome/NativeMessagingHosts'
else:
    p.error('this installer currently supports macOS and Linux Chrome')
host_dir.mkdir(parents=True, exist_ok=True)
wrapper = data_dir / 'grantide-native-host.sh'
wrapper.write_text('#!/bin/sh\n'
    + 'export GRANTIDE_EXTENSION_ID=' + shlex.quote(args.extension_id) + '\n'
    + 'export GRANTIDE_DATA_DIR=' + shlex.quote(str(data_dir)) + '\n'
    + 'exec ' + shlex.quote(str(binary)) + ' extension-host "$@"\n')
wrapper.chmod(0o700)
manifest = {
    'name': 'com.grantide.login',
    'description': 'Grantide local login bridge',
    'path': str(wrapper),
    'type': 'stdio',
    'allowed_origins': ['chrome-extension://' + args.extension_id + '/'],
}
target = host_dir / 'com.grantide.login.json'
target.write_text(json.dumps(manifest, indent=2) + '\n')
target.chmod(0o600)
print('Installed native host for this exact extension ID. Reload the extension in Chrome.')
