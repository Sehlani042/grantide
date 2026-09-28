package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func credentialCall() Call { return Call{ServiceID: "api", Method: "POST", Path: "/v1/tasks"} }

func TestCredentialHandoffPreservesApproval(t *testing.T) {
	f := setup(t, "approval")
	r, err := f.e.RequestCredential(f.actor, credentialCall())
	if err != nil {
		t.Fatal(err)
	}
	same, err := f.e.RequestCredential(f.actor, credentialCall())
	if err != nil || same.ID != r.ID {
		t.Fatal("duplicate prompt")
	}
	if _, ok := f.e.GetCredentialRequest("other", r.ID); ok {
		t.Fatal("cross-agent access")
	}
	if err := f.e.CancelCredential("other", r.ID); err == nil {
		t.Fatal("cross-agent cancellation")
	}
	old := f.submit(t)
	var successful atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if out, err := f.e.ResolveCredential(r.ID, "save", "", "fake-new-password"); err == nil && out.Status == "fulfilled" {
				successful.Add(1)
			}
		}()
	}
	wg.Wait()
	if successful.Load() != 1 || f.calls.Load() != 0 {
		t.Fatal("replay accepted or operation dispatched")
	}
	if out, _ := f.e.GetRequest(f.actor, old.ID); out.Status != "invalidated" {
		t.Fatal("old authorization survived")
	}
	out, ok := f.e.GetCredentialRequest(f.actor, r.ID)
	if !ok || out.Status != "fulfilled" {
		t.Fatal(out)
	}
	b, _ := json.Marshal(f.e.Snapshot())
	if strings.Contains(string(b), "fake-new-password") {
		t.Fatal("password in snapshot")
	}
	state, err := f.e.store.Load()
	if err != nil || state.Services[0].Secret != "fake-new-password" {
		t.Fatal("credential not persisted")
	}
	disk, err := os.ReadFile(filepath.Join(f.e.store.dir, "state.enc"))
	if err != nil || strings.Contains(string(disk), "fake-new-password") {
		t.Fatal("plaintext on disk")
	}
	next := f.submit(t)
	if next.Status != "pending" || f.calls.Load() != 0 {
		t.Fatal("credential entry bypassed approval")
	}
	if _, err := f.e.Decide(context.Background(), next.ID, "once"); err != nil || f.calls.Load() != 1 {
		t.Fatal("authorized operation failed", err)
	}
}

func TestCredentialLifecycleAndPolicy(t *testing.T) {
	for _, mode := range []string{"", "deny"} {
		f := setup(t, mode)
		if _, err := f.e.RequestCredential(f.actor, credentialCall()); err == nil {
			t.Fatal("denied operation requested credential")
		}
	}
	for _, action := range []string{"expire", "disable", "reject", "cancel", "save-failure"} {
		t.Run(action, func(t *testing.T) {
			f := setup(t, "auto")
			now := time.Now()
			f.e.now = func() time.Time { return now }
			r, err := f.e.RequestCredential(f.actor, credentialCall())
			if err != nil {
				t.Fatal(err)
			}
			switch action {
			case "expire":
				now = now.Add(PendingTTL)
			case "disable":
				if _, _, err := f.e.PutAgent(f.actor, "disabled", false); err != nil {
					t.Fatal(err)
				}
			case "reject":
				if _, err := f.e.ResolveCredential(r.ID, "reject", "", ""); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				if err := f.e.CancelCredential(f.actor, r.ID); err != nil {
					t.Fatal(err)
				}
			case "save-failure":
				f.e.store.dir = filepath.Join(t.TempDir(), "missing")
			}
			if _, err := f.e.ResolveCredential(r.ID, "save", "", "must-not-save"); err == nil {
				t.Fatal("inactive request saved")
			}
			if f.e.service("api").Secret != "fake-upstream-secret" || f.calls.Load() != 0 {
				t.Fatal("credential or operation changed")
			}
		})
	}
}

func TestCredentialHTTPSeparation(t *testing.T) {
	f := setup(t, "approval")
	operator := randomToken()
	h := Handler(f.e, operator, "", http.NotFoundHandler())
	r, err := f.e.RequestCredential(f.actor, credentialCall())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		token, body string
		status      int
	}{
		{f.token, `{"decision":"save","secret":"fake-pass"}`, 401},
		{operator, `{"decision":"save","fake-pass":"value"}`, 400},
		{operator, `{"decision":"save","secret":"fake-pass"}`, 200},
	} {
		req := httptest.NewRequest("POST", "http://localhost/admin/credential-requests/"+r.ID+"/decision", strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+tc.token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != tc.status || strings.Contains(w.Body.String(), "fake-pass") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func TestBasicCredentialForwardAndRedaction(t *testing.T) {
	var valid atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		valid.Store(ok && u == "fake-user" && p == "fake-password")
		fmt.Fprint(w, r.Header.Get("Authorization")+" fake-password")
	}))
	defer upstream.Close()
	f := setup(t, "approval")
	_, err := f.e.PutService(Service{ID: "api", Name: "Basic test", Origin: upstream.URL, Enabled: true, AllowPrivate: true, AuthType: "basic", AuthHeader: "Authorization"}, true)
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.e.RequestCredential(f.actor, credentialCall())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.e.ResolveCredential(r.ID, "save", "fake-user", "fake-password"); err != nil {
		t.Fatal(err)
	}
	f.e.executor = executeHTTP
	next := f.submit(t)
	if valid.Load() {
		t.Fatal("forwarded before approval")
	}
	done, err := f.e.Decide(context.Background(), next.ID, "once")
	if err != nil || !valid.Load() || done.Result == nil {
		t.Fatal(done, err)
	}
	if strings.Contains(done.Result.Body, "fake-password") || strings.Contains(done.Result.Body, base64.StdEncoding.EncodeToString([]byte("fake-user:fake-password"))) {
		t.Fatal("password reflected")
	}
}
