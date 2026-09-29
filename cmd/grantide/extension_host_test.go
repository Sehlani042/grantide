package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestExtensionHostChild(t *testing.T) {
	if os.Getenv("GRANTIDE_HOST_TEST") != "1" {
		return
	}
	if extensionHost([]string{os.Getenv("GRANTIDE_HOST_ORIGIN")}) != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestNativeHostFramingRestrictedRoutesAndOrigin(t *testing.T) {
	const id = "abcdefghijklmnopabcdefghijklmnop"
	token := strings.Repeat("a", 64)
	var mu sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("missing private loopback auth")
		}
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/complete") {
			b, _ := io.ReadAll(r.Body)
			if !json.Valid(b) || strings.Contains(string(b), token) {
				t.Error("invalid completion body")
			}
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "port"), []byte(u.Port()), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "operator.token"), []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	invoke := func(message string, origin string) ([]byte, error) {
		var input bytes.Buffer
		binary.Write(&input, binary.NativeEndian, uint32(len(message)))
		input.WriteString(message)
		cmd := exec.Command(os.Args[0], "-test.run=TestExtensionHostChild")
		cmd.Env = append(os.Environ(), "GRANTIDE_HOST_TEST=1", "GRANTIDE_DATA_DIR="+dir, "GRANTIDE_EXTENSION_ID="+id, "GRANTIDE_HOST_ORIGIN="+origin)
		cmd.Stdin = &input
		return cmd.Output()
	}
	for _, message := range []string{`{"action":"poll"}`, `{"action":"claim","id":"login_0123456789abcdef"}`, `{"action":"complete","id":"login_0123456789abcdef","status":"completed","fields":{"cpu":"2 cores"}}`} {
		out, err := invoke(message, "chrome-extension://"+id+"/")
		if err != nil {
			t.Fatal(err)
		}
		if len(out) < 4 || int(binary.NativeEndian.Uint32(out[:4])) != len(out)-4 || string(out[4:]) != `{"ok":true}` {
			t.Fatal("native response frame")
		}
		if bytes.Contains(out, []byte(token)) {
			t.Fatal("native host leaked operator token")
		}
	}
	for _, item := range []struct{ body, origin string }{{`{"action":"admin/state"}`, "chrome-extension://" + id + "/"}, {`{"action":"claim","id":"../state"}`, "chrome-extension://" + id + "/"}, {`{"action":"poll"}`, "chrome-extension://pppppppppppppppppppppppppppppppp/"}} {
		if _, err := invoke(item.body, item.origin); err == nil {
			t.Fatal("untrusted native action accepted")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"GET /admin/extension/next", "POST /admin/extension/logins/login_0123456789abcdef/claim", "POST /admin/extension/logins/login_0123456789abcdef/complete"}
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("native routing: %v", paths)
	}
}
