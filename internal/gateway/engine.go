package gateway

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Engine struct {
	loginRunner LoginRunner
	loginRuns   map[string]*LoginRun
	loginBusy   bool
	loginClosed bool
	loginWG     sync.WaitGroup
	mu          sync.Mutex
	store       *Store
	state       State
	requests    map[string]*Request
	leases      map[string]*Lease
	credentials map[string]*CredentialRequest
	handoffs    map[string]*BrowserHandoff
	demo        bool
	now         func() time.Time
	executor    func(context.Context, Service, Call) (*Result, error)
}

func clone[T any](v T) T { b, _ := json.Marshal(v); var out T; _ = json.Unmarshal(b, &out); return out }

func NewEngine(store *Store, demo bool) (*Engine, error) {
	state, err := store.Load()
	if err != nil {
		return nil, err
	}
	e := &Engine{store: store, state: state, requests: map[string]*Request{}, leases: map[string]*Lease{}, credentials: map[string]*CredentialRequest{}, handoffs: map[string]*BrowserHandoff{}, demo: demo, now: time.Now}
	if e.state.LoginGrants == nil {
		e.state.LoginGrants = map[string]*LoginGrant{}
	}
	for _, g := range e.state.LoginGrants {
		if g.Revision != e.state.Revision {
			g.Revoked = true
		}
	}
	e.loginRuns = map[string]*LoginRun{}
	e.executor = executeHTTP
	if demo && len(state.Services) == 0 && len(state.Agents) == 0 && len(state.Rules) == 0 {
		e.state.Agents = []Agent{{ID: "demo-agent", Name: "Sandbox agent", Enabled: true}}
		e.state.Services = []Service{{ID: "sandbox", Name: "Sandbox API", Origin: "demo://sandbox", Enabled: true}}
		for _, item := range []struct{ id, name, method, path, mode string }{
			{"observe", "Read metrics", "GET", "/metrics", "auto"},
			{"deploy", "Deploy production", "POST", "/deploy", "approval"},
			{"diagnose", "Temporary diagnostics", "POST", "/diagnostics", "lease"},
			{"destroy", "Protect production data", "DELETE", "/database", "deny"},
		} {
			e.state.Rules = append(e.state.Rules, Rule{ID: item.id, Name: item.name, AgentID: "*", ServiceID: "sandbox", Methods: []string{item.method}, PathPrefix: item.path, Mode: item.mode, Enabled: true, LeaseSeconds: 900, MaxUses: 3})
		}
		if err := store.Save(e.state); err != nil {
			return nil, err
		}
	}
	return e, nil
}

func (e *Engine) Authenticate(token string) string {
	if len(token) != 64 {
		return ""
	}
	hash := tokenHash(token)
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, a := range e.state.Agents {
		if a.Enabled && subtle.ConstantTimeCompare([]byte(hash), []byte(a.TokenHash)) == 1 {
			return a.ID
		}
	}
	return ""
}

func (e *Engine) agent(id string) *Agent {
	for i := range e.state.Agents {
		if e.state.Agents[i].ID == id {
			return &e.state.Agents[i]
		}
	}
	return nil
}
func (e *Engine) service(id string) *Service {
	for i := range e.state.Services {
		if e.state.Services[i].ID == id {
			return &e.state.Services[i]
		}
	}
	return nil
}

func (e *Engine) record(action, actor, target, detail string) error {
	old := e.state.Audit
	e.state.Audit = append(append([]Event{}, old...), Event{ID: newID("ev"), At: e.now().UTC(), Action: action, Actor: actor, Target: target, Detail: detail})
	if len(e.state.Audit) > 500 {
		e.state.Audit = e.state.Audit[len(e.state.Audit)-500:]
	}
	if err := e.store.Save(e.state); err != nil {
		e.state.Audit = old
		return errors.New("cannot persist audit; operation blocked")
	}
	return nil
}

func (e *Engine) mutate(action, target string, fn func() error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	before := clone(e.state)
	if err := fn(); err != nil {
		e.state = before
		return err
	}
	e.state.Revision++
	if err := e.record(action, "operator", target, "Configuration changed; pending requests and leases invalidated"); err != nil {
		e.state = before
		return err
	}
	e.invalidatePending()
	return nil
}

