package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestExtensionLoginFailedSaveAndRestartGuard(t *testing.T) {
	f, a, g := loginFixture(t, nil)
	dir := f.e.store.dir
	f.e.store.dir = filepath.Join(t.TempDir(), "missing")
	if _, err := f.e.StartExtensionLogin(f.actor, g.ID); err == nil {
		t.Fatal("failed save started extension")
	}
	if f.e.state.LoginGrants[g.ID].Remaining != 1 || f.e.account(a.ID).LoginNeedsReview || f.e.account(a.ID).NextLoginAt != nil {
		t.Fatal("reservation rollback")
	}
	f.e.store.dir = dir
	r, err := f.e.StartExtensionLogin(f.actor, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.e.store.dir = filepath.Join(t.TempDir(), "missing")
	if in, err := f.e.ClaimExtensionLogin(r.ID); err == nil || in.Password != "" {
		t.Fatal("failed claim exposed credential")
	}
	if f.e.loginRuns[r.ID].Status != "extension_pending" {
		t.Fatal("failed claim changed status")
	}
	f.e.store.dir = dir
	if _, err := f.e.ClaimExtensionLogin(r.ID); err != nil {
		t.Fatal(err)
	}
	f.e.store.dir = filepath.Join(t.TempDir(), "missing")
	if err := f.e.CompleteExtensionLogin(r.ID, "completed", nil); err == nil || !f.e.account(a.ID).LoginNeedsReview || f.e.loginRuns[r.ID].Authenticated {
		t.Fatal("failed completion reported success")
	}
	f.e.store.dir = dir
	reloaded, err := NewEngine(f.e.store, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reloaded.CloseLogins)
	if reloaded.NextExtensionLogin() != nil || !reloaded.account(a.ID).LoginNeedsReview || reloaded.state.LoginGrants[g.ID].Remaining != 0 {
		t.Fatal("crash/restart reset reservation guard")
	}
}

func TestExtensionLoginBudgetSingleClaimAndSecretBoundary(t *testing.T) {
	f, a, g := loginFixture(t, nil)
	if _, err := f.e.StartExtensionLogin("other", g.ID); err == nil {
		t.Fatal("cross-agent start")
	}
	r, err := f.e.StartExtensionLogin(f.actor, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "extension_pending" || r.Transport != "extension" || f.e.state.LoginGrants[g.ID].Remaining != 0 || !f.e.account(a.ID).LoginNeedsReview {
		t.Fatal("reservation missing")
	}
	if err := f.e.CompleteExtensionLogin(r.ID, "completed", nil); err == nil {
		t.Fatal("unclaimed result")
	}
	if _, err := f.e.StartExtensionLogin(f.actor, g.ID); err == nil {
		t.Fatal("use replay")
	}
	var wg sync.WaitGroup
	claims := make(chan LoginInput, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			in, err := f.e.ClaimExtensionLogin(r.ID)
			if err == nil {
				claims <- in
			}
		}()
	}
	wg.Wait()
	close(claims)
	if len(claims) != 1 {
		t.Fatal("claim was not atomic")
	}
	in := <-claims
	if in.Password != "FakeOnly-Pass-582" || in.Username != "fake@example.test" {
		t.Fatal("trusted claim missing credentials")
	}
	if err := f.e.CompleteExtensionLogin(r.ID, "completed", map[string]string{"price": in.Password}); err == nil {
		t.Fatal("secret result accepted")
	}
	if err := f.e.CompleteExtensionLogin(r.ID, "completed", map[string]string{"cpu": "2 cores"}); err != nil {
		t.Fatal(err)
	}
	out, ok := f.e.GetLogin(f.actor, r.ID)
	if !ok || !out.Authenticated || out.Fields["cpu"] != "2 cores" || f.e.account(a.ID).LoginNeedsReview {
		t.Fatal("completion missing")
	}
	if _, ok := f.e.GetLogin("other", r.ID); ok {
		t.Fatal("cross-agent read")
	}
	if err := f.e.CompleteExtensionLogin(r.ID, "completed", nil); err == nil {
		t.Fatal("result replay")
	}
	for _, v := range []any{out, f.e.NextExtensionLogin(), f.e.Snapshot()} {
		b, _ := json.Marshal(v)
		if strings.Contains(string(b), in.Password) || strings.Contains(string(b), in.Username) {
			t.Fatal("public metadata leaked")
		}
	}
}

func TestExtensionLoginRevocationExpiryAndFailurePause(t *testing.T) {
	for _, action := range []string{"revoke", "cancel", "expire", "failure", "configuration"} {
		t.Run(action, func(t *testing.T) {
			f, a, g := loginFixture(t, nil)
			r, err := f.e.StartExtensionLogin(f.actor, g.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.e.ClaimExtensionLogin(r.ID); err != nil {
				t.Fatal(err)
			}
			switch action {
			case "revoke":
				err = f.e.RevokeLoginGrant(g.ID)
			case "cancel":
				err = f.e.CancelLogin(f.actor, r.ID)
			case "expire":
				f.e.now = func() time.Time { return r.ExpiresAt }
				f.e.NextExtensionLogin()
			case "failure":
				err = f.e.CompleteExtensionLogin(r.ID, "login_failed", nil)
			case "configuration":
				_, _, err = f.e.PutAgent(f.actor, "Renamed", true)
			}
			if err != nil {
				t.Fatal(err)
			}
			if f.e.NextExtensionLogin() != nil || !f.e.account(a.ID).LoginNeedsReview {
				t.Fatal("terminal run or failure guard lost")
			}
			if err := f.e.CompleteExtensionLogin(r.ID, "completed", nil); err == nil {
				t.Fatal("late success")
			}
		})
	}
}

func TestExtensionHostHTTPIsOperatorOnly(t *testing.T) {
	f, _, g := loginFixture(t, nil)
	op := randomToken()
	s := httptest.NewServer(Handler(f.e, op, "", http.NotFoundHandler()))
	defer s.Close()
	r, err := f.e.StartExtensionLogin(f.actor, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, token, body, origin string) (int, string) {
		req, _ := http.NewRequest(method, s.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Origin", origin)
		res, err := s.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	for _, item := range []struct{ method, path string }{{"GET", "/admin/extension/next"}, {"POST", "/admin/extension/logins/" + r.ID + "/claim"}, {"POST", "/admin/extension/logins/" + r.ID + "/complete"}} {
		if code, _ := request(item.method, item.path, f.token, `{}`, ""); code != 401 {
			t.Fatal("agent reached native-host API")
		}
	}
	if code, _ := request("POST", "/admin/extension/logins/"+r.ID+"/claim", op, `{}`, "https://attacker.test"); code != 403 {
		t.Fatal("cross-origin native claim")
	}
	if code, b := request("GET", "/admin/extension/next", op, "", ""); code != 200 || strings.Contains(b, "FakeOnly") {
		t.Fatal("metadata poll")
	}
	if code, b := request("GET", "/v1/logins/"+r.ID, f.token, "", ""); code != 200 || strings.Contains(b, "FakeOnly") {
		t.Fatal("agent result")
	}
}
