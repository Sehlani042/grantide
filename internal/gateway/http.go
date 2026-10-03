package gateway

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

func jsonResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, message string) {
	jsonResponse(w, status, map[string]string{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		fail(w, 400, "Invalid or oversized JSON input: "+err.Error())
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		fail(w, 400, "Expected exactly one JSON value")
		return false
	}
	return true
}

func Handler(e *Engine, operatorToken, expectedHost string, assets http.Handler, options ...*OperatorAuth) http.Handler {
	mux := http.NewServeMux()
	var sessions *OperatorAuth
	if len(options) > 0 {
		sessions = options[0]
	}
	// Cookies have no port scope. Isolate multiple local Grantide instances.
	cookieName := operatorCookie + "_" + tokenHash(operatorToken)[:12]
	cookieToken := func(r *http.Request) string {
		c, err := r.Cookie(cookieName)
		if err != nil {
			if legacy, e := r.Cookie(operatorCookie); e == nil && sessions.Authorized(legacy.Value) {
				return legacy.Value
			}
			return ""
		}
		return c.Value
	}
	admin := func(fn http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if (operatorToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(operatorToken)) != 1) && !sessions.Authorized(cookieToken(r)) {
				fail(w, 401, "Operator authentication required")
				return
			}
			fn(w, r)
		}
	}
	agent := func(fn func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			actor := e.Authenticate(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			if actor == "" {
				fail(w, 401, "Valid agent token required")
				return
			}
			if e.websiteOnly(actor) && !(strings.HasPrefix(r.URL.Path, "/v1/logins") || r.URL.Path == "/v1/extension-logins" || r.URL.Path == "/v1/login-grants") {
				fail(w, 403, "Website-only identity")
				return
			}
			fn(w, r, actor)
		}
	}
	if sessions != nil {
		mux.HandleFunc("POST /admin/login", func(w http.ResponseWriter, r *http.Request) {
			var in struct {
				Password string `json:"password"`
				Token    string `json:"token"`
				Remember bool   `json:"remember"`
			}
			r.Body = http.MaxBytesReader(w, r.Body, 2048)
			d := json.NewDecoder(r.Body)
			d.DisallowUnknownFields()
			if d.Decode(&in) != nil || d.Decode(new(any)) != io.EOF {
				fail(w, 400, "Invalid login input")
				return
			}
			value, expiry, err := sessions.Login(in.Password, in.Token, operatorToken, in.Remember)
			if err != nil {
				status := http.StatusInternalServerError
				if errors.Is(err, errOperatorCredentials) {
					status = http.StatusUnauthorized
				}
				if errors.Is(err, errOperatorThrottle) {
					status = http.StatusTooManyRequests
					w.Header().Set("Retry-After", "60")
				}
				fail(w, status, err.Error())
				return
			}
			cookie := &http.Cookie{Name: cookieName, Value: value, Path: "/admin", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil}
			if in.Remember {
				cookie.MaxAge = 30 * 24 * 60 * 60
				cookie.Expires = expiry
			}
			http.SetCookie(w, cookie)
			jsonResponse(w, 200, map[string]any{"authenticated": true, "remembered": in.Remember, "expires_at": expiry})
		})
		mux.HandleFunc("POST /admin/logout", admin(func(w http.ResponseWriter, r *http.Request) {
			if err := sessions.Logout(cookieToken(r)); err != nil {
				fail(w, 500, "Could not end administrator session")
				return
			}
			http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/admin", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil, MaxAge: -1, Expires: time.Unix(1, 0)})
			jsonResponse(w, 200, map[string]bool{"ok": true})
		}))
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]string{"status": "ok", "version": Version})
	})
	mux.HandleFunc("GET /admin/state", admin(func(w http.ResponseWriter, r *http.Request) { jsonResponse(w, 200, e.Snapshot()) }))
	putAgent := admin(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		}
		if !decode(w, r, &in) {
			return
		}
		a, token, err := e.PutAgent(r.PathValue("id"), in.Name, in.Enabled)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		jsonResponse(w, 200, map[string]any{"agent": a, "token": token})
	})
	mux.HandleFunc("POST /admin/agents", putAgent)
	mux.HandleFunc("PUT /admin/agents/{id}", putAgent)
	mux.HandleFunc("POST /admin/services", admin(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Service     Service `json:"service"`
			ClearSecret bool    `json:"clear_secret"`
		}
		if !decode(w, r, &in) {
			return
		}
		s, err := e.PutService(in.Service, in.ClearSecret)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		jsonResponse(w, 200, s)
	}))
	mux.HandleFunc("POST /admin/rules", admin(func(w http.ResponseWriter, r *http.Request) {
		var in Rule
		if !decode(w, r, &in) {
			return
		}
		rule, err := e.PutRule(in)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		jsonResponse(w, 200, rule)
	}))
	mux.HandleFunc("DELETE /admin/rules/{id}", admin(func(w http.ResponseWriter, r *http.Request) {
		if err := e.DeleteRule(r.PathValue("id")); err != nil {
			fail(w, 400, err.Error())
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /admin/requests/{id}/decision", admin(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Decision string `json:"decision"`
		}
		if !decode(w, r, &in) {
			return
		}
		out, err := e.Decide(r.Context(), r.PathValue("id"), in.Decision)
		if err != nil {
			fail(w, 409, err.Error())
			return
		}
		jsonResponse(w, 200, out)
	}))
	mux.HandleFunc("POST /admin/leases/{id}/revoke", admin(func(w http.ResponseWriter, r *http.Request) {
		if err := e.RevokeLease(r.PathValue("id")); err != nil {
			fail(w, 404, err.Error())
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /admin/demo", admin(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Mode string `json:"mode"`
		}
		if !decode(w, r, &in) {
			return
		}
		out, err := e.Demo(r.Context(), in.Mode)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		jsonResponse(w, 200, out)
	}))
	mux.HandleFunc("POST /v1/requests", agent(func(w http.ResponseWriter, r *http.Request, actor string) {
		var in Call
		if !decode(w, r, &in) {
			return
		}
		out, err := e.Submit(r.Context(), actor, in)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		status := 200
		if out.Status == "pending" {
			status = 202
		}
		if out.Status == "denied" {
			status = 403
		}
		jsonResponse(w, status, out)
	}))
	mux.HandleFunc("GET /v1/requests/{id}", agent(func(w http.ResponseWriter, r *http.Request, actor string) {
		out, ok := e.GetRequest(actor, r.PathValue("id"))
		if !ok {
			fail(w, 404, "Request not found")
			return
		}
		jsonResponse(w, 200, out)
	}))
	mux.HandleFunc("POST /v1/requests/{id}/cancel", agent(func(w http.ResponseWriter, r *http.Request, actor string) {
		if err := e.Cancel(actor, r.PathValue("id")); err != nil {
			fail(w, 404, err.Error())
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /v1/credential-requests", agent(func(w http.ResponseWriter, r *http.Request, actor string) {
		var in Call
		if !decode(w, r, &in) {
			return
		}
		out, err := e.RequestCredential(actor, in)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		jsonResponse(w, 202, out)
	}))
	mux.HandleFunc("GET /v1/credential-requests/{id}", agent(func(w http.ResponseWriter, r *http.Request, actor string) {
		out, ok := e.GetCredentialRequest(actor, r.PathValue("id"))
		if !ok {
			fail(w, 404, "Credential request not found")
			return
		}
		jsonResponse(w, 200, out)
	}))
	mux.HandleFunc("POST /v1/credential-requests/{id}/cancel", agent(func(w http.ResponseWriter, r *http.Request, actor string) {
		if err := e.CancelCredential(actor, r.PathValue("id")); err != nil {
			fail(w, 404, err.Error())
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /admin/credential-requests/{id}/decision", admin(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Decision string `json:"decision"`
			Username string `json:"username"`
			Secret   string `json:"secret"`
		}
		// Never echo decoder errors: unknown field names may contain pasted secrets.
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if d.Decode(&in) != nil || d.Decode(new(any)) != io.EOF {
			fail(w, 400, "Invalid credential input")
			return
		}
		out, err := e.ResolveCredential(r.PathValue("id"), in.Decision, in.Username, in.Secret)
		if err != nil {
			fail(w, 409, err.Error())
			return
		}
		jsonResponse(w, 200, out)
	}))
	mux.HandleFunc("POST /admin/browser-sites", admin(func(w http.ResponseWriter, r *http.Request) {
		var in BrowserSite
		if !decode(w, r, &in) {
			return
		}
		out, err := e.PutBrowserSite(in)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		jsonResponse(w, 200, out)
	}))
	mux.HandleFunc("POST /v1/browser-handoffs", agent(func(w http.ResponseWriter, r *http.Request, actor string) {
		var in BrowserTarget
		if !decode(w, r, &in) {
			return
		}
		out, err := e.RequestBrowserHandoff(actor, in)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		jsonResponse(w, 202, out)
	}))
	mux.HandleFunc("GET /v1/browser-handoffs/{id}", agent(func(w http.ResponseWriter, r *http.Request, actor string) {
		out, ok := e.GetBrowserHandoff(actor, r.PathValue("id"))
		if !ok {
			fail(w, 404, "Browser handoff not found")
			return
		}
		jsonResponse(w, 200, out)
	}))
	mux.HandleFunc("POST /v1/browser-handoffs/{id}/cancel", agent(func(w http.ResponseWriter, r *http.Request, actor string) {
		if err := e.CancelBrowserHandoff(actor, r.PathValue("id")); err != nil {
			fail(w, 404, err.Error())
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /v1/browser-handoffs/{id}/verify", agent(func(w http.ResponseWriter, r *http.Request, actor string) {
		var in struct {
			Target        BrowserTarget `json:"target"`
			Origin        string        `json:"origin"`
			Authenticated bool          `json:"authenticated"`
		}
		if !decode(w, r, &in) {
			return
		}
		out, err := e.VerifyBrowserHandoff(actor, r.PathValue("id"), in.Target, in.Origin, in.Authenticated)
		if err != nil {
			fail(w, 409, err.Error())
			return
		}
		jsonResponse(w, 200, out)
	}))
	mux.HandleFunc("POST /admin/browser-handoffs/{id}/decision", admin(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Decision string `json:"decision"`
		}
		if !decode(w, r, &in) {
			return
		}
		out, err := e.BrowserDecision(r.PathValue("id"), in.Decision)
		if err != nil {
			fail(w, 409, err.Error())
			return
		}
		jsonResponse(w, 200, out)
	}))
	e.loginRoutes(mux, admin, agent)
	e.connectionRoutes(mux, admin)
	mux.Handle("GET /", assets)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if expectedHost != "" && r.Host != expectedHost {
			fail(w, 403, "Unexpected Host")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			fail(w, 403, "Cross-origin request blocked")
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			fail(w, 403, "Cross-site request blocked")
			return
		}
		mux.ServeHTTP(w, r)
	})
}