func (e *Engine) invalidatePending() {
	for _, g := range e.state.LoginGrants {
		e.revokeLoginLocked(g)
	}
	for _, r := range e.handoffs {
		if handoffActive(r.Status) {
			r.Status = "invalidated"
		}
	}
	for _, r := range e.credentials {
		if r.Status == "pending" {
			r.Status = "invalidated"
		}
	}
	for _, r := range e.requests {
		if r.Status == "pending" {
			r.Status, r.Reason = "invalidated", "Configuration changed; submit a new request"
		}
	}
	for _, l := range e.leases {
		l.Revoked = true
	}
}

func (e *Engine) PutAgent(id, name string, enabled bool) (Agent, string, error) {
	var out Agent
	token := ""
	if strings.TrimSpace(name) == "" || len(name) > 100 {
		return out, token, errors.New("agent name must be 1–100 characters")
	}
	if id == "" {
		id, token = newID("agent"), randomToken()
	}
	err := e.mutate("agent.updated", id, func() error {
		if a := e.agent(id); a != nil {
			a.Name, a.Enabled = name, enabled
			out = *a
			return nil
		}
		if token == "" {
			return errors.New("agent not found")
		}
		if len(e.state.Agents) >= 100 {
			return errors.New("agent limit reached")
		}
		out = Agent{ID: id, Name: name, Enabled: enabled, TokenHash: tokenHash(token)}
		e.state.Agents = append(e.state.Agents, out)
		return nil
	})
	out.TokenHash = ""
	return out, token, err
}

func (e *Engine) PutService(s Service, clearSecret bool) (Service, error) {
	if clearSecret && s.Secret != "" {
		return Service{}, errors.New("choose either a new credential or clear the saved credential")
	}
	if s.ID == "" {
		s.ID = newID("service")
	}
	err := e.mutate("service.updated", s.ID, func() error {
		old := e.service(s.ID)
		if old != nil && s.Secret == "" && !clearSecret {
			s.Secret = old.Secret
		}
		s.SecretSet = false
		s.Origin = strings.TrimSuffix(s.Origin, "/")
		if err := validateService(s, e.demo); err != nil {
			return err
		}
		if old != nil {
			*old = s
		} else {
			if len(e.state.Services) >= 100 {
				return errors.New("service limit reached")
			}
			e.state.Services = append(e.state.Services, s)
		}
		return nil
	})
	s.SecretSet = s.Secret != ""
	s.Secret = ""
	return s, err
}

func (e *Engine) PutRule(r Rule) (Rule, error) {
	if r.ID == "" {
		r.ID = newID("rule")
	}
	err := e.mutate("rule.updated", r.ID, func() error {
		if err := validateRule(r); err != nil {
			return err
		}
		if e.service(r.ServiceID) == nil {
			return errors.New("service not found")
		}
		if r.AgentID != "*" && e.agent(r.AgentID) == nil {
			return errors.New("agent not found")
		}
		for i := range e.state.Rules {
			if e.state.Rules[i].ID == r.ID {
				e.state.Rules[i] = clone(r)
				return nil
			}
		}
		if len(e.state.Rules) >= 500 {
			return errors.New("rule limit reached")
		}
		e.state.Rules = append(e.state.Rules, clone(r))
		return nil
	})
	return r, err
}

func (e *Engine) DeleteRule(id string) error {
	return e.mutate("rule.deleted", id, func() error {
		for i, r := range e.state.Rules {
			if r.ID == id {
				e.state.Rules = append(e.state.Rules[:i], e.state.Rules[i+1:]...)
				return nil
			}
		}
		return errors.New("rule not found")
	})
}

