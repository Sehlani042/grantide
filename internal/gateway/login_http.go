package gateway

import (
	"encoding/json"
	"io"
	"net/http"
)

func (e *Engine) loginRoutes(mux *http.ServeMux, admin func(http.HandlerFunc) http.HandlerFunc, agent func(func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc) {
	mux.HandleFunc("POST /admin/web-accounts", admin(func(w http.ResponseWriter, r *http.Request) {
		var in WebAccount
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if d.Decode(&in) != nil || d.Decode(new(any)) != io.EOF {
			fail(w, 400, "Invalid account input")
			return
		}
		out, err := e.PutWebAccount(in)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		jsonResponse(w, 200, out)
	}))
	mux.HandleFunc("DELETE /admin/web-accounts/{id}", admin(func(w http.ResponseWriter, r *http.Request) {
		if err := e.DeleteWebAccount(r.PathValue("id")); err != nil {
			fail(w, 400, err.Error())
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /admin/web-accounts/{id}/review-login", admin(func(w http.ResponseWriter, r *http.Request) {
		if err := e.ReviewAccountLogin(r.PathValue("id")); err != nil {
			fail(w, 409, err.Error())
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /admin/login-grants", admin(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			AccountID     string `json:"account_id"`
			AgentID       string `json:"agent_id"`
			ReadPath      string `json:"read_path"`
			Seconds       int    `json:"seconds"`
			Uses          int    `json:"uses"`
			UnlimitedUses bool   `json:"unlimited_uses"`
		}
		if !decode(w, r, &in) {
			return
		}
		out, err := e.CreateLoginGrantWithLimit(in.AccountID, in.AgentID, in.ReadPath, in.Seconds, in.Uses, in.UnlimitedUses)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		jsonResponse(w, 200, out)
	}))
	mux.HandleFunc("POST /admin/login-grants/{id}/revoke", admin(func(w http.ResponseWriter, r *http.Request) {
		if err := e.RevokeLoginGrant(r.PathValue("id")); err != nil {
			fail(w, 409, err.Error())
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("GET /v1/login-grants", agent(func(w http.ResponseWriter, r *http.Request, actor string) { jsonResponse(w, 200, e.LoginGrants(actor)) }))
	mux.HandleFunc("POST /v1/logins", agent(func(w http.ResponseWriter, r *http.Request, actor string) {
		var in struct {
			GrantID string `json:"grant_id"`
		}
		if !decode(w, r, &in) {
			return
		}
		out, err := e.StartLogin(actor, in.GrantID)
		if err != nil {
			fail(w, 403, err.Error())
			return
		}
		jsonResponse(w, 202, out)
	}))
	mux.HandleFunc("GET /v1/logins/{id}", agent(func(w http.ResponseWriter, r *http.Request, actor string) {
		out, ok := e.GetLogin(actor, r.PathValue("id"))
		if !ok {
			fail(w, 404, "Run not found")
			return
		}
		jsonResponse(w, 200, out)
	}))
	mux.HandleFunc("POST /v1/logins/{id}/cancel", agent(func(w http.ResponseWriter, r *http.Request, actor string) {
		if err := e.CancelLogin(actor, r.PathValue("id")); err != nil {
			fail(w, 409, err.Error())
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /admin/logins/{id}/resume", admin(func(w http.ResponseWriter, r *http.Request) {
		if err := e.ResumeLogin(r.PathValue("id")); err != nil {
			fail(w, 409, err.Error())
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /admin/logins/{id}/cancel", admin(func(w http.ResponseWriter, r *http.Request) {
		if err := e.CancelLogin("operator", r.PathValue("id")); err != nil {
			fail(w, 409, err.Error())
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
	}))
}
