package gateway

import (
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Only the hash is persisted. The calling local helper owns the raw credential.
type ConnectionRequest struct {
	ID           string    `json:"id"`
	TokenHash    string    `json:"token_hash,omitempty"`
	Name         string    `json:"name"`
	Conversation string    `json:"conversation"`
	Target       string    `json:"target"`
	AccountLabel string    `json:"account_label,omitempty"`
	AgentID      string    `json:"agent_id,omitempty"`
	AccountID    string    `json:"account_id,omitempty"`
	GrantID      string    `json:"grant_id,omitempty"`
	RunID        string    `json:"run_id,omitempty"`
	Status       string    `json:"status"`
	Revision     int       `json:"revision"`
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	Result       *LoginRun `json:"result,omitempty"`
}
type ConnectInput struct {
	Name         string `json:"name"`
	Conversation string `json:"conversation"`
	Target       string `json:"target"`
	AccountLabel string `json:"account_label,omitempty"`
}

var connectionToken = regexp.MustCompile(`^[a-f0-9]{64}$`)

func connectionPath(target string) (string, bool) {
	if target == CloudConeOrigin+"/" {
		return "", true
	}
	path := strings.TrimPrefix(target, CloudConeOrigin)
	return path, strings.HasPrefix(target, CloudConeOrigin) && instancePath.MatchString(path)
}
func publicConnection(r *ConnectionRequest) ConnectionRequest {
	out := clone(*r)
	out.TokenHash = ""
	return out
}
func (e *Engine) connectionStateLocked(r *ConnectionRequest) {
	if r.Status == "pending" {
		if r.Revision != e.state.Revision {
			r.Status = "invalidated"
		} else if !e.now().Before(r.ExpiresAt) {
			r.Status = "expired"
		}
	}
	if r.Status == "executing" {
		if run := e.loginRuns[r.RunID]; run != nil {
			if !loginActive(run.Status) {
				r.Status = run.Status
				result := clone(*run)
				r.Result = &result
			}
		} else {
			r.Status = "interrupted"
		}
	}
}
func (e *Engine) connectionViewLocked(r *ConnectionRequest) ConnectionRequest {
	e.connectionStateLocked(r)
	out := publicConnection(r)
	if run := e.loginRuns[r.RunID]; run != nil {
		result := clone(*run)
		out.Result = &result
	}
	return out
}
func (e *Engine) RequestConnection(token string, in ConnectInput) (ConnectionRequest, error) {
	if !connectionToken.MatchString(token) || !labelOK(in.Name, 100) || !labelOK(in.Conversation, 100) || len(in.AccountLabel) > 100 {
		return ConnectionRequest{}, errors.New("invalid local connection request")
	}
	path, ok := connectionPath(in.Target)
	if !ok {
		return ConnectionRequest{}, errors.New("supported target required: CloudCone login or exact instance management URL")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	hash := tokenHash(token)
	actor := ""
	for _, a := range e.state.Agents {
		if a.TokenHash == hash {
			if !a.Enabled {
				return ConnectionRequest{}, errors.New("this identity was disabled by the operator")
			}
			actor = a.ID
		}
	}
	pending := 0
	for _, r := range e.state.ConnectionRequests {
		e.connectionStateLocked(r)
		if r.Status == "pending" {
			pending++
		}
		if r.TokenHash == hash && (r.Status == "pending" || r.Status == "executing") {
			if r.Target == in.Target && r.AccountLabel == in.AccountLabel {
				return e.connectionViewLocked(r), nil
			}
			return ConnectionRequest{}, errors.New("finish or reject this identity's current request first")
		}
	}
	if pending >= 16 {
		return ConnectionRequest{}, errors.New("connection review queue is full")
	}
	before := clone(e.state)
	if len(e.state.ConnectionRequests) >= 128 {
		var oldest *ConnectionRequest
		for _, r := range e.state.ConnectionRequests {
			if r.Status != "pending" && r.Status != "executing" && (oldest == nil || r.CreatedAt.Before(oldest.CreatedAt)) {
				oldest = r
			}
		}
		if oldest == nil {
			return ConnectionRequest{}, errors.New("connection history is full")
		}
		delete(e.state.ConnectionRequests, oldest.ID)
	}
	now := e.now().UTC()
	r := &ConnectionRequest{ID: newID("connect"), TokenHash: hash, Name: in.Name, Conversation: in.Conversation, Target: in.Target, AccountLabel: in.AccountLabel, AgentID: actor, Status: "pending", Revision: e.state.Revision, CreatedAt: now, ExpiresAt: now.Add(15 * time.Minute)}
	e.state.ConnectionRequests[r.ID] = r
	// Reuse only an unambiguous, exact existing grant for this identity.
	var match *LoginGrant
	ambiguous := false
	if actor != "" {
		for _, g := range e.state.LoginGrants {
			a := e.account(g.AccountID)
			if a != nil && a.Enabled && g.AgentID == actor && g.ReadPath == path && !g.Revoked && g.Revision == e.state.Revision && !g.expired(now) && (g.UnlimitedUses || g.Remaining > 0) && (in.AccountLabel == "" || a.Name == in.AccountLabel) {
				if match != nil && match.AccountID != g.AccountID {
					ambiguous = true
				}
				match = g
			}
		}
	}
	if match != nil && !ambiguous {
		r.AccountID, r.GrantID, r.Status = match.AccountID, match.ID, "executing"
		run, err := e.startExtensionLoginLocked(actor, match.ID, r)
		if err != nil {
			e.state = before
			return ConnectionRequest{}, err
		}
		r.RunID = run.ID
	} else if err := e.record("connection.requested", "local-client", r.ID, "Local identity requested approval for a scoped website login"); err != nil {
		e.state = before
		return ConnectionRequest{}, err
	}
	return e.connectionViewLocked(r), nil
}
func (e *Engine) GetConnection(token, id string) (ConnectionRequest, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	for _, a := range e.state.Agents {
		if a.TokenHash == tokenHash(token) && !a.Enabled {
			return ConnectionRequest{}, false
		}
	}
	r := e.state.ConnectionRequests[id]
	if id == "latest" && connectionToken.MatchString(token) {
		for _, candidate := range e.state.ConnectionRequests {
			if candidate.TokenHash == tokenHash(token) && (r == nil || candidate.CreatedAt.After(r.CreatedAt)) {
				r = candidate
			}
		}
	}
	if !connectionToken.MatchString(token) || r == nil || r.TokenHash != tokenHash(token) {
		return ConnectionRequest{}, false
	}
	return e.connectionViewLocked(r), true
}
func (e *Engine) DecideConnection(id, decision, accountID string, persistent bool) (ConnectionRequest, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	r := e.state.ConnectionRequests[id]
	if r == nil {
		return ConnectionRequest{}, errors.New("connection request not found")
	}
	e.connectionStateLocked(r)
	if r.Status != "pending" {
		return ConnectionRequest{}, errors.New("connection request is no longer pending")
	}
	before := clone(e.state)
	if decision == "reject" {
		r.Status = "rejected"
		if err := e.record("connection.rejected", "operator", id, "Local connection rejected"); err != nil {
			e.state = before
			return ConnectionRequest{}, err
		}
		return publicConnection(r), nil
	}
	if decision != "approve" {
		return ConnectionRequest{}, errors.New("invalid decision")
	}
	a := e.account(accountID)
	if a == nil || !a.Enabled || a.Password == "" || a.Origin != CloudConeOrigin {
		return ConnectionRequest{}, errors.New("select an enabled saved CloudCone account")
	}
	if r.AgentID == "" {
		if len(e.state.Agents) >= 100 {
			return ConnectionRequest{}, errors.New("agent limit reached")
		}
		r.AgentID = newID("agent")
		// Enrollment adds only a website identity; it cannot invoke wildcard API policies.
		e.state.Agents = append(e.state.Agents, Agent{ID: r.AgentID, Name: r.Name, Enabled: true, TokenHash: r.TokenHash, WebsiteOnly: true})
	} else if agent := e.agent(r.AgentID); agent == nil || !agent.Enabled {
		return ConnectionRequest{}, errors.New("identity is disabled")
	}
	for k, g := range e.state.LoginGrants {
		if g.Revoked || g.expired(e.now()) || (!g.UnlimitedUses && g.Remaining == 0) {
			delete(e.state.LoginGrants, k)
		}
	}
	if len(e.state.LoginGrants) >= 128 {
		e.state = before
		return ConnectionRequest{}, errors.New("login grant limit reached")
	}
	path, _ := connectionPath(r.Target)
	g := &LoginGrant{ID: newID("login_grant"), AccountID: a.ID, AgentID: r.AgentID, Origin: a.Origin, ReadPath: path, Revision: e.state.Revision, Remaining: 1}
	if persistent {
		g.Remaining = 0
		g.UnlimitedUses = true
	} else {
		expiry := e.now().UTC().Add(10 * time.Minute)
		g.ExpiresAt = &expiry
	}
	e.state.LoginGrants[g.ID] = g
	r.AccountID, r.GrantID, r.Status = a.ID, g.ID, "executing"
	detail := "Approved one login: " + r.Target
	if persistent {
		detail = "Approved unlimited revocable logins without expiry: " + r.Target
	}
	e.state.Audit = append(e.state.Audit, Event{ID: newID("ev"), At: e.now().UTC(), Action: "connection.approved", Actor: "operator", Target: id, Detail: detail})
	// Persist enrollment, scope and consumption together before the extension can claim.
	run, err := e.startExtensionLoginLocked(r.AgentID, g.ID, r)
	if err != nil {
		e.state = before
		return ConnectionRequest{}, err
	}
	r.RunID = run.ID
	return e.connectionViewLocked(r), nil
}
func (e *Engine) websiteOnly(actor string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	a := e.agent(actor)
	return a != nil && a.WebsiteOnly
}
func (e *Engine) connectionSnapshot(s *Snapshot) {
	s.ConnectionRequests = []ConnectionRequest{}
	for _, r := range e.state.ConnectionRequests {
		s.ConnectionRequests = append(s.ConnectionRequests, e.connectionViewLocked(r))
	}
	sort.Slice(s.ConnectionRequests, func(i, j int) bool { return s.ConnectionRequests[i].CreatedAt.After(s.ConnectionRequests[j].CreatedAt) })
}
func (e *Engine) connectionRoutes(mux *http.ServeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("POST /v1/connections", func(w http.ResponseWriter, r *http.Request) {
		var in ConnectInput
		if !decode(w, r, &in) {
			return
		}
		out, err := e.RequestConnection(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), in)
		if err != nil {
			fail(w, 409, err.Error())
			return
		}
		jsonResponse(w, 202, out)
	})
	mux.HandleFunc("GET /v1/connections/{id}", func(w http.ResponseWriter, r *http.Request) {
		out, ok := e.GetConnection(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), r.PathValue("id"))
		if !ok {
			fail(w, 404, "Connection not found")
			return
		}
		jsonResponse(w, 200, out)
	})
	mux.HandleFunc("POST /admin/connections/{id}/decision", admin(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Decision   string `json:"decision"`
			AccountID  string `json:"account_id"`
			Persistent bool   `json:"persistent"`
		}
		if !decode(w, r, &in) {
			return
		}
		out, err := e.DecideConnection(r.PathValue("id"), in.Decision, in.AccountID, in.Persistent)
		if err != nil {
			fail(w, 409, err.Error())
			return
		}
		jsonResponse(w, 200, out)
	}))
}
