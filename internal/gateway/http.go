package gateway

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"
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

func Handler(e *Engine, operatorToken, expectedHost string, assets http.Handler) http.Handler {
	mux := http.NewServeMux()
	admin := func(fn http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if operatorToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(operatorToken)) != 1 {
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
			fn(w, r, actor)
		}
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