func (e *Engine) expire() {
	now := e.now()
	for _, r := range e.loginRuns {
		if loginActive(r.Status) && !now.Before(r.ExpiresAt) {
			r.Status = "expired"
			if r.cancel != nil {
				r.cancel()
			}
		}
	}
	for _, r := range e.handoffs {
		if handoffActive(r.Status) && !now.Before(r.ExpiresAt) {
			r.Status = "expired"
		}
	}
	for _, r := range e.credentials {
		if r.Status == "pending" && !now.Before(r.ExpiresAt) {
			r.Status = "expired"
		}
	}
	for _, r := range e.requests {
		if r.Status == "pending" && !now.Before(r.ExpiresAt) {
			r.Status, r.Reason = "expired", "Approval window expired"
			if err := e.record("request.expired", "gateway", r.ID, r.Reason); err != nil {
				r.Reason += "; expiry audit unavailable"
			}
		}
	}
}

func (e *Engine) makeRoom() error {
	e.expire()
	if len(e.requests) < 256 {
		return nil
	}
	var oldest *Request
	for _, r := range e.requests {
		if r.Status != "pending" && r.Status != "executing" && (oldest == nil || r.CreatedAt.Before(oldest.CreatedAt)) {
			oldest = r
		}
	}
	if oldest == nil {
		return errors.New("request capacity reached")
	}
	delete(e.requests, oldest.ID)
	return nil
}

func validateCall(c Call) error {
	if !methodOK(c.Method) || !pathOK(c.Path) {
		return errors.New("invalid method or canonical path")
	}
	if len(c.Body) > MaxBody || (c.Body != "" && !json.Valid([]byte(c.Body))) {
		return errors.New("body must be valid JSON, at most 64 KiB")
	}
	if (c.Method == "GET" || c.Method == "HEAD") && c.Body != "" {
		return errors.New("GET/HEAD requests cannot have a body")
	}
	size := 0
	for k, v := range c.Query {
		size += len(k) + len(v)
	}
	if len(c.Query) > 32 || size > 8192 {
		return errors.New("query is too large")
	}
	return nil
}

func (e *Engine) Submit(ctx context.Context, actor string, c Call) (Request, error) {
	c = clone(c)
	if err := validateCall(c); err != nil {
		return Request{}, err
	}
	e.mu.Lock()
	a := e.agent(actor)
	if a == nil || !a.Enabled {
		e.mu.Unlock()
		return Request{}, errors.New("agent disabled or unknown")
	}
	if err := e.makeRoom(); err != nil {
		e.mu.Unlock()
		return Request{}, err
	}
	count := 0
	for _, r := range e.requests {
		if r.AgentID == actor && (r.Status == "pending" || r.Status == "executing") {
			count++
		}
	}
	if count >= 16 {
		e.mu.Unlock()
		return Request{}, errors.New("agent has too many pending or executing requests")
	}
	now := e.now().UTC()
	rule, reason := evaluate(e.state.Rules, actor, c, now)
	r := &Request{ID: newID("req"), AgentID: actor, Call: c, RuleID: rule.ID, Mode: rule.Mode, Revision: e.state.Revision, Status: "pending", Reason: reason, CreatedAt: now, ExpiresAt: now.Add(PendingTTL)}
	if rule.ExpiresAt != nil && rule.ExpiresAt.Before(r.ExpiresAt) {
		r.ExpiresAt = *rule.ExpiresAt
	}
	s := e.service(c.ServiceID)
	if s != nil {
		r.Origin = s.Origin
	}
	if s == nil || !s.Enabled || rule.ID == "" || rule.Mode == "deny" {
		r.Status, r.Mode = "denied", "deny"
		if s == nil || !s.Enabled {
			r.Reason = "Service disabled or unknown"
		}
	}
	e.requests[r.ID] = r
	var lease *Lease
	if r.Status != "denied" && rule.Mode == "lease" {
		for _, l := range e.leases {
			if !l.Revoked && l.AgentID == actor && l.RuleID == rule.ID && l.Revision == e.state.Revision && now.Before(l.ExpiresAt) && l.Remaining > 0 {
				lease = l
				break
			}
		}
	}
	execute := r.Status != "denied" && (rule.Mode == "auto" || lease != nil)
	action := "request." + r.Status
	if execute {
		action = "request.authorized"
	}
	if err := e.record(action, actor, r.ID, c.Method+" "+c.ServiceID+c.Path+" · "+r.Reason); err != nil {
		delete(e.requests, r.ID)
		e.mu.Unlock()
		return Request{}, err
	}
	if execute {
		if lease != nil {
			lease.Remaining--
		}
		r.Status, r.Reason = "executing", "Authorized for dispatch"
		service := *s
		e.mu.Unlock()
		return e.execute(ctx, r.ID, service, c), nil
	}
	out := clone(*r)
	e.mu.Unlock()
	return out, nil
}

