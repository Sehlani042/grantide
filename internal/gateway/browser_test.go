package gateway

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func browserFixture(t *testing.T) (*fixture, BrowserTarget) {
	t.Helper()
	f := setup(t, "approval")
	_, err := f.e.PutBrowserSite(BrowserSite{ID: "cloud", Name: "Fake cloud", LoginURL: "https://console.example.com/account", AgentID: f.actor, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return f, BrowserTarget{SiteID: "cloud", Browser: "Codex profile: test-chat", TabID: "tab-1", ThreadID: "test-chat", ThreadTitle: "Fake cloud review"}
}
func TestBrowserHandoffRequiresHumanAndOriginalContext(t *testing.T) {
	f, target := browserFixture(t)
	r, err := f.e.RequestBrowserHandoff(f.actor, target)
	if err != nil {
		t.Fatal(err)
	}
	same, err := f.e.RequestBrowserHandoff(f.actor, target)
	if err != nil || r.ID != same.ID {
		t.Fatal("duplicate handoff")
	}
	if _, err = f.e.VerifyBrowserHandoff(f.actor, r.ID, target, "https://console.example.com", true); err == nil {
		t.Fatal("agent bypassed human")
	}
	if _, ok := f.e.GetBrowserHandoff("other", r.ID); ok {
		t.Fatal("cross-agent read")
	}
	if err = f.e.CancelBrowserHandoff("other", r.ID); err == nil {
		t.Fatal("cross-agent cancel")
	}
	if _, err = f.e.BrowserDecision(r.ID, "done"); err != nil {
		t.Fatal(err)
	}
	if out, _ := f.e.GetBrowserHandoff(f.actor, r.ID); out.Status != "verification_required" {
		t.Fatal("human confirmation treated as login proof")
	}
	for _, change := range []string{"tab", "browser", "thread", "site"} {
		wrong := target
		switch change {
		case "tab":
			wrong.TabID = "other"
		case "browser":
			wrong.Browser = "other"
		case "thread":
			wrong.ThreadID = "other"
		case "site":
			wrong.SiteID = "other"
		}
		if _, err = f.e.VerifyBrowserHandoff(f.actor, r.ID, wrong, "https://console.example.com", true); err == nil {
			t.Fatal("wrong context accepted", change)
		}
	}
	for _, origin := range []string{"https://evil.example.com", "https://console.example.com.evil.com", "http://console.example.com", "https://console.example.com/?code=secret", "https://console.example.com/path"} {
		if _, err = f.e.VerifyBrowserHandoff(f.actor, r.ID, target, origin, true); err == nil {
			t.Fatal("wrong origin accepted", origin)
		}
	}
	var passed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := f.e.VerifyBrowserHandoff(f.actor, r.ID, target, "https://console.example.com", true)
			if err == nil && out.Status == "verified" {
				passed.Add(1)
			}
		}()
	}
	wg.Wait()
	if passed.Load() != 1 || f.calls.Load() != 0 {
		t.Fatal("verification replay or upstream execution")
	}
}
func TestBrowserHandoffLifecycle(t *testing.T) {
	for _, action := range []string{"expire", "reject", "cancel", "configuration", "failed-login", "save-failure"} {
		t.Run(action, func(t *testing.T) {
			f, target := browserFixture(t)
			now := time.Now()
			f.e.now = func() time.Time { return now }
			r, err := f.e.RequestBrowserHandoff(f.actor, target)
			if err != nil {
				t.Fatal(err)
			}
			switch action {
			case "expire":
				now = now.Add(BrowserHandoffTTL)
			case "reject":
				_, err = f.e.BrowserDecision(r.ID, "reject")
			case "cancel":
				err = f.e.CancelBrowserHandoff(f.actor, r.ID)
			case "configuration":
				_, _, err = f.e.PutAgent(f.actor, "Disabled", false)
			case "failed-login":
				_, err = f.e.BrowserDecision(r.ID, "done")
				if err == nil {
					_, err = f.e.VerifyBrowserHandoff(f.actor, r.ID, target, "https://console.example.com", false)
				}
			case "save-failure":
				f.e.store.dir = filepath.Join(t.TempDir(), "missing")
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.e.BrowserDecision(r.ID, "done"); err == nil {
				t.Fatal("inactive handoff accepted")
			}
			if _, err = f.e.VerifyBrowserHandoff(f.actor, r.ID, target, "https://console.example.com", true); err == nil {
				t.Fatal("inactive handoff verified")
			}
			if f.calls.Load() != 0 {
				t.Fatal("website operation executed")
			}
		})
	}
}
func TestBrowserWebsiteAllowlistAndValidation(t *testing.T) {
	f, target := browserFixture(t)
	other, _, err := f.e.PutAgent("", "Other", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.e.RequestBrowserHandoff(other.ID, target); err == nil {
		t.Fatal("wrong agent requested website")
	}
	for _, raw := range []string{"javascript:alert(1)", "https://user:pass@example.com", "https://example.com/?token=secret", "https://example.com/#secret", "http://example.com/", "https://example.com\\@evil.test"} {
		if _, err = f.e.PutBrowserSite(BrowserSite{ID: "bad", Name: "bad", LoginURL: raw, AgentID: "*", Enabled: true}); err == nil {
			t.Fatal("unsafe URL accepted", raw)
		}
	}
	r, err := f.e.RequestBrowserHandoff(f.actor, target)
	if err != nil {
		t.Fatal(err)
	}
	changed := target
	changed.ThreadID = "different-thread"
	if _, err = f.e.RequestBrowserHandoff(f.actor, changed); err == nil {
		t.Fatal("same tab concurrently claimed")
	}
	if _, err = f.e.BrowserDecision(r.ID, "done"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.e.PutBrowserSite(BrowserSite{ID: "cloud", Name: "cloud", LoginURL: "https://console.example.com", AgentID: f.actor, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if out, _ := f.e.GetBrowserHandoff(f.actor, r.ID); out.Status != "invalidated" {
		t.Fatal("awaiting verification survived config change")
	}
}
func TestBrowserHTTPHumanAgentSeparation(t *testing.T) {
	f, target := browserFixture(t)
	operator := randomToken()
	h := Handler(f.e, operator, "", http.NotFoundHandler())
	r, err := f.e.RequestBrowserHandoff(f.actor, target)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path, token, body string
		status            int
	}{
		{"/admin/browser-handoffs/" + r.ID + "/decision", f.token, `{"decision":"done"}`, 401},
		{"/admin/browser-handoffs/" + r.ID + "/decision", operator, `{"decision":"done"}`, 200},
		{"/v1/browser-handoffs/" + r.ID + "/verify", operator, `{}`, 401},
		{"/v1/browser-handoffs", f.token, `{"site_id":"cloud","browser":"test","tab_id":"1","password":"fake"}`, 400},
	} {
		req := httptest.NewRequest("POST", "http://localhost"+tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+tc.token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
