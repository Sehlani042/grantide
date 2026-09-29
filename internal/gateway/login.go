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

var instancePath = regexp.MustCompile(`^/(?:compute/[0-9]+|vps/[0-9]+(?:/manage)?)$`)

const loginInterval = time.Minute

type WebAccount struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Origin           string     `json:"origin"`
	Username         string     `json:"username,omitempty"`
	Password         string     `json:"password,omitempty"`
	Enabled          bool       `json:"enabled"`
	SecretSet        bool       `json:"secret_set,omitempty"`
	NextLoginAt      *time.Time `json:"next_login_at,omitempty"`
	LoginNeedsReview bool       `json:"login_needs_review"`
}
type LoginGrant struct {
	ID            string     `json:"id"`
	AccountID     string     `json:"account_id"`
	AgentID       string     `json:"agent_id"`
	Origin        string     `json:"origin"`
	ReadPath      string     `json:"read_path,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at"`
	Revision      int        `json:"revision"`
	Remaining     int        `json:"remaining"`
	UnlimitedUses bool       `json:"unlimited_uses"`
	Revoked       bool       `json:"revoked"`
}
type LoginGrantView struct {
	LoginGrant
	NextLoginAt      *time.Time `json:"next_login_at,omitempty"`
	LoginNeedsReview bool       `json:"login_needs_review"`
}
type LoginRun struct {
	ID            string            `json:"id"`
	GrantID       string            `json:"grant_id"`
	AgentID       string            `json:"agent_id"`
	AccountID     string            `json:"account_id"`
	Origin        string            `json:"origin"`
	ReadPath      string            `json:"read_path,omitempty"`
	Status        string            `json:"status"`
	Transport     string            `json:"transport,omitempty"`
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

func (g LoginGrant) expired(now time.Time) bool {
	return g.ExpiresAt != nil && !now.Before(*g.ExpiresAt)
}

func loginActive(s string) bool {
	return s == "executing" || s == "human_required" || s == "extension_pending" || s == "extension_claimed"
}
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
		// These controls belong to the executor, never to account form input.
		a.NextLoginAt, a.LoginNeedsReview = nil, false
		if old != nil {
			a.NextLoginAt, a.LoginNeedsReview = old.NextLoginAt, old.LoginNeedsReview
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
	return e.CreateLoginGrantWithLimit(account, actor, path, seconds, uses, false)
}
func (e *Engine) CreateLoginGrantWithLimit(account, actor, path string, seconds, uses int, unlimited bool) (LoginGrant, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	a, agent := e.account(account), e.agent(actor)
	validUses := (!unlimited && uses >= 1 && uses <= 20) || (unlimited && uses == 0)
	if a == nil || !a.Enabled || agent == nil || !agent.Enabled || (seconds != 0 && (seconds < 30 || seconds > 86400)) || !validUses || (path != "" && !instancePath.MatchString(path)) {
		return LoginGrant{}, errors.New("select an enabled account and agent, 0 (no expiry) or 30–86400 seconds, 1–20 uses or explicit unlimited_uses with uses=0, and an optional exact instance overview path")
	}
	if len(e.state.LoginGrants) >= 128 {
		for id, g := range e.state.LoginGrants {
			if g.Revoked || g.expired(e.now()) {
				delete(e.state.LoginGrants, id)
			}
		}
		if len(e.state.LoginGrants) >= 128 {
			return LoginGrant{}, errors.New("grant limit reached")
		}
	}
	g := LoginGrant{ID: newID("login_grant"), AccountID: a.ID, AgentID: actor, Origin: a.Origin, ReadPath: path, Revision: e.state.Revision, Remaining: uses, UnlimitedUses: unlimited}
	if seconds > 0 {
		expiry := e.now().UTC().Add(time.Duration(seconds) * time.Second)
		g.ExpiresAt = &expiry
	}
	e.state.LoginGrants[g.ID] = &g
	if err := e.record("login_grant.created", "operator", g.ID, "Website login scope approved"); err != nil {
		delete(e.state.LoginGrants, g.ID)
		return LoginGrant{}, err
	}
	return g, nil
}
func (e *Engine) revokeLoginLocked(g *LoginGrant) {
	g.Revoked = true
	for _, r := range e.loginRuns {
		if r.GrantID == g.ID && loginActive(r.Status) {
			r.Status = "cancelled"
			if r.cancel != nil {
				r.cancel()
			}
		}
	}
}
func (e *Engine) RevokeLoginGrant(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	g := e.state.LoginGrants[id]
	if g == nil {
		return errors.New("grant not found")
	}
	previous := g.Revoked
	g.Revoked = true
	if err := e.record("login_grant.revoked", "operator", id, "Login cancellation requested; dispatched website actions cannot be recalled"); err != nil {
		g.Revoked = previous
		return err
	}
	e.revokeLoginLocked(g)
	return nil
}
func (e *Engine) loginGrantView(g LoginGrant) LoginGrantView {
	v := LoginGrantView{LoginGrant: g}
	if a := e.account(g.AccountID); a != nil {
		v.NextLoginAt, v.LoginNeedsReview = a.NextLoginAt, a.LoginNeedsReview
	}
	return clone(v)
}
func (e *Engine) LoginGrants(actor string) []LoginGrantView {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	out := []LoginGrantView{}
	for _, g := range e.state.LoginGrants {
		if g.AgentID == actor {
			out = append(out, e.loginGrantView(*g))
		}
	}
	return out
}
func (e *Engine) StartLogin(actor, grant string) (LoginRun, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	g := e.state.LoginGrants[grant]
	if g == nil || g.AgentID != actor || g.Revision != e.state.Revision || g.Revoked || (!g.UnlimitedUses && g.Remaining < 1) || g.expired(e.now()) {
		return LoginRun{}, errors.New("active login grant not found")
	}
	a, agent := e.account(g.AccountID), e.agent(actor)
	if a == nil || !a.Enabled || agent == nil || !agent.Enabled || e.loginRunner == nil {
		return LoginRun{}, errors.New("login is unavailable")
	}
	if e.loginBusy || e.extensionBusyLocked() || e.loginClosed {
		return LoginRun{}, errors.New("a controlled browser is already active")
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
	r := &LoginRun{ID: newID("login"), GrantID: g.ID, AgentID: actor, AccountID: a.ID, Origin: g.Origin, ReadPath: g.ReadPath, Status: "executing", Transport: "worker", CreatedAt: now, ExpiresAt: deadline, resume: make(chan struct{}, 1)}
	previousRemaining, previousNext, previousReview := g.Remaining, a.NextLoginAt, a.LoginNeedsReview
	if !g.UnlimitedUses {
		g.Remaining--
	}
	next := now.Add(loginInterval)
	// Persist before launch: even a crash cannot reset the throttle or retry a failed login.
	a.NextLoginAt, a.LoginNeedsReview = &next, true
	if err := e.record("login.started", actor, r.ID, "Login reservation and account retry guard persisted"); err != nil {
		g.Remaining, a.NextLoginAt, a.LoginNeedsReview = previousRemaining, previousNext, previousReview
		return LoginRun{}, err
	}
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

// Only the operator may acknowledge a failed/interrupted login. The cooldown remains.
func (e *Engine) ReviewAccountLogin(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	a := e.account(id)
	if a == nil || !a.LoginNeedsReview {
		return errors.New("account is not waiting for login review")
	}
	if e.loginBusy || e.extensionBusyLocked() {
		return errors.New("wait for the controlled browser to close")
	}
	a.LoginNeedsReview = false
	if err := e.record("login.reviewed", "operator", id, "Operator cleared account login pause; cooldown retained"); err != nil {
		a.LoginNeedsReview = true
		return err
	}
	return nil
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
		a := e.account(r.AccountID)
		if ev.Status == "completed" && a != nil {
			a.LoginNeedsReview = false
		}
		if err := e.record("login."+ev.Status, r.AgentID, id, "Controlled browser status changed"); err != nil {
			if ev.Status == "completed" && a != nil {
				a.LoginNeedsReview = true
			}
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
	if err := e.record("login.cancelled", actor, id, "Login cancellation requested; extension sessions remain in the browser"); err != nil {
		return err
	}
	r.Status = "cancelled"
	if r.cancel != nil {
		r.cancel()
	}
	return nil
}
func (e *Engine) CloseLogins() {
	e.mu.Lock()
	e.loginClosed = true
	for _, r := range e.loginRuns {
		if loginActive(r.Status) {
			r.Status = "cancelled"
			if r.cancel != nil {
				r.cancel()
			}
		}
	}
	e.mu.Unlock()
	e.loginWG.Wait()
}
func (e *Engine) loginSnapshot(s *Snapshot) {
	s.LoginWorkerReady = e.loginRunner != nil
	s.WebAccounts = []WebAccount{}
	s.LoginGrants = []LoginGrantView{}
	s.LoginRuns = []LoginRun{}
	for _, a := range e.state.WebAccounts {
		s.WebAccounts = append(s.WebAccounts, publicAccount(a))
	}
	for _, g := range e.state.LoginGrants {
		s.LoginGrants = append(s.LoginGrants, e.loginGrantView(*g))
	}
	for _, r := range e.loginRuns {
		s.LoginRuns = append(s.LoginRuns, clone(*r))
	}
	sort.Slice(s.LoginRuns, func(i, j int) bool { return s.LoginRuns[i].CreatedAt.After(s.LoginRuns[j].CreatedAt) })
}
