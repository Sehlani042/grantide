package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHTTPAgentCannotAdminAndOriginChecks(t *testing.T) {
	f := setup(t, "approval")
	operator := randomToken()
	server := httptest.NewUnstartedServer(nil)
	server.Config.Handler = Handler(f.e, operator, server.Listener.Addr().String(), http.NotFoundHandler())
	server.Start()
	defer server.Close()
	for _, tc := range []struct {
		path, token, origin, host string
		status                    int
	}{{"/admin/state", f.token, "", "", 401}, {"/admin/state", operator, "https://evil.test", "", 403}, {"/admin/state", operator, "", "evil.test", 403}, {"/admin/state", operator, "", "", 200}, {"/v1/requests/unknown", f.token, "", "", 404}} {
		r, _ := http.NewRequest("GET", server.URL+tc.path, nil)
		r.Header.Set("Authorization", "Bearer "+tc.token)
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		if tc.host != "" {
			r.Host = tc.host
		}
		resp, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Fatal(tc, resp.StatusCode, string(b))
		}
		if strings.Contains(string(b), "fake-upstream-secret") {
			t.Fatal("secret in API")
		}
	}
}

func TestEndToEndApprovalBeforeCredentialForward(t *testing.T) {
	var calls atomic.Int32
	var gotBody, gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer upstream.Close()
	f := setup(t, "approval")
	f.e.executor = executeHTTP
	_, err := f.e.PutService(Service{ID: "api", Name: "Fake", Origin: upstream.URL, AllowPrivate: true, Enabled: true, AuthHeader: "Authorization", AuthPrefix: "Bearer ", Secret: "fake-only"}, false)
	if err != nil {
		t.Fatal(err)
	}
	operator := randomToken()
	server := httptest.NewServer(Handler(f.e, operator, "", http.NotFoundHandler()))
	defer server.Close()
	send := func(path, token string, input any) Request {
		t.Helper()
		b, _ := json.Marshal(input)
		req, _ := http.NewRequest("POST", server.URL+path, bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out Request
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	r := send("/v1/requests", f.token, exampleCall())
	if r.Status != "pending" || calls.Load() != 0 {
		t.Fatal("forwarded before approval", r)
	}
	r = send("/admin/requests/"+r.ID+"/decision", operator, map[string]string{"decision": "once"})
	if r.Status != "completed" || calls.Load() != 1 || gotBody != exampleCall().Body || gotAuth != "Bearer fake-only" {
		t.Fatal("wrong forwarded request", r, gotBody, gotAuth)
	}
}

func TestExecutorBlocksPrivateTargetsRedirectsAndRedacts(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1); fmt.Fprint(w, "private") }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer redirect.Close()
	c := Call{Method: "GET", Path: "/"}
	s := Service{Origin: target.URL}
	if _, err := executeHTTP(context.Background(), s, c); err == nil {
		t.Fatal("private network allowed")
	}
	if targetCalls.Load() != 0 {
		t.Fatal("reached private target")
	}
	s = Service{Origin: redirect.URL, AllowPrivate: true, AuthHeader: "Authorization", Secret: "fake-secret"}
	if _, err := executeHTTP(context.Background(), s, c); err == nil {
		t.Fatal("redirect followed")
	}
	if targetCalls.Load() != 0 {
		t.Fatal("redirect destination reached")
	}
	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, r.Header.Get("Authorization")) }))
	defer echo.Close()
	s.Origin = echo.URL
	r, err := executeHTTP(context.Background(), s, c)
	if err != nil || strings.Contains(r.Body, "fake-secret") {
		t.Fatal("credential echoed", r, err)
	}
}
func TestPublicAddressPolicy(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.100.100.200", "0.0.0.0", "::1", "::ffff:127.0.0.1", "fc00::1", "fe80::1", "64:ff9b::a00:1"} {
		if publicIP(netip.MustParseAddr(ip)) {
			t.Fatal("reserved IP allowed", ip)
		}
	}
	if !publicIP(netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("public IP blocked")
	}
}
func TestUnknownFieldsAndOversizeHTTPInput(t *testing.T) {
	f := setup(t, "auto")
	h := Handler(f.e, randomToken(), "", http.NotFoundHandler())
	for _, body := range []string{`{"service_id":"api","method":"POST","path":"/v1","origin":"http://evil"}`, `{}` + `{}`, strings.Repeat("x", 129<<10)} {
		req := httptest.NewRequest("POST", "http://localhost/v1/requests", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+f.token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatal("invalid input accepted", w.Code)
		}
	}
	if f.calls.Load() != 0 {
		t.Fatal("invalid input reached upstream")
	}
}
