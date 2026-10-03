import concurrent.futures
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

SCRIPT = Path(__file__).with_name('invoke.py')
spec = importlib.util.spec_from_file_location('invoke', SCRIPT)
invoke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(invoke)

class HelperTests(unittest.TestCase):
    def test_atomic_private_identity(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)/'private'/'identity.token'
            with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
                results = list(pool.map(lambda _: invoke.identity_file(path), range(24)))
            self.assertEqual(len(set(results)), 1)
            self.assertEqual(len(results[0]), 64)
            if os.name != 'nt':
                self.assertEqual(path.stat().st_mode & 0o777, 0o600)
                path.chmod(0o644)
                with self.assertRaises(ValueError): invoke.read_private(path)

    def test_new_chat_needs_no_registry_or_token_copy(self):
        observed = []
        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args): pass
            def do_POST(self):
                body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
                observed.append((body,self.headers['Authorization']))
                self.send_response(202);self.end_headers()
                self.wfile.write(json.dumps({'id':'connect_'+'a'*16,'status':'pending','target':body['target']}).encode())
        server = ThreadingHTTPServer(('127.0.0.1',0),Handler)
        thread = threading.Thread(target=server.serve_forever,daemon=True);thread.start()
        try:
            with tempfile.TemporaryDirectory() as directory:
                directory = Path(directory)
                discovery = directory/'local-service.json'
                discovery.write_text(json.dumps({'url':'http://127.0.0.1:'+str(server.server_port)}))
                env = {k:v for k,v in os.environ.items() if not k.startswith('GRANTIDE_')}
                outputs = []
                for chat in ('first-chat','second-chat','first-chat'):
                    out = subprocess.run([sys.executable,str(SCRIPT),'--config',str(directory/'connections.json'),'--discovery',str(discovery),'login','--url','https://app.cloudcone.com/vps/12345/manage','--wait','0'],env=dict(env,CODEX_THREAD_ID=chat),capture_output=True,text=True)
                    self.assertEqual(out.returncode,0,out.stderr)
                    outputs.append(out.stdout+out.stderr)
                self.assertEqual(observed[0][1],observed[2][1]);self.assertNotEqual(observed[0][1],observed[1][1])
                for _,authorization in observed:
                    self.assertNotIn(authorization.split()[1],''.join(outputs))
                self.assertFalse((directory/'connections.json').exists())
        finally:
            server.shutdown();server.server_close();thread.join()

if __name__ == '__main__': unittest.main()