func (e *Engine) execute(ctx context.Context, id string, s Service, c Call) Request {
	var result *Result
	var err error
	if e.demo && s.Origin == "demo://sandbox" {
		b, _ := json.Marshal(map[string]any{"ok": true, "simulated": true, "message": "Request reached the sandbox after authorization", "method": c.Method, "path": c.Path, "query": c.Query, "body": c.Body})
		result = &Result{Status: 200, Body: string(b)}
	} else {
		result, err = e.executor(ctx, s, c)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.requests[id]
	r.Status, r.Reason, r.Result = "completed", "Upstream responded", result
	if err != nil {
		r.Status, r.Reason = "failed", err.Error()
	}
	if saveErr := e.record("request."+r.Status, r.AgentID, id, r.Reason); saveErr != nil {
		r.Reason += "; completion audit unavailable (dispatch already occurred)"
	}
	return clone(*r)
}

func (e *Engine) Decide(ctx context.Context, id, decision string) (Request, error) {
	e.mu.Lock()
	e.expire()
	r := e.requests[id]
	if r == nil || r.Status != "pending" {
		e.mu.Unlock()
		return Request{}, errors.New("request is no longer pending")
	}
	if decision != "reject" && decision != "once" && decision != "lease" {
		e.mu.Unlock()
		return Request{}, errors.New("invalid decision")
	}
	if decision == "reject" {
		if err := e.record("request.rejected", "operator", id, "Human rejected this request"); err != nil {
			e.mu.Unlock()
			return Request{}, err
		}
		r.Status, r.Reason = "rejected", "Human rejected this request"
		out := clone(*r)
		e.mu.Unlock()
		return out, nil
	}
	a, s := e.agent(r.AgentID), e.service(r.Call.ServiceID)
	rule, _ := evaluate(e.state.Rules, r.AgentID, r.Call, e.now())
	if r.Revision != e.state.Revision || a == nil || !a.Enabled || s == nil || !s.Enabled || rule.ID != r.RuleID || rule.Mode == "deny" {
		r.Status, r.Reason = "invalidated", "Authorization changed"
		e.mu.Unlock()
		return Request{}, errors.New("authorization changed; resubmit")
	}
	if decision == "lease" && rule.Mode != "lease" {
		e.mu.Unlock()
		return Request{}, errors.New("this rule permits one-time approval only")
	}
	detail := "Decision: " + decision
	if decision == "lease" {
		now := e.now().UTC()
		for k, l := range e.leases {
			if l.Revoked || !now.Before(l.ExpiresAt) || l.Remaining == 0 {
				delete(e.leases, k)
			}
		}
		if len(e.leases) >= 256 {
			e.mu.Unlock()
			return Request{}, errors.New("lease capacity reached")
		}
		detail += fmt.Sprintf("; rule=%s; seconds=%d; max_calls=%d", rule.ID, rule.LeaseSeconds, rule.MaxUses)
	}
	if err := e.record("request.approved", "operator", id, detail); err != nil {
		e.mu.Unlock()
		return Request{}, err
	}
	if decision == "lease" {
		now := e.now().UTC()
		// Re-granting replaces the prior lease for the same scope; grants never accumulate.
		for _, l := range e.leases {
			if l.AgentID == r.AgentID && l.RuleID == rule.ID {
				l.Revoked = true
			}
		}
		l := &Lease{ID: newID("lease"), AgentID: r.AgentID, RuleID: rule.ID, ServiceID: rule.ServiceID, PathPrefix: rule.PathPrefix, Methods: append([]string{}, rule.Methods...), Revision: e.state.Revision, CreatedAt: now, ExpiresAt: now.Add(time.Duration(rule.LeaseSeconds) * time.Second), Total: rule.MaxUses, Remaining: rule.MaxUses - 1}
		if rule.ExpiresAt != nil && rule.ExpiresAt.Before(l.ExpiresAt) {
			l.ExpiresAt = *rule.ExpiresAt
		}
		e.leases[l.ID] = l
	}
	r.Status, r.Reason = "executing", "Human approved the exact request"
	service, call := *s, clone(r.Call)
	e.mu.Unlock()
	return e.execute(ctx, id, service, call), nil
}

func (e *Engine) Cancel(actor, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	r := e.requests[id]
	if r == nil || r.AgentID != actor || r.Status != "pending" {
		return errors.New("pending request not found")
	}
	if err := e.record("request.cancelled", actor, id, "Agent cancelled pending request"); err != nil {
		return err
	}
	r.Status, r.Reason = "cancelled", "Agent cancelled request"
	return nil
}

func (e *Engine) RevokeLease(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	l := e.leases[id]
	if l == nil {
		return errors.New("lease not found")
	}
	if err := e.record("lease.revoked", "operator", id, "Further calls require approval"); err != nil {
		return err
	}
	l.Revoked = true
	return nil
}

func (e *Engine) GetRequest(actor, id string) (Request, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	r := e.requests[id]
	if r == nil || r.AgentID != actor {
		return Request{}, false
	}
	return clone(*r), true
}

func (e *Engine) Snapshot() Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	s := Snapshot{BrowserSites: clone(e.state.BrowserSites), BrowserHandoffs: []BrowserHandoff{}, Version: Version, Revision: e.state.Revision, Demo: e.demo, ServerTime: e.now().UTC(), Agents: clone(e.state.Agents), Services: clone(e.state.Services), Rules: clone(e.state.Rules), Audit: clone(e.state.Audit), Requests: []Request{}, Leases: []Lease{}, CredentialRequests: []CredentialRequest{}}
	for i := range s.Agents {
		s.Agents[i].TokenHash = ""
	}
	for i := range s.Services {
		s.Services[i].SecretSet = s.Services[i].Secret != ""
		s.Services[i].Secret = ""
	}
	for _, r := range e.handoffs {
		s.BrowserHandoffs = append(s.BrowserHandoffs, *r)
	}
	sort.Slice(s.BrowserHandoffs, func(i, j int) bool { return s.BrowserHandoffs[i].CreatedAt.After(s.BrowserHandoffs[j].CreatedAt) })
	for _, r := range e.credentials {
		s.CredentialRequests = append(s.CredentialRequests, *r)
	}
	sort.Slice(s.CredentialRequests, func(i, j int) bool { return s.CredentialRequests[i].CreatedAt.After(s.CredentialRequests[j].CreatedAt) })
	for _, r := range e.requests {
		s.Requests = append(s.Requests, clone(*r))
	}
	for _, l := range e.leases {
		s.Leases = append(s.Leases, clone(*l))
	}
	sort.Slice(s.Requests, func(i, j int) bool { return s.Requests[i].CreatedAt.After(s.Requests[j].CreatedAt) })
	sort.Slice(s.Leases, func(i, j int) bool { return s.Leases[i].CreatedAt.After(s.Leases[j].CreatedAt) })
	e.loginSnapshot(&s)
	return s
}

func (e *Engine) Demo(ctx context.Context, mode string) (Request, error) {
	if !e.demo {
		return Request{}, errors.New("demo is not enabled")
	}
	c := Call{ServiceID: "sandbox"}
	switch mode {
	case "auto":
		c.Method, c.Path = "GET", "/metrics"
	case "approval":
		c.Method, c.Path, c.Body = "POST", "/deploy", `{"environment":"production","version":"v1.2.0"}`
	case "lease":
		c.Method, c.Path, c.Body = "POST", "/diagnostics", `{"check":"service-health"}`
	case "deny":
		c.Method, c.Path = "DELETE", "/database"
	default:
		return Request{}, fmt.Errorf("unknown demo scenario")
	}
	return e.Submit(ctx, "demo-agent", c)
}
