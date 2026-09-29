package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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

func loginFixture(t *testing.T, runner LoginRunner) (*fixture, WebAccount, LoginGrant) {
	t.Helper()
	f := setup(t, "")
	a, err := f.e.PutWebAccount(WebAccount{Name: "Fake account", Origin: CloudConeOrigin, Username: "fake@example.test", Password: "FakeOnly-Pass-582", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	f.e.SetLoginRunner(runner)
	g, err := f.e.CreateLoginGrant(a.ID, f.actor, "/compute/12345", 600, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.e.CloseLogins)
	return f, a, g
}
func awaitLogin(t *testing.T, f *fixture, id, status string) LoginRun {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		r, _ := f.e.GetLogin(f.actor, id)
		if r.Status == status {
			return r
		}
		time.Sleep(time.Millisecond)
	}
	r, _ := f.e.GetLogin(f.actor, id)
	t.Fatalf("wanted %s got %s", status, r.Status)
	return r
}
func TestControlledLoginHumanGateAndNoSecret(t *testing.T) {
	var saw atomic.Bool
	f, a, g := loginFixture(t, func(ctx context.Context, in LoginInput, emit func(LoginEvent) error, resume <-chan struct{}) error {
		saw.Store(in.Password == "FakeOnly-Pass-582" && in.Username == "fake@example.test")
		if err := emit(LoginEvent{Status: "human_required"}); err != nil {
			return err
		}
		if err := emit(LoginEvent{Status: "completed", Authenticated: true}); err == nil {
			return errors.New("human gate bypassed")
		}
		select {
		case <-resume:
		case <-ctx.Done():
			return ctx.Err()
		}
		return emit(LoginEvent{Status: "completed", Authenticated: true, Fields: map[string]string{"cpu": "2 cores", "memory": "2 GB"}})
	})
	if a.Password != "" || a.Username != "" || !a.SecretSet {
		t.Fatal("account response leaked")
	}
	r, err := f.e.StartLogin(f.actor, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	awaitLogin(t, f, r.ID, "human_required")
	if _, ok := f.e.GetLogin("other", r.ID); ok {
		t.Fatal("cross-agent read")
	}
	if _, err := f.e.StartLogin("other", g.ID); err == nil {
		t.Fatal("cross-agent dispatch")
	}
	if err := f.e.ResumeLogin(r.ID); err != nil {
		t.Fatal(err)
	}
	r = awaitLogin(t, f, r.ID, "completed")
	if !saw.Load() || !r.Authenticated || r.Fields["cpu"] != "2 cores" {
		t.Fatal("worker failed")
	}
	if _, err := f.e.StartLogin(f.actor, g.ID); err == nil {
		t.Fatal("use replay")
	}
	b, _ := json.Marshal(f.e.Snapshot())
	if strings.Contains(string(b), "FakeOnly-Pass-582") || strings.Contains(string(b), "fake@example.test") {
		t.Fatal("snapshot leaked")
	}
	stored, _ := os.ReadFile(filepath.Join(f.e.store.dir, "state.enc"))
	if bytes.Contains(stored, []byte("FakeOnly-Pass-582")) {
		t.Fatal("plaintext persisted")
	}
	recovered, err := NewEngine(f.e.store, false)
	if err != nil || recovered.state.WebAccounts[0].Password != "FakeOnly-Pass-582" {
		t.Fatal("encrypted reload failed")
	}
	if recovered.state.LoginGrants[g.ID].Remaining != 0 || len(recovered.loginRuns) != 0 {
		t.Fatal("restart restored consumed budget or runtime run")
	}
}
func TestControlledLoginAtomicBudgetAndCancellation(t *testing.T) {
	var dispatched atomic.Int32
	exited := make(chan struct{})
	f, _, g := loginFixture(t, func(ctx context.Context, in LoginInput, emit func(LoginEvent) error, resume <-chan struct{}) error {
		dispatched.Add(1)
		defer close(exited)
		<-ctx.Done()
		return ctx.Err()
	})
	var winners atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.e.StartLogin(f.actor, g.ID); err == nil {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatal("multiple budget winners")
	}
	if err := f.e.RevokeLoginGrant(g.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("worker not cancelled")
	}
	if dispatched.Load() != 1 {
		t.Fatal("wrong dispatch count")
	}
	for _, r := range f.e.Snapshot().LoginRuns {
		if r.Status != "cancelled" {
			t.Fatal(r.Status)
		}
	}
}
func TestControlledLoginValidationSaveFailureAndInvalidation(t *testing.T) {
	f, a, g := loginFixture(t, func(ctx context.Context, in LoginInput, emit func(LoginEvent) error, resume <-chan struct{}) error {
		<-ctx.Done()
		return ctx.Err()
	})
	for _, path := range []string{"/compute/1/delete", "//evil.test", "/billing", "/compute/1?x=1", "/compute/%31"} {
		if _, err := f.e.CreateLoginGrant(a.ID, f.actor, path, 60, 1); err == nil {
			t.Fatal("unsafe read scope", path)
		}
	}
	for _, origin := range []string{"http://app.cloudcone.com", "https://evil.test", "https://app.cloudcone.com.evil.test"} {
		if _, err := f.e.PutWebAccount(WebAccount{Name: "bad", Origin: origin, Username: "x", Password: "x", Enabled: true}); err == nil {
			t.Fatal("wrong origin")
		}
	}
	originalDir := f.e.store.dir
	f.e.store.dir = filepath.Join(t.TempDir(), "missing", "dir")
	if _, err := f.e.StartLogin(f.actor, g.ID); err == nil {
		t.Fatal("dispatched without audit")
	}
	if f.e.state.LoginGrants[g.ID].Remaining != 1 {
		t.Fatal("failed save consumed grant")
	}
	f.e.store.dir = originalDir
	r, err := f.e.StartLogin(f.actor, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.e.PutAgent(f.actor, "updated", true); err != nil {
		t.Fatal(err)
	}
	awaitLogin(t, f, r.ID, "cancelled")
	if _, err := f.e.StartLogin(f.actor, g.ID); err == nil {
		t.Fatal("invalidated grant accepted")
	}
}
func TestControlledLoginExpiryAndWorkerOutputFilter(t *testing.T) {
	for _, ev := range []LoginEvent{{Status: "completed", Authenticated: true, Fields: map[string]string{"password": "secret"}}, {Status: "completed", Authenticated: true, Fields: map[string]string{"cpu": "FakeOnly-Pass-582"}}, {Status: "completed", Authenticated: false}, {Status: "human_required", Authenticated: true}, {Status: "secret"}} {
		if safeLoginEvent(ev, LoginInput{Password: "FakeOnly-Pass-582", Username: "fake@example.test", ReadPath: "/compute/1"}) {
			t.Fatal("unsafe worker output")
		}
	}
	f, _, g := loginFixture(t, func(ctx context.Context, in LoginInput, emit func(LoginEvent) error, resume <-chan struct{}) error {
		<-ctx.Done()
		return ctx.Err()
	})
	f.e.mu.Lock()
	expiry := time.Now().Add(30 * time.Millisecond)
	f.e.state.LoginGrants[g.ID].ExpiresAt = &expiry
	f.e.mu.Unlock()
	r, err := f.e.StartLogin(f.actor, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	awaitLogin(t, f, r.ID, "expired")
	if err := f.e.ResumeLogin(r.ID); err == nil {
		t.Fatal("expired resume")
	}
	if _, err := f.e.StartLogin(f.actor, g.ID); err == nil {
		t.Fatal("expired grant")
	}
}
func TestControlledLoginHTTPBoundaries(t *testing.T) {
	f, a, g := loginFixture(t, func(ctx context.Context, in LoginInput, emit func(LoginEvent) error, resume <-chan struct{}) error {
		if err := emit(LoginEvent{Status: "human_required"}); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	})
	op := randomToken()
	s := httptest.NewServer(Handler(f.e, op, "", http.NotFoundHandler()))
	defer s.Close()
	request := func(method, path, token, body string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(method, s.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := s.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	for _, path := range []string{"/admin/web-accounts", "/admin/web-accounts/" + a.ID + "/review-login", "/admin/login-grants", "/admin/logins/x/resume"} {
		if status, _ := request("POST", path, f.token, `{}`); status != 401 {
			t.Fatal("agent admin access")
		}
	}
	status, b := request("POST", "/admin/web-accounts", op, `{"pasted-secret-as-field":"x"}`)
	if status != 400 || strings.Contains(b, "pasted-secret") {
		t.Fatal("decoder reflected input")
	}
	status, b = request("POST", "/v1/logins", f.token, `{"grant_id":"`+g.ID+`","password":"fake"}`)
	if status != 400 {
		t.Fatal("agent supplied credentials")
	}
	status, b = request("POST", "/v1/logins", f.token, `{"grant_id":"`+g.ID+`"}`)
	if status != 202 {
		t.Fatal(status, b)
	}
	var r LoginRun
	json.Unmarshal([]byte(b), &r)
	awaitLogin(t, f, r.ID, "human_required")
	if status, _ := request("POST", "/admin/logins/"+r.ID+"/resume", f.token, `{}`); status != 401 {
		t.Fatal("agent resumed challenge")
	}
	if status, _ := request("DELETE", "/admin/web-accounts/"+a.ID, op, ""); status != 200 {
		t.Fatal("delete failed")
	}
	awaitLogin(t, f, r.ID, "cancelled")
}

func waitLoginWorker(t *testing.T, e *Engine) {
	t.Helper()
	e.loginWG.Wait()
}

func TestUnlimitedLoginExplicitOptInAndLegacyBudget(t *testing.T) {
	f, a, old := loginFixture(t, func(ctx context.Context, in LoginInput, emit func(LoginEvent) error, resume <-chan struct{}) error {
		return emit(LoginEvent{Status: "completed", Authenticated: true})
	})
	for _, tc := range []struct {
		uses      int
		unlimited bool
	}{{0, false}, {-1, true}, {1, true}, {21, false}} {
		if _, err := f.e.CreateLoginGrantWithLimit(a.ID, f.actor, "", 0, tc.uses, tc.unlimited); err == nil {
			t.Fatal("ambiguous use limit accepted", tc)
		}
	}
	g, err := f.e.CreateLoginGrantWithLimit(a.ID, f.actor, "/vps/12345/manage", 0, 0, true)
	if err != nil || !g.UnlimitedUses || g.ExpiresAt != nil {
		t.Fatal("explicit unlimited rejected", err)
	}
	if _, err = f.e.StartLogin("other", g.ID); err == nil {
		t.Fatal("cross-agent unlimited grant")
	}
	if f.e.state.LoginGrants[old.ID].UnlimitedUses {
		t.Fatal("legacy grant upgraded")
	}
	f.e.state.LoginGrants[old.ID].Remaining = 0
	if _, err = f.e.StartLogin(f.actor, old.ID); err == nil {
		t.Fatal("exhausted grant upgraded")
	}
	for _, path := range []string{"/vps/12345/manage/reboot", "/vps/12345/manage?delete=1", "/compute/12345/manage"} {
		if _, err = f.e.CreateLoginGrantWithLimit(a.ID, f.actor, path, 0, 0, true); err == nil {
			t.Fatal("overbroad manage scope", path)
		}
	}
}

func TestUnlimitedLoginThrottlePersistenceAndRevocation(t *testing.T) {
	runner := func(ctx context.Context, in LoginInput, emit func(LoginEvent) error, resume <-chan struct{}) error {
		return emit(LoginEvent{Status: "completed", Authenticated: true})
	}
	f, a, _ := loginFixture(t, runner)
	g, err := f.e.CreateLoginGrantWithLimit(a.ID, f.actor, "/vps/12345/manage", 0, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		r, err := f.e.StartLogin(f.actor, g.ID)
		if err != nil {
			t.Fatal(err)
		}
		if r.ExpiresAt.Sub(r.CreatedAt) != 10*time.Minute {
			t.Fatal("unbounded run")
		}
		waitLoginWorker(t, f.e)
		if r, _ := f.e.GetLogin(f.actor, r.ID); r.Status != "completed" {
			t.Fatal(r.Status)
		}
		if _, err = f.e.StartLogin(f.actor, g.ID); err == nil {
			t.Fatal("cooldown bypass")
		}
		other, err := f.e.CreateLoginGrant(a.ID, f.actor, "", 0, 1)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.e.StartLogin(f.actor, other.ID); err == nil {
			t.Fatal("second grant bypassed account cooldown")
		}
		f.e.CloseLogins()
		restored, err := NewEngine(f.e.store, false)
		if err != nil {
			t.Fatal(err)
		}
		f.e = restored
		restored.SetLoginRunner(runner)
		t.Cleanup(restored.CloseLogins)
		if !restored.state.LoginGrants[g.ID].UnlimitedUses || restored.state.LoginGrants[g.ID].Remaining != 0 {
			t.Fatal("unlimited changed across restart")
		}
		if restored.account(a.ID).LoginNeedsReview {
			t.Fatal("successful login left paused")
		}
		if _, err = restored.StartLogin(f.actor, g.ID); err == nil {
			t.Fatal("restart cleared cooldown")
		}
		next := *restored.account(a.ID).NextLoginAt
		restored.now = func() time.Time { return next.Add(time.Second) }
	}
	if err = f.e.RevokeLoginGrant(g.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.e.StartLogin(f.actor, g.ID); err == nil {
		t.Fatal("revoked unlimited grant accepted")
	}
}

func TestLoginFailureAndCrashPauseSurviveRestart(t *testing.T) {
	for _, status := range []string{"login_failed", "adapter_mismatch", "network_error", "crash"} {
		t.Run(status, func(t *testing.T) {
			runner := func(ctx context.Context, in LoginInput, emit func(LoginEvent) error, resume <-chan struct{}) error {
				if status == "crash" {
					return errors.New("fake worker failure")
				}
				return emit(LoginEvent{Status: status})
			}
			f, a, _ := loginFixture(t, runner)
			g, err := f.e.CreateLoginGrantWithLimit(a.ID, f.actor, "", 0, 0, true)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.e.StartLogin(f.actor, g.ID); err != nil {
				t.Fatal(err)
			}
			waitLoginWorker(t, f.e)
			f.e.CloseLogins()
			restored, err := NewEngine(f.e.store, false)
			if err != nil {
				t.Fatal(err)
			}
			f.e = restored
			restored.SetLoginRunner(runner)
			t.Cleanup(restored.CloseLogins)
			next := *restored.account(a.ID).NextLoginAt
			restored.now = func() time.Time { return next.Add(time.Second) }
			if _, err = restored.StartLogin(f.actor, g.ID); err == nil {
				t.Fatal("failed login retried after restart")
			}
			// Account edits cannot clear execution-owned pause state.
			if _, err = restored.PutWebAccount(WebAccount{ID: a.ID, Name: "Renamed fake", Origin: CloudConeOrigin, Enabled: true}); err != nil {
				t.Fatal(err)
			}
			if !restored.account(a.ID).LoginNeedsReview {
				t.Fatal("account edit cleared pause")
			}
			dir := restored.store.dir
			restored.store.dir = filepath.Join(t.TempDir(), "missing")
			if err = restored.ReviewAccountLogin(a.ID); err == nil || !restored.account(a.ID).LoginNeedsReview {
				t.Fatal("failed review save opened account")
			}
			restored.store.dir = dir
			if err = restored.ReviewAccountLogin(a.ID); err != nil {
				t.Fatal(err)
			}
			if !restored.account(a.ID).NextLoginAt.Equal(next) {
				t.Fatal("review reset cooldown")
			}
		})
	}
}

func TestUnlimitedLoginReservationRollbackAndConcurrentRevoke(t *testing.T) {
	f, a, _ := loginFixture(t, func(ctx context.Context, in LoginInput, emit func(LoginEvent) error, resume <-chan struct{}) error {
		<-ctx.Done()
		return ctx.Err()
	})
	g, err := f.e.CreateLoginGrantWithLimit(a.ID, f.actor, "", 0, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	dir := f.e.store.dir
	f.e.store.dir = filepath.Join(t.TempDir(), "missing")
	if _, err = f.e.StartLogin(f.actor, g.ID); err == nil {
		t.Fatal("dispatch without persisted guard")
	}
	if f.e.account(a.ID).LoginNeedsReview || f.e.account(a.ID).NextLoginAt != nil {
		t.Fatal("failed dispatch guard not rolled back")
	}
	f.e.store.dir = dir
	var winners atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.e.StartLogin(f.actor, g.ID); err == nil {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatal("concurrent unlimited starts", winners.Load())
	}
	if err = f.e.ReviewAccountLogin(a.ID); err == nil {
		t.Fatal("review allowed while browser active")
	}
	if err = f.e.RevokeLoginGrant(g.ID); err != nil {
		t.Fatal(err)
	}
	waitLoginWorker(t, f.e)
	if !f.e.account(a.ID).LoginNeedsReview {
		t.Fatal("interrupted login not paused")
	}
	// Persisted reservation also blocks retry after process death before any completion event.
	restored, err := NewEngine(f.e.store, false)
	if err != nil {
		t.Fatal(err)
	}
	if !restored.account(a.ID).LoginNeedsReview {
		t.Fatal("restart lost interrupted guard")
	}
}

func TestNoExpiryGrantSurvivesRestartWithBudgetAndRevocation(t *testing.T) {
	runner := func(ctx context.Context, in LoginInput, emit func(LoginEvent) error, resume <-chan struct{}) error {
		if in.Deadline.Sub(time.Now()) > 10*time.Minute {
			t.Error("unbounded worker lifetime")
		}
		return emit(LoginEvent{Status: "completed", Authenticated: true})
	}
	f, a, _ := loginFixture(t, runner)
	g, err := f.e.CreateLoginGrant(a.ID, f.actor, "", 0, 2)
	if err != nil || g.ExpiresAt != nil {
		t.Fatal("no-expiry grant rejected", err)
	}
	f.e.CloseLogins()
	restored, err := NewEngine(f.e.store, false)
	if err != nil {
		t.Fatal(err)
	}
	restored.SetLoginRunner(runner)
	f.e = restored
	t.Cleanup(restored.CloseLogins)
	if got := restored.LoginGrants(f.actor); len(got) != 2 {
		t.Fatal("grants lost on restart")
	}
	r, err := restored.StartLogin(f.actor, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	awaitLogin(t, f, r.ID, "completed")
	restored.CloseLogins()
	restored, err = NewEngine(f.e.store, false)
	if err != nil {
		t.Fatal(err)
	}
	f.e = restored
	restored.SetLoginRunner(runner)
	t.Cleanup(restored.CloseLogins)
	if restored.state.LoginGrants[g.ID].Remaining != 1 {
		t.Fatal("budget reset across restart")
	}
	if err := restored.RevokeLoginGrant(g.ID); err != nil {
		t.Fatal(err)
	}
	last, err := NewEngine(restored.store, false)
	if err != nil {
		t.Fatal(err)
	}
	last.SetLoginRunner(runner)
	t.Cleanup(last.CloseLogins)
	if _, err = last.StartLogin(f.actor, g.ID); err == nil {
		t.Fatal("revoked grant resurrected")
	}
}
func TestPersistentGrantConfigInvalidationAndSaveRollback(t *testing.T) {
	f, a, g := loginFixture(t, func(ctx context.Context, in LoginInput, emit func(LoginEvent) error, resume <-chan struct{}) error {
		return emit(LoginEvent{Status: "completed", Authenticated: true})
	})
	originalDir := f.e.store.dir
	f.e.store.dir = filepath.Join(t.TempDir(), "missing")
	if _, err := f.e.CreateLoginGrant(a.ID, f.actor, "", 0, 2); err == nil {
		t.Fatal("failed save created grant")
	}
	if len(f.e.state.LoginGrants) != 1 {
		t.Fatal("failed grant retained")
	}
	if err := f.e.RevokeLoginGrant(g.ID); err == nil || f.e.state.LoginGrants[g.ID].Revoked {
		t.Fatal("failed revoke not rolled back")
	}
	f.e.store.dir = originalDir
	if _, _, err := f.e.PutAgent(f.actor, "Updated label", true); err != nil {
		t.Fatal(err)
	}
	restored, err := NewEngine(f.e.store, false)
	if err != nil {
		t.Fatal(err)
	}
	restored.SetLoginRunner(f.e.loginRunner)
	t.Cleanup(restored.CloseLogins)
	if !restored.state.LoginGrants[g.ID].Revoked {
		t.Fatal("configuration invalidation lost on reload")
	}
	if _, err := restored.StartLogin(f.actor, g.ID); err == nil {
		t.Fatal("invalidated grant executed after restart")
	}
}
