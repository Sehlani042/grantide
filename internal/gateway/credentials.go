package gateway

import "errors"

// RequestCredential records intent only. It never authorizes an upstream call.
func (e *Engine) RequestCredential(actor string, c Call) (CredentialRequest, error) {
	if c.Body != "" || len(c.Query) != 0 {
		return CredentialRequest{}, errors.New("credential requests accept only service, method and path")
	}
	if err := validateCall(c); err != nil {
		return CredentialRequest{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	a, s := e.agent(actor), e.service(c.ServiceID)
	rule, _ := evaluate(e.state.Rules, actor, c, e.now())
	if a == nil || !a.Enabled || s == nil || !s.Enabled || s.AuthHeader == "" || rule.ID == "" || rule.Mode == "deny" {
		return CredentialRequest{}, errors.New("credential request requires an enabled service with authentication and a matching non-deny policy")
	}
	count := 0
	for _, r := range e.credentials {
		if r.AgentID == actor && r.Status == "pending" {
			if r.ServiceID == c.ServiceID && r.Method == c.Method && r.Path == c.Path {
				return *r, nil
			}
			count++
		}
	}
	if count >= 8 {
		return CredentialRequest{}, errors.New("too many pending credential requests")
	}
	if len(e.credentials) >= 128 {
		var oldest *CredentialRequest
		for _, r := range e.credentials {
			if r.Status != "pending" && (oldest == nil || r.CreatedAt.Before(oldest.CreatedAt)) {
				oldest = r
			}
		}
		if oldest == nil {
			return CredentialRequest{}, errors.New("credential request capacity reached")
		}
		delete(e.credentials, oldest.ID)
	}
	now := e.now().UTC()
	authType := s.AuthType
	if authType == "" {
		authType = "header"
	}
	r := CredentialRequest{ID: newID("cred"), AgentID: actor, ServiceID: s.ID, Origin: s.Origin, AuthType: authType, Method: c.Method, Path: c.Path, Revision: e.state.Revision, Status: "pending", CreatedAt: now, ExpiresAt: now.Add(PendingTTL)}
	if rule.ExpiresAt != nil && rule.ExpiresAt.Before(r.ExpiresAt) {
		r.ExpiresAt = *rule.ExpiresAt
	}
	if err := e.record("credential.requested", actor, r.ID, "Credential entry requested for "+s.ID); err != nil {
		return CredentialRequest{}, err
	}
	e.credentials[r.ID] = &r
	return r, nil
}

func (e *Engine) GetCredentialRequest(actor, id string) (CredentialRequest, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	r := e.credentials[id]
	if r == nil || r.AgentID != actor {
		return CredentialRequest{}, false
	}
	return *r, true
}

func (e *Engine) CancelCredential(actor, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	r := e.credentials[id]
	if r == nil || r.AgentID != actor || r.Status != "pending" {
		return errors.New("pending credential request not found")
	}
	if err := e.record("credential.cancelled", actor, id, "Credential request cancelled"); err != nil {
		return err
	}
	r.Status = "cancelled"
	return nil
}

// Credentials arrive only through the separately authenticated operator endpoint.
func (e *Engine) ResolveCredential(id, decision, username, secret string) (CredentialRequest, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	r := e.credentials[id]
	if r == nil || r.Status != "pending" {
		return CredentialRequest{}, errors.New("credential request is no longer pending")
	}
	if decision == "reject" {
		if err := e.record("credential.rejected", "operator", id, "Credential request rejected"); err != nil {
			return CredentialRequest{}, err
		}
		r.Status = "rejected"
		return *r, nil
	}
	if decision != "save" || secret == "" {
		return CredentialRequest{}, errors.New("choose save with a credential, or reject")
	}
	a, s := e.agent(r.AgentID), e.service(r.ServiceID)
	rule, _ := evaluate(e.state.Rules, r.AgentID, Call{ServiceID: r.ServiceID, Method: r.Method, Path: r.Path}, e.now())
	if r.Revision != e.state.Revision || a == nil || !a.Enabled || s == nil || !s.Enabled || rule.ID == "" || rule.Mode == "deny" {
		r.Status = "invalidated"
		return CredentialRequest{}, errors.New("authorization changed; resubmit")
	}
	next := *s
	next.Secret = secret
	if s.AuthType == "basic" {
		next.Username = username
	} else if username != "" {
		return CredentialRequest{}, errors.New("username is only supported for HTTP Basic")
	}
	if err := validateService(next, e.demo); err != nil {
		return CredentialRequest{}, err
	}
	before := clone(e.state)
	*s = next
	e.state.Revision++
	if err := e.record("credential.saved", "operator", id, "Service credential updated; existing requests and grants invalidated; no operation dispatched"); err != nil {
		e.state = before
		return CredentialRequest{}, err
	}
	e.invalidatePending()
	r.Status = "fulfilled"
	return *r, nil
}
