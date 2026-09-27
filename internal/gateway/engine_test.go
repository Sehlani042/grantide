package gateway

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fixture struct {
	e            *Engine
	actor, token string
	calls        atomic.Int32
}

func setup(t *testing.T, mode string) *fixture {
	t.Helper()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e, err := NewEngine(store, false)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{e: e}
	a, token, err := e.PutAgent("", "Test agent", true)
	if err != nil {
		t.Fatal(err)
	}
	f.actor, f.token = a.ID, token
	_, err = e.PutService(Service{ID: "api", Name: "Test API", Origin: "https://api.example.com", Enabled: true, AuthHeader: "Authorization", AuthPrefix: "Bearer ", Secret: "fake-upstream-secret"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if mode != "" {
		_, err = e.PutRule(Rule{ID: "test", Name: "Test policy", AgentID: "*", ServiceID: "api", Methods: []string{"POST"}, PathPrefix: "/v1", Mode: mode, Enabled: true, LeaseSeconds: 60, MaxUses: 3})
		if err != nil {
			t.Fatal(err)
		}
	}
	e.executor = func(ctx context.Context, s Service, c Call) (*Result, error) {
		f.calls.Add(1)
		return &Result{Status: 200, Body: c.Body}, nil
	}
	return f
}
func exampleCall() Call {
	return Call{ServiceID: "api", Method: "POST", Path: "/v1/tasks", Query: map[string]string{"env": "test"}, Body: `{"task":"safe"}`}
}
func (f *fixture) submit(t *testing.T) Request {
	t.Helper()
	r, err := f.e.Submit(context.Background(), f.actor, exampleCall())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestDefaultDenyAndPrecedence(t *testing.T) {
	f := setup(t, "")
	r := f.submit(t)
	if r.Status != "denied" || f.calls.Load() != 0 {
		t.Fatal(r)
	}
	_, err := f.e.PutRule(Rule{ID: "auto", Name: "Allow", AgentID: f.actor, ServiceID: "api", Methods: []string{"POST"}, PathPrefix: "/v1/tasks", Mode: "auto", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.e.PutRule(Rule{ID: "deny", Name: "Block", AgentID: "*", ServiceID: "api", Methods: []string{"POST"}, PathPrefix: "/", Mode: "deny", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	r = f.submit(t)
	if r.Status != "denied" || f.calls.Load() != 0 {
		t.Fatal("broad explicit deny did not win", r)
	}
}
func TestApprovalHoldsExactRequestAndIsConsumedOnce(t *testing.T) {
	f := setup(t, "approval")
	c := exampleCall()
	r, err := f.e.Submit(context.Background(), f.actor, c)
	if err != nil {
		t.Fatal(err)
	}
	c.Query["env"] = "changed"
	if r.Status != "pending" || f.calls.Load() != 0 {
		t.Fatal("forwarded before approval")
	}
	stored, _ := f.e.GetRequest(f.actor, r.ID)
	if stored.Call.Query["env"] != "test" {
		t.Fatal("caller mutated pending request")
	}
	var exact atomic.Bool
	f.e.executor = func(_ context.Context, s Service, c Call) (*Result, error) {
		f.calls.Add(1)
		exact.Store(c.Body == `{"task":"safe"}` && c.Query["env"] == "test" && s.Secret == "fake-upstream-secret")
		return &Result{Status: 200}, nil
	}
	var wg sync.WaitGroup
	var success atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.e.Decide(context.Background(), r.ID, "once"); err == nil {
				success.Add(1)
			}
		}()
	}
	wg.Wait()
	if success.Load() != 1 || f.calls.Load() != 1 || !exact.Load() {
		t.Fatalf("success=%d calls=%d exact=%t", success.Load(), f.calls.Load(), exact.Load())
	}
}
func TestRejectCancelExpireAndMutationNeverDispatch(t *testing.T) {
	for _, scenario := range []string{"reject", "cancel", "expire", "rule", "service", "agent"} {
		t.Run(scenario, func(t *testing.T) {
			f := setup(t, "approval")
			r := f.submit(t)
			switch scenario {
			case "reject":
				_, _ = f.e.Decide(context.Background(), r.ID, "reject")
			case "cancel":
				_ = f.e.Cancel(f.actor, r.ID)
			case "expire":
				now := time.Now().Add(PendingTTL)
				f.e.now = func() time.Time { return now }
			case "rule":
				rule := f.e.Snapshot().Rules[0]
				rule.Mode = "auto"
				_, _ = f.e.PutRule(rule)
			case "service":
				s := f.e.Snapshot().Services[0]
				s.Enabled = false
				_, _ = f.e.PutService(s, false)
			case "agent":
				_, _, _ = f.e.PutAgent(f.actor, "Disabled", false)
			}
			if _, err := f.e.Decide(context.Background(), r.ID, "once"); err == nil {
				t.Fatal("stale request approved")
			}
			if f.calls.Load() != 0 {
				t.Fatal("unexpected upstream call")
			}
		})
	}
}
func TestLeaseScopeExpiryAndConcurrentLimit(t *testing.T) {
	f := setup(t, "lease")
	r := f.submit(t)
	if _, err := f.e.Decide(context.Background(), r.ID, "lease"); err != nil {
		t.Fatal(err)
	}
	a, _, _ := f.e.PutAgent("", "Another agent", true) // Global revision invalidates the first grant.
	r = f.submit(t)
	_, err := f.e.Decide(context.Background(), r.ID, "lease")
	if err != nil {
		t.Fatal(err)
	}
	other, err := f.e.Submit(context.Background(), a.ID, exampleCall())
	if err != nil || other.Status != "pending" {
		t.Fatal("lease crossed agent identity", other, err)
	}
	before := f.calls.Load()
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = f.e.Submit(context.Background(), f.actor, exampleCall()) }()
	}
	wg.Wait()
	if f.calls.Load()-before != 2 {
		t.Fatalf("remaining budget was exceeded: %d", f.calls.Load()-before)
	}
	leases := f.e.Snapshot().Leases
	found := false
	for _, l := range leases {
		if !l.Revoked {
			found = true
			if l.Remaining != 0 {
				t.Fatal(l)
			}
		}
	}
	if !found {
		t.Fatal("missing grant")
	}
}
func TestLeaseExpiryRevocationAndRuleChange(t *testing.T) {
	for _, scenario := range []string{"time", "revoke", "rule"} {
		t.Run(scenario, func(t *testing.T) {
			f := setup(t, "lease")
			r := f.submit(t)
			_, _ = f.e.Decide(context.Background(), r.ID, "lease")
			switch scenario {
			case "time":
				now := time.Now().Add(61 * time.Second)
				f.e.now = func() time.Time { return now }
			case "revoke":
				_ = f.e.RevokeLease(f.e.Snapshot().Leases[0].ID)
			case "rule":
				rule := f.e.Snapshot().Rules[0]
				rule.Name = "Changed"
				_, _ = f.e.PutRule(rule)
			}
			r = f.submit(t)
			if r.Status != "pending" || f.calls.Load() != 1 {
				t.Fatal("stale lease reused", r)
			}
		})
	}
}
func TestRuleExpiryDoesNotFallThrough(t *testing.T) {
	f := setup(t, "auto")
	past := time.Now().Add(-time.Minute)
	rule := f.e.Snapshot().Rules[0]
	rule.ExpiresAt = &past
	_, _ = f.e.PutRule(rule)
	_, _ = f.e.PutRule(Rule{ID: "broad", Name: "Broad", AgentID: "*", ServiceID: "api", Methods: []string{"POST"}, PathPrefix: "/", Mode: "auto", Enabled: true})
	if r := f.submit(t); r.Status != "denied" {
		t.Fatal(r)
	}
}
func TestApprovalCannotMintLease(t *testing.T) {
	f := setup(t, "approval")
	r := f.submit(t)
	if _, err := f.e.Decide(context.Background(), r.ID, "lease"); err == nil {
		t.Fatal("lease issued from per-call rule")
	}
	if f.calls.Load() != 0 {
		t.Fatal("forwarded")
	}
}
func TestAuthAndSnapshots(t *testing.T) {
	f := setup(t, "auto")
	if f.e.Authenticate(f.token) != f.actor || f.e.Authenticate("wrong") != "" {
		t.Fatal("authentication")
	}
	r := f.submit(t)
	if _, ok := f.e.GetRequest("another", r.ID); ok {
		t.Fatal("cross-agent request exposed")
	}
	b, _ := json.Marshal(f.e.Snapshot())
	if strings.Contains(string(b), "fake-upstream-secret") || strings.Contains(string(b), tokenHash(f.token)) {
		t.Fatal("credential leak")
	}
	_, _, _ = f.e.PutAgent(f.actor, "Disabled", false)
	if f.e.Authenticate(f.token) != "" {
		t.Fatal("revoked token accepted")
	}
}
func TestEncryptedPersistenceRestartAndCorruption(t *testing.T) {
	f := setup(t, "lease")
	r := f.submit(t)
	_, _ = f.e.Decide(context.Background(), r.ID, "lease")
	path := filepath.Join(f.e.store.dir, "state.enc")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "fake-upstream-secret") {
		t.Fatal("unencrypted secret")
	}
	reloaded, err := NewEngine(f.e.store, false)
	if err != nil {
		t.Fatal(err)
	}
	s := reloaded.Snapshot()
	if len(s.Requests) != 0 || len(s.Leases) != 0 || len(s.Rules) != 1 || reloaded.Authenticate(f.token) != f.actor {
		t.Fatal("restart contract", s)
	}
	b[len(b)-1] ^= 1
	_ = os.WriteFile(path, b, 0600)
	if _, err := NewEngine(f.e.store, false); err == nil {
		t.Fatal("corrupt state accepted")
	}
}
func TestPersistenceFailureBlocksDispatchAndRollsBack(t *testing.T) {
	f := setup(t, "auto")
	before := f.e.Snapshot()
	f.e.store.dir = filepath.Join(t.TempDir(), "missing")
	if _, err := f.e.Submit(context.Background(), f.actor, exampleCall()); err == nil {
		t.Fatal("dispatch without audit")
	}
	if f.calls.Load() != 0 {
		t.Fatal("called upstream")
	}
	rule := before.Rules[0]
	rule.Mode = "deny"
	if _, err := f.e.PutRule(rule); err == nil {
		t.Fatal("mutation without persistence")
	}
	if f.e.Snapshot().Rules[0].Mode != "auto" {
		t.Fatal("failed mutation did not roll back")
	}
}
func TestPathMethodAndIdentityScope(t *testing.T) {
	r := Rule{ID: "x", AgentID: "a", ServiceID: "s", Methods: []string{"GET"}, PathPrefix: "/v1", Mode: "auto", Enabled: true}
	for _, tc := range []struct {
		actor, path, method string
		allow               bool
	}{{"a", "/v1", "GET", true}, {"a", "/v1/test", "GET", true}, {"a", "/v10", "GET", false}, {"b", "/v1", "GET", false}, {"a", "/v1", "DELETE", false}} {
		got, _ := evaluate([]Rule{r}, tc.actor, Call{ServiceID: "s", Path: tc.path, Method: tc.method}, time.Now())
		if (got.ID != "") != tc.allow {
			t.Fatal(tc, got)
		}
	}
	for _, p := range []string{"//evil.test", "/v1/../admin", "/v1/%2e%2e/admin", "/v1%2fadmin", "/v1\\admin", "/v1?x=y"} {
		if pathOK(p) {
			t.Fatal("ambiguous path accepted", p)
		}
	}
}
func TestRequestValidationAndBoundedQueue(t *testing.T) {
	f := setup(t, "approval")
	for _, c := range []Call{{Method: "CONNECT", Path: "/"}, {Method: "POST", Path: "/", Body: "not json"}, {Method: "GET", Path: "/", Body: "{}"}, {Method: "POST", Path: "/", Body: `"` + strings.Repeat("a", MaxBody) + `"`}} {
		if err := validateCall(c); err == nil {
			t.Fatal("invalid call accepted")
		}
	}
	for i := 0; i < 16; i++ {
		f.submit(t)
	}
	if _, err := f.e.Submit(context.Background(), f.actor, exampleCall()); err == nil {
		t.Fatal("unbounded pending queue")
	}
}
