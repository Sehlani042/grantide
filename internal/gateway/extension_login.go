package gateway

import (
	"errors"
	"sort"
	"time"
)

// ExtensionLogin is metadata only. The native host never sends a password for a poll.
type ExtensionLogin struct {
	ID       string    `json:"id"`
	Origin   string    `json:"origin"`
	ReadPath string    `json:"read_path,omitempty"`
	Status   string    `json:"status"`
	Deadline time.Time `json:"deadline"`
}

func (e *Engine) extensionBusyLocked() bool {
	for _, r := range e.loginRuns {
		if r.Transport == "extension" && loginActive(r.Status) {
			return true
		}
	}
	return false
}

// StartExtensionLogin reserves exactly the same persisted grant budget and
// account retry guard as the isolated worker. No credential leaves this method.
func (e *Engine) StartExtensionLogin(actor, grant string) (LoginRun, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.startExtensionLoginLocked(actor, grant, nil)
}

func (e *Engine) startExtensionLoginLocked(actor, grant string, connection *ConnectionRequest) (LoginRun, error) {
	e.expire()
	g := e.state.LoginGrants[grant]
	if g == nil || g.AgentID != actor || g.Revision != e.state.Revision || g.Revoked || (!g.UnlimitedUses && g.Remaining < 1) || g.expired(e.now()) {
		return LoginRun{}, errors.New("active login grant not found")
	}
	a, agent := e.account(g.AccountID), e.agent(actor)
	if a == nil || !a.Enabled || agent == nil || !agent.Enabled || e.loginClosed || e.loginBusy || e.extensionBusyLocked() {
		return LoginRun{}, errors.New("login is unavailable")
	}
	if a.LoginNeedsReview {
		return LoginRun{}, errors.New("account login paused after an unsuccessful or interrupted run; operator review required")
	}
	if a.NextLoginAt != nil && e.now().Before(*a.NextLoginAt) {
		return LoginRun{}, errors.New("account login rate limit: wait until next_login_at")
	}
	if len(e.loginRuns) >= 128 {
		var oldest *LoginRun
		for _, r := range e.loginRuns {
			if !loginActive(r.Status) && (oldest == nil || r.CreatedAt.Before(oldest.CreatedAt)) {
				oldest = r
			}
		}
		if oldest == nil {
			return LoginRun{}, errors.New("run limit reached")
		}
		delete(e.loginRuns, oldest.ID)
	}
	now := e.now().UTC()
	deadline := now.Add(10 * time.Minute)
	if g.ExpiresAt != nil && g.ExpiresAt.Before(deadline) {
		deadline = *g.ExpiresAt
	}
	r := &LoginRun{ID: newID("login"), GrantID: g.ID, AgentID: actor, AccountID: a.ID, Origin: g.Origin, ReadPath: g.ReadPath, Status: "extension_pending", Transport: "extension", CreatedAt: now, ExpiresAt: deadline}
	if connection != nil {
		connection.RunID = r.ID
	}
	previousRemaining, previousNext, previousReview := g.Remaining, a.NextLoginAt, a.LoginNeedsReview
	if !g.UnlimitedUses {
		g.Remaining--
	}
	next := now.Add(loginInterval)
	a.NextLoginAt, a.LoginNeedsReview = &next, true
	if err := e.record("login.started", actor, r.ID, "Extension login reservation and account retry guard persisted"); err != nil {
		g.Remaining, a.NextLoginAt, a.LoginNeedsReview = previousRemaining, previousNext, previousReview
		return LoginRun{}, err
	}
	e.loginRuns[r.ID] = r
	return clone(*r), nil
}

func (e *Engine) NextExtensionLogin() *ExtensionLogin {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	var runs []*LoginRun
	for _, r := range e.loginRuns {
		if r.Transport == "extension" && loginActive(r.Status) {
			runs = append(runs, r)
		}
	}
	if len(runs) == 0 {
		return nil
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].CreatedAt.Before(runs[j].CreatedAt) })
	r := runs[0]
	return &ExtensionLogin{ID: r.ID, Origin: r.Origin, ReadPath: r.ReadPath, Status: r.Status, Deadline: r.ExpiresAt}
}

// ClaimExtensionLogin gives the password to the locally installed native host
// once. A lost claim must expire and receive operator review before a retry.
func (e *Engine) ClaimExtensionLogin(id string) (LoginInput, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	r := e.loginRuns[id]
	if r == nil || r.Transport != "extension" || r.Status != "extension_pending" {
		return LoginInput{}, errors.New("pending extension login not found")
	}
	g, a := e.state.LoginGrants[r.GrantID], e.account(r.AccountID)
	if g == nil || g.Revoked || g.Revision != e.state.Revision || g.expired(e.now()) || a == nil || !a.Enabled || a.Password == "" {
		return LoginInput{}, errors.New("login grant is no longer active")
	}
	if err := e.record("login.extension_claimed", "extension", id, "Credential handed to paired native host"); err != nil {
		return LoginInput{}, err
	}
	r.Status = "extension_claimed"
	return LoginInput{Origin: r.Origin, Username: a.Username, Password: a.Password, ReadPath: r.ReadPath, Deadline: r.ExpiresAt}, nil
}

// The extension checks fixed authentication markers. This is a trusted
// extension report, not independent proof from the Go gateway.
func (e *Engine) CompleteExtensionLogin(id, status string, fields map[string]string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	r := e.loginRuns[id]
	if r == nil || r.Transport != "extension" || r.Status != "extension_claimed" {
		return errors.New("claimed extension login not found")
	}
	if status != "completed" && status != "login_failed" && status != "adapter_mismatch" && status != "network_error" {
		return errors.New("invalid extension result")
	}
	a := e.account(r.AccountID)
	if a == nil {
		return errors.New("account not found")
	}
	in := LoginInput{Username: a.Username, Password: a.Password, ReadPath: r.ReadPath}
	ev := LoginEvent{Status: status, Authenticated: status == "completed", Fields: fields}
	if !safeLoginEvent(ev, in) {
		return errors.New("invalid extension result")
	}
	if status == "completed" {
		a.LoginNeedsReview = false
	}
	beforeConnections := clone(e.state.ConnectionRequests)
	for _, c := range e.state.ConnectionRequests {
		if c.RunID == id {
			result := clone(*r)
			result.Status, result.Authenticated, result.Fields = status, ev.Authenticated, clone(fields)
			if status == "completed" {
				now := e.now().UTC()
				result.VerifiedAt = &now
			}
			c.Status, c.Result = status, &result
		}
	}
	if err := e.record("login."+status, "extension", id, "Trusted browser extension reported authentication status"); err != nil {
		e.state.ConnectionRequests = beforeConnections
		if status == "completed" {
			a.LoginNeedsReview = true
		}
		return err
	}
	r.Status, r.Authenticated, r.Fields = status, ev.Authenticated, clone(fields)
	if status == "completed" {
		now := e.now().UTC()
		r.VerifiedAt = &now
	}
	return nil
}
