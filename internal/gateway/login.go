package gateway

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
)

const CloudConeOrigin = "https://app.cloudcone.com"

var instancePath = regexp.MustCompile(`^/(compute|vps)/[0-9]+$`)

type WebAccount struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Origin    string `json:"origin"`
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
	Enabled   bool   `json:"enabled"`
	SecretSet bool   `json:"secret_set,omitempty"`
}
type LoginGrant struct {
	ID        string    `json:"id"`
	AccountID string    `json:"account_id"`
	AgentID   string    `json:"agent_id"`
	Origin    string    `json:"origin"`
	ReadPath  string    `json:"read_path,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
	Remaining int       `json:"remaining"`
	Revoked   bool      `json:"revoked"`
}
type LoginRun struct {
	ID            string            `json:"id"`
	GrantID       string            `json:"grant_id"`
	AgentID       string            `json:"agent_id"`
	AccountID     string            `json:"account_id"`
	Origin        string            `json:"origin"`
	ReadPath      string            `json:"read_path,omitempty"`
	Status        string            `json:"status"`
	CreatedAt     time.Time         `json:"created_at"`
	ExpiresAt     time.Time         `json:"expires_at"`
	Authenticated bool              `json:"authenticated"`
	Fields        map[string]string `json:"fields,omitempty"`
	VerifiedAt    *time.Time        `json:"verified_at,omitempty"`
	cancel        context.CancelFunc
	resume        chan struct{}
}

// This type is only passed to the trusted worker over a private pipe, never an API response.
type LoginInput struct {
	Origin   string    `json:"origin"`
	Username string    `json:"username"`
	Password string    `json:"password"`
	ReadPath string    `json:"read_path"`
	Deadline time.Time `json:"deadline"`
}
type LoginEvent struct {
	Status        string            `json:"status"`
	Authenticated bool              `json:"authenticated"`
	Fields        map[string]string `json:"fields,omitempty"`
}
type LoginRunner func(context.Context, LoginInput, func(LoginEvent) error, <-chan struct{}) error

func loginActive(s string) bool { return s == "executing" || s == "human_required" }
func publicAccount(a WebAccount) WebAccount {
	a.SecretSet = a.Password != ""
	a.Password = ""
	a.Username = ""
	return a
}
func (e *Engine) account(id string) *WebAccount {
	for i := range e.state.WebAccounts {
		if e.state.WebAccounts[i].ID == id {
			return &e.state.WebAccounts[i]
		}
	}
	return nil
}
func (e *Engine) SetLoginRunner(r LoginRunner) { e.mu.Lock(); defer e.mu.Unlock(); e.loginRunner = r }
func (e *Engine) PutWebAccount(a WebAccount) (WebAccount, error) {
	if a.ID == "" {
		a.ID = newID("account")
	}
	if !validID.MatchString(a.ID) || !labelOK(a.Name, 100) || a.Origin != CloudConeOrigin {
		return WebAccount{}, errors.New("select the supported CloudCone origin and a valid account label")
	}
	err := e.mutate("web_account.updated", a.ID, func() error {
		old := e.account(a.ID)
		if old != nil {
			if a.Username == "" {
				a.Username = old.Username
			}
			if a.Password == "" {
				a.Password = old.Password
			}
		}
		if !labelOK(a.Username, 256) || len(a.Password) == 0 || len(a.Password) > 4096 {
			return errors.New("a valid username and password are required")
		}
		a.SecretSet = false
		if old != nil {
			*old = a
		} else {
			if len(e.state.WebAccounts) >= 100 {
				return errors.New("account limit reached")
			}
			e.state.WebAccounts = append(e.state.WebAccounts, a)
		}
		return nil
	})
	return publicAccount(a), err
}
func (e *Engine) DeleteWebAccount(id string) error {
	return e.mutate("web_account.deleted", id, func() error {
		for i, a := range e.state.WebAccounts {
			if a.ID == id {
				e.state.WebAccounts = append(e.state.WebAccounts[:i], e.state.WebAccounts[i+1:]...)
				return nil
			}
		}
		return errors.New("account not found")
	})
}
func (e *Engine) CreateLoginGrant(account, actor, path string, seconds, uses int) (LoginGrant, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	a, agent := e.account(account), e.agent(actor)
	if e.loginRunner == nil {
		return LoginGrant{}, errors.New("controlled browser worker is not configured")
	}
	if a == nil || !a.Enabled || agent == nil || !agent.Enabled || seconds < 30 || seconds > 86400 || uses < 1 || uses > 20 || (path != "" && !instancePath.MatchString(path)) {
		return LoginGrant{}, errors.New("select an enabled account and agent, 30–86400 seconds, 1–20 uses, and an optional exact instance overview path")
	}
	if len(e.loginGrants) >= 128 {
		for id, g := range e.loginGrants {
			if g.Revoked || !e.now().Before(g.ExpiresAt) {
				delete(e.loginGrants, id)
			}
		}
		if len(e.loginGrants) >= 128 {
			return LoginGrant{}, errors.New("grant limit reached")
		}
	}
	g := LoginGrant{ID: newID("login_grant"), AccountID: a.ID, AgentID: actor, Origin: a.Origin, ReadPath: path, ExpiresAt: e.now().UTC().Add(time.Duration(seconds) * time.Second), Remaining: uses}
	if err := e.record("login_grant.created", "operator", g.ID, "Website login scope approved"); err != nil {
		return LoginGrant{}, err
	}
	e.loginGrants[g.ID] = &g
	return g, nil
}
func (e *Engine) revokeLoginLocked(g *LoginGrant) {
	g.Revoked = true
	for _, r := range e.loginRuns {
		if r.GrantID == g.ID && loginActive(r.Status) {
			r.Status = "cancelled"
			r.cancel()
		}
	}
}
func (e *Engine) RevokeLoginGrant(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	g := e.loginGrants[id]
	if g == nil {
		return errors.New("grant not found")
	}
	if err := e.record("login_grant.revoked", "operator", id, "Worker cancellation requested"); err != nil {
		return err
	}
	e.revokeLoginLocked(g)
	return nil
}
func (e *Engine) LoginGrants(actor string) []LoginGrant {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	out := []LoginGrant{}
	for _, g := range e.loginGrants {
		if g.AgentID == actor {
			out = append(out, *g)
		}
	}
	return out
}
func (e *Engine) StartLogin(actor, grant string) (LoginRun, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	g := e.loginGrants[grant]
	if g == nil || g.AgentID != actor || g.Revoked || g.Remaining < 1 || !e.now().Before(g.ExpiresAt) {
		return LoginRun{}, errors.New("active login grant not found")
	}
	a, agent := e.account(g.AccountID), e.agent(actor)
	if a == nil || !a.Enabled || agent == nil || !agent.Enabled || e.loginRunner == nil {
		return LoginRun{}, errors.New("login is unavailable")
	}
	if e.loginBusy || e.loginClosed {
		return LoginRun{}, errors.New("a controlled browser is already active")
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
	if g.ExpiresAt.Before(deadline) {
		deadline = g.ExpiresAt
	}
	r := &LoginRun{ID: newID("login"), GrantID: g.ID, AgentID: actor, AccountID: a.ID, Origin: g.Origin, ReadPath: g.ReadPath, Status: "executing", CreatedAt: now, ExpiresAt: deadline, resume: make(chan struct{}, 1)}
	if err := e.record("login.started", actor, r.ID, "Grant use consumed before controlled login"); err != nil {
		return LoginRun{}, err
	}
	g.Remaining--
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	r.cancel = cancel
	e.loginRuns[r.ID] = r
	e.loginBusy = true
	input := LoginInput{Origin: a.Origin, Username: a.Username, Password: a.Password, ReadPath: g.ReadPath, Deadline: deadline}
	runner := e.loginRunner
	e.loginWG.Add(1)
	go e.performLogin(ctx, r.ID, input, runner, r.resume)
	return clone(*r), nil
}

var fieldFormats = map[string]*regexp.Regexp{
	"cpu":        regexp.MustCompile(`(?i)^[0-9]{1,4}(?:\s*(?:v?cpus?|cores?))?$`),
	"memory":     regexp.MustCompile(`(?i)^[0-9.]{1,10}\s*(?:[KMGT]i?B)$`),
	"disk":       regexp.MustCompile(`(?i)^[0-9.]{1,10}\s*(?:[KMGT]i?B)(?:\s+(?:SSD|HDD|NVMe))?$`),
	"bandwidth":  regexp.MustCompile(`(?i)^[0-9.]{1,10}\s*(?:[KMGT]i?(?:B|bit)/?s|[KMGT]bps)$`),
	"traffic":    regexp.MustCompile(`(?i)^[0-9.]{1,10}\s*(?:[KMGT]i?B)(?:\s*/\s*month)?$`),
	"price":      regexp.MustCompile(`(?i)^(?:\$|USD\s*)[0-9.,]{1,15}(?:\s*/\s*(?:year|month|quarter))?$`),
	"next_due":   regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`),
	"auto_renew": regexp.MustCompile(`(?i)^(?:enabled|disabled|on|off|yes|no)$`),
}

