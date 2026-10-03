package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestOperatorPasswordPersistenceExpiryAndRevocation(t *testing.T) {
	dir := t.TempDir()
	password := "fake-console-password-42"
	if err := SetOperatorPassword(dir, password); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "operator-auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(password)) {
		t.Fatal("plaintext password stored")
	}
	info, _ := os.Stat(filepath.Join(dir, "operator-auth.json"))
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal("auth file permissions")
	}
	auth, err := OpenOperatorAuth(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	auth.now = func() time.Time { return now }
	if _, _, err := auth.Login("wrong", "", "operator", true); err != errOperatorCredentials {
		t.Fatal("wrong password accepted")
	}
	token, expiry, err := auth.Login(password, "", "operator", true)
	if err != nil {
		t.Fatal(err)
	}
	if !auth.Authorized(token) || expiry.Sub(now) != 30*24*time.Hour {
		t.Fatal("remembered session")
	}
	raw, _ = os.ReadFile(auth.path)
	if bytes.Contains(raw, []byte(token)) {
		t.Fatal("raw session token stored")
	}
	reloaded, err := OpenOperatorAuth(dir)
	if err != nil {
		t.Fatal(err)
	}
	reloaded.now = func() time.Time { return now }
	if !reloaded.Authorized(token) {
		t.Fatal("restart lost remembered session")
	}
	if reloaded.Authorized(strings.Repeat("f", 64)) {
		t.Fatal("tampered cookie accepted")
	}
	now = expiry
	if reloaded.Authorized(token) {
		t.Fatal("expired session accepted")
	}
	now = expiry.Add(-time.Hour)
	if err := reloaded.Logout(token); err != nil {
		t.Fatal(err)
	}
	again, err := OpenOperatorAuth(dir)
	if err != nil {
		t.Fatal(err)
	}
	if again.Authorized(token) {
		t.Fatal("logged-out cookie survives restart")
	}
	fresh, _, err := again.Login(password, "", "operator", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetOperatorPassword(dir, "another-fake-password"); err != nil {
		t.Fatal(err)
	}
	changed, _ := OpenOperatorAuth(dir)
	if changed.Authorized(fresh) {
		t.Fatal("password change preserved old session")
	}
	if _, _, err := changed.Login(password, "", "operator", true); err != errOperatorCredentials {
		t.Fatal("old password accepted")
	}
}

func TestOperatorThrottleAndPersistenceFailure(t *testing.T) {
	dir := t.TempDir()
	if err := SetOperatorPassword(dir, "fake-password"); err != nil {
		t.Fatal(err)
	}
	a, _ := OpenOperatorAuth(dir)
	now := time.Now()
	a.now = func() time.Time { return now }
	for i := 0; i < 5; i++ {
		if _, _, err := a.Login("incorrect", "", "operator", true); err != errOperatorCredentials {
			t.Fatal(err)
		}
	}
	if _, _, err := a.Login("fake-password", "", "operator", true); err != errOperatorThrottle {
		t.Fatal("no throttle")
	}
	now = now.Add(time.Minute)
	token, _, err := a.Login("fake-password", "", "operator", false)
	if err != nil {
		t.Fatal(err)
	}
	oldPath := a.path
	a.path = filepath.Join(dir, "missing", "record")
	if _, _, err := a.Login("fake-password", "", "operator", true); err == nil {
		t.Fatal("failed save reported success")
	}
	if len(a.record.Sessions) != 1 {
		t.Fatal("failed login save mutated sessions")
	}
	if err := a.Logout(token); err == nil {
		t.Fatal("failed logout save accepted")
	}
	if !a.Authorized(token) {
		t.Fatal("failed logout altered session")
	}
	a.path = oldPath
	if err := os.WriteFile(filepath.Join(dir, "server.lock"), []byte("fake"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SetOperatorPassword(dir, "other-password"); err == nil {
		t.Fatal("changed password while server running")
	}
}

func TestOperatorCookieHTTPBoundaries(t *testing.T) {
	f := setup(t, "approval")
	dir := t.TempDir()
	password := "fake-browser-password"
	if err := SetOperatorPassword(dir, password); err != nil {
		t.Fatal(err)
	}
	auth, _ := OpenOperatorAuth(dir)
	operator := randomToken()
	h := Handler(f.e, operator, "127.0.0.1:12345", http.NotFoundHandler(), auth)
	send := func(method, path, body string, cookie *http.Cookie, bearer, origin string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "http://127.0.0.1:12345"+path, strings.NewReader(body))
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	body, _ := json.Marshal(map[string]any{"password": password, "remember": true})
	if w := send("POST", "/admin/login", string(body), nil, "", "https://evil.test"); w.Code != 403 {
		t.Fatal("cross-origin login", w.Code)
	}
	w := send("POST", "/admin/login", string(body), nil, "", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("session cookie missing")
	}
	c := cookies[0]
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/admin" || c.Domain != "" || c.MaxAge != 30*24*60*60 {
		t.Fatal("cookie attributes")
	}
	if strings.Contains(w.Body.String(), password) || strings.Contains(w.Body.String(), c.Value) || strings.Contains(w.Body.String(), operator) {
		t.Fatal("login response leaked secret")
	}
	if w := send("GET", "/admin/state", "", c, "", ""); w.Code != 200 {
		t.Fatal("cookie admin failed", w.Code)
	}
	if w := send("GET", "/admin/state", "", nil, f.token, ""); w.Code != 401 {
		t.Fatal("agent reached admin")
	}
	if w := send("GET", "/v1/login-grants", "", c, "", ""); w.Code != 401 {
		t.Fatal("admin cookie reached agent")
	}
	if w := send("POST", "/admin/logout", "{}", c, "", "https://evil.test"); w.Code != 403 {
		t.Fatal("cross-origin logout")
	}
	if !auth.Authorized(c.Value) {
		t.Fatal("cross-origin logout revoked cookie")
	}
	if w := send("POST", "/admin/logout", "{}", c, "", ""); w.Code != 200 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout failed")
	}
	if w := send("GET", "/admin/state", "", c, "", ""); w.Code != 401 {
		t.Fatal("revoked cookie accepted")
	}
	if w := send("GET", "/admin/state", "", nil, operator, ""); w.Code != 200 {
		t.Fatal("native bearer broken")
	}
	body, _ = json.Marshal(map[string]any{"password": password, "remember": false})
	w = send("POST", "/admin/login", string(body), nil, "", "")
	if w.Code != 200 {
		t.Fatal("session-only login failed")
	}
	c = w.Result().Cookies()[0]
	if c.MaxAge != 0 || !c.Expires.IsZero() {
		t.Fatal("session-only persisted cookie")
	}
	if w := send("POST", "/admin/login", `{"secret-as-field-name":"anything"}`, nil, "", ""); w.Code != 400 || strings.Contains(w.Body.String(), "secret-as-field-name") {
		t.Fatal("decoder leaked login input")
	}
}

func TestOperatorCookieNamesDoNotCollideAcrossLocalInstances(t *testing.T) {
	f := setup(t, "approval")
	var handlers []http.Handler
	var cookies []*http.Cookie
	for range 2 {
		auth, err := OpenOperatorAuth(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		operator := randomToken()
		h := Handler(f.e, operator, "localhost", http.NotFoundHandler(), auth)
		body, _ := json.Marshal(map[string]any{"token": operator, "remember": true})
		req := httptest.NewRequest("POST", "http://localhost/admin/login", bytes.NewReader(body))
		out := httptest.NewRecorder()
		h.ServeHTTP(out, req)
		if out.Code != 200 {
			t.Fatal(out.Code)
		}
		handlers = append(handlers, h)
		cookies = append(cookies, out.Result().Cookies()[0])
	}
	if cookies[0].Name == cookies[1].Name {
		t.Fatal("localhost cookies collide across ports")
	}
	for _, h := range handlers {
		req := httptest.NewRequest("GET", "http://localhost/admin/state", nil)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		out := httptest.NewRecorder()
		h.ServeHTTP(out, req)
		if out.Code != 200 {
			t.Fatal("another instance evicted the session")
		}
	}
}
