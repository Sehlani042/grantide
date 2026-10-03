package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func connectInput() ConnectInput {
	return ConnectInput{Name: "Test chat", Conversation: "test-chat", Target: CloudConeOrigin + "/vps/12345/manage"}
}
func TestConnectionApprovalDispatchAndRestart(t *testing.T) {
	f, a, oldGrant := loginFixture(t, nil)
	token := randomToken()
	r, err := f.e.RequestConnection(token, connectInput())
	if err != nil {
		t.Fatal(err)
	}
	if f.e.Authenticate(token) != "" || f.e.NextExtensionLogin() != nil {
		t.Fatal("unapproved enrollment executed")
	}
	if _, ok := f.e.GetConnection(randomToken(), r.ID); ok {
		t.Fatal("other identity read request")
	}
	again, err := f.e.RequestConnection(token, connectInput())
	if err != nil || again.ID != r.ID {
		t.Fatal("duplicate request")
	}
	latest, ok := f.e.GetConnection(token, "latest")
	if !ok || latest.ID != r.ID {
		t.Fatal("owned latest request missing")
	}
	if _, ok := f.e.GetConnection(randomToken(), "latest"); ok {
		t.Fatal("latest exposed another identity")
	}
	revision := f.e.state.Revision
	approved, err := f.e.DecideConnection(r.ID, "approve", a.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if f.e.state.Revision != revision || f.e.state.LoginGrants[oldGrant.ID].Revoked {
		t.Fatal("enrollment invalidated unrelated authority")
	}
	if approved.RunID == "" || approved.Result.Status != "extension_pending" || f.e.Authenticate(token) != approved.AgentID || !f.e.websiteOnly(approved.AgentID) {
		t.Fatal("approval did not dispatch and bind identity")
	}
	if _, err = f.e.DecideConnection(r.ID, "approve", a.ID, true); err == nil {
		t.Fatal("approval replay")
	}
	data, _ := json.Marshal(f.e.Snapshot())
	if strings.Contains(string(data), token) || strings.Contains(string(data), tokenHash(token)) || strings.Contains(string(data), "FakeOnly-Pass") {
		t.Fatal("snapshot exposed secret")
	}
	if _, err = f.e.ClaimExtensionLogin(approved.RunID); err != nil {
		t.Fatal(err)
	}
	if err = f.e.CompleteExtensionLogin(approved.RunID, "completed", map[string]string{"cpu": "2 cores"}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewEngine(f.e.store, false)
	if err != nil {
		t.Fatal(err)
	}
	persisted, ok := reloaded.GetConnection(token, r.ID)
	if !ok || persisted.Status != "completed" || !persisted.Result.Authenticated || persisted.Result.Fields["cpu"] != "2 cores" {
		t.Fatal("completion lost on restart")
	}
	f.e.now = func() time.Time { return time.Now().Add(61 * time.Second) }
	next, err := f.e.RequestConnection(token, connectInput())
	if err != nil {
		t.Fatal(err)
	}
	if next.ID == r.ID || next.Status != "executing" || next.GrantID != approved.GrantID {
		t.Fatal("existing exact grant not reused")
	}
	restart, err := NewEngine(f.e.store, false)
	if err != nil {
		t.Fatal(err)
	}
	interrupted, _ := restart.GetConnection(token, next.ID)
	if interrupted.Status != "interrupted" || restart.NextExtensionLogin() != nil {
		t.Fatal("restart replayed pending execution")
	}
}
func TestConnectionDefaultDenyExpiryRollbackAndConcurrency(t *testing.T) {
	f, a, _ := loginFixture(t, nil)
	token := randomToken()
	input := connectInput()
	input.Target = "https://evil.test/"
	if _, err := f.e.RequestConnection(token, input); err == nil {
		t.Fatal("foreign origin")
	}
	r, err := f.e.RequestConnection(token, connectInput())
	if err != nil {
		t.Fatal(err)
	}
	before := len(f.e.state.Agents)
	dir := f.e.store.dir
	f.e.store.dir = t.TempDir() + "/missing"
	if _, err = f.e.DecideConnection(r.ID, "approve", a.ID, false); err == nil {
		t.Fatal("failed persistence dispatched")
	}
	f.e.store.dir = dir
	if len(f.e.state.Agents) != before || f.e.NextExtensionLogin() != nil || f.e.state.ConnectionRequests[r.ID].Status != "pending" {
		t.Fatal("failed approval was not atomic")
	}
	var wg sync.WaitGroup
	success := make(chan bool, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.e.DecideConnection(r.ID, "approve", a.ID, false)
			success <- err == nil
		}()
	}
	wg.Wait()
	close(success)
	count := 0
	for ok := range success {
		if ok {
			count++
		}
	}
	if count != 1 {
		t.Fatal("double dispatch")
	}
	other := randomToken()
	pending, _ := f.e.RequestConnection(other, connectInput())
	f.e.now = func() time.Time { return time.Now().Add(16 * time.Minute) }
	if _, err = f.e.DecideConnection(pending.ID, "approve", a.ID, true); err == nil {
		t.Fatal("expired approval")
	}
}
func TestConnectionHTTPBoundary(t *testing.T) {
	f, a, _ := loginFixture(t, nil)
	token := randomToken()
	r, _ := f.e.RequestConnection(token, connectInput())
	handler := Handler(f.e, "operator", "localhost", http.NotFoundHandler())
	req := httptest.NewRequest("POST", "http://localhost/admin/connections/"+r.ID+"/decision", strings.NewReader(`{"decision":"approve"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != 401 {
		t.Fatal("agent self approval")
	}
	req = httptest.NewRequest("POST", "http://localhost/v1/connections", strings.NewReader(`{}`))
	req.Header.Set("Origin", "https://evil.test")
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != 403 {
		t.Fatal("cross-origin enrollment")
	}
	out, err := f.e.DecideConnection(r.ID, "approve", a.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest("POST", "http://localhost/v1/requests", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+token)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != 403 || out.AgentID == "" {
		t.Fatal("website enrollment inherited HTTP access")
	}
}