func safeLoginEvent(ev LoginEvent, in LoginInput) bool {
	if len(ev.Fields) > len(fieldFormats) {
		return false
	}
	for k, v := range ev.Fields {
		re := fieldFormats[k]
		if re == nil || !re.MatchString(v) || strings.Contains(v, in.Password) || strings.Contains(v, in.Username) {
			return false
		}
	}
	if len(ev.Fields) > 0 && (!ev.Authenticated || in.ReadPath == "") {
		return false
	}
	switch ev.Status {
	case "human_required", "completed", "adapter_mismatch", "login_failed", "network_error":
	default:
		return false
	}
	if ev.Status != "completed" && (ev.Authenticated || len(ev.Fields) > 0) {
		return false
	}
	return ev.Status != "completed" || ev.Authenticated
}
func (e *Engine) performLogin(ctx context.Context, id string, in LoginInput, runner LoginRunner, resume <-chan struct{}) {
	defer e.loginWG.Done()
	defer func() { e.mu.Lock(); defer e.mu.Unlock(); e.loginBusy = false; e.loginRuns[id].cancel() }()
	emit := func(ev LoginEvent) error {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.expire()
		r := e.loginRuns[id]
		if r.Status != "executing" || ctx.Err() != nil {
			return errors.New("run cancelled")
		}
		if !safeLoginEvent(ev, in) {
			return errors.New("invalid worker event")
		}
		if err := e.record("login."+ev.Status, r.AgentID, id, "Controlled browser status changed"); err != nil {
			return err
		}
		r.Status = ev.Status
		r.Authenticated = ev.Authenticated
		r.Fields = clone(ev.Fields)
		if ev.Status == "completed" {
			now := e.now().UTC()
			r.VerifiedAt = &now
		}
		return nil
	}
	_ = runner(ctx, in, emit, resume)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	r := e.loginRuns[id]
	if loginActive(r.Status) {
		r.Status = "failed"
		_ = e.record("login.failed", r.AgentID, id, "Worker ended without a valid completion")
	}
}
func (e *Engine) GetLogin(actor, id string) (LoginRun, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	r := e.loginRuns[id]
	if r == nil || (actor != "operator" && r.AgentID != actor) {
		return LoginRun{}, false
	}
	return clone(*r), true
}
func (e *Engine) ResumeLogin(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	r := e.loginRuns[id]
	if r == nil || r.Status != "human_required" {
		return errors.New("run is not waiting for a human")
	}
	if err := e.record("login.resume", "operator", id, "Human finished; worker must verify authentication"); err != nil {
		return err
	}
	r.Status = "executing"
	r.resume <- struct{}{}
	return nil
}
func (e *Engine) CancelLogin(actor, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.loginRuns[id]
	if r == nil || (actor != "operator" && actor != r.AgentID) || !loginActive(r.Status) {
		return errors.New("active run not found")
	}
	if err := e.record("login.cancelled", actor, id, "Worker cancellation requested"); err != nil {
		return err
	}
	r.Status = "cancelled"
	r.cancel()
	return nil
}
func (e *Engine) CloseLogins() {
	e.mu.Lock()
	e.loginClosed = true
	for _, r := range e.loginRuns {
		if loginActive(r.Status) {
			r.Status = "cancelled"
			r.cancel()
		}
	}
	e.mu.Unlock()
	e.loginWG.Wait()
}
func (e *Engine) loginSnapshot(s *Snapshot) {
	s.LoginWorkerReady = e.loginRunner != nil
	s.WebAccounts = []WebAccount{}
	s.LoginGrants = []LoginGrant{}
	s.LoginRuns = []LoginRun{}
	for _, a := range e.state.WebAccounts {
		s.WebAccounts = append(s.WebAccounts, publicAccount(a))
	}
	for _, g := range e.loginGrants {
		s.LoginGrants = append(s.LoginGrants, *g)
	}
	for _, r := range e.loginRuns {
		s.LoginRuns = append(s.LoginRuns, clone(*r))
	}
	sort.Slice(s.LoginRuns, func(i, j int) bool { return s.LoginRuns[i].CreatedAt.After(s.LoginRuns[j].CreatedAt) })
}
