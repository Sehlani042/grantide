package gateway

import (
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode"
)

const BrowserHandoffTTL = 15 * time.Minute

type BrowserSite struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	LoginURL string `json:"login_url"`
	AgentID  string `json:"agent_id"`
	Enabled  bool   `json:"enabled"`
}

type BrowserTarget struct {
	SiteID      string `json:"site_id"`
	Browser     string `json:"browser"`
	TabID       string `json:"tab_id"`
	ThreadID    string `json:"thread_id,omitempty"`
	ThreadTitle string `json:"thread_title,omitempty"`
}

type BrowserHandoff struct {
	ID        string        `json:"id"`
	AgentID   string        `json:"agent_id"`
	Target    BrowserTarget `json:"target"`
	LoginURL  string        `json:"login_url"`
	Revision  int           `json:"revision"`
	Status    string        `json:"status"`
	CreatedAt time.Time     `json:"created_at"`
	ExpiresAt time.Time     `json:"expires_at"`
}

func handoffActive(s string) bool { return s == "waiting" || s == "verification_required" }
func labelOK(s string, limit int) bool {
	return strings.TrimSpace(s) != "" && len(s) <= limit && !strings.ContainsFunc(s, unicode.IsControl)
}
func browserURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || strings.Contains(raw, "\\") {
		return nil, errors.New("use an HTTPS website URL without credentials, query or fragment")
	}
	// Keep destinations visually unambiguous; international domains use explicit punycode.
	for _, r := range u.Host {
		if r > 127 || r <= 32 {
			return nil, errors.New("use an ASCII website hostname")
		}
	}
	return u, nil
}
func (e *Engine) browserSite(id string) *BrowserSite {
	for i := range e.state.BrowserSites {
		if e.state.BrowserSites[i].ID == id {
			return &e.state.BrowserSites[i]
		}
	}
	return nil
}
func (e *Engine) PutBrowserSite(s BrowserSite) (BrowserSite, error) {
	if s.ID == "" {
		s.ID = newID("site")
	}
	if !validID.MatchString(s.ID) || !labelOK(s.Name, 100) {
		return BrowserSite{}, errors.New("invalid website ID or name")
	}
	if _, err := browserURL(s.LoginURL); err != nil {
		return BrowserSite{}, err
	}
	err := e.mutate("browser_site.updated", s.ID, func() error {
		if s.AgentID != "*" && e.agent(s.AgentID) == nil {
			return errors.New("select an agent or *")
		}
		if old := e.browserSite(s.ID); old != nil {
			*old = s
		} else {
			if len(e.state.BrowserSites) >= 100 {
				return errors.New("website limit reached")
			}
			e.state.BrowserSites = append(e.state.BrowserSites, s)
		}
		return nil
	})
	return s, err
}
func (e *Engine) RequestBrowserHandoff(actor string, target BrowserTarget) (BrowserHandoff, error) {
	if !labelOK(target.Browser, 160) || !labelOK(target.TabID, 160) || (target.ThreadID != "" && !validID.MatchString(target.ThreadID)) || (target.ThreadTitle != "" && !labelOK(target.ThreadTitle, 160)) {
		return BrowserHandoff{}, errors.New("supply browser/profile and tab identifiers; labels cannot contain control characters")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	a, s := e.agent(actor), e.browserSite(target.SiteID)
	if a == nil || !a.Enabled || s == nil || !s.Enabled || (s.AgentID != "*" && s.AgentID != actor) {
		return BrowserHandoff{}, errors.New("website handoff is not allowed for this agent")
	}
	count := 0
	for _, r := range e.handoffs {
		if !handoffActive(r.Status) {
			continue
		}
		if r.Target.Browser == target.Browser && r.Target.TabID == target.TabID {
			if r.AgentID == actor && r.Target == target {
				return *r, nil
			}
			return BrowserHandoff{}, errors.New("this browser tab already has an active handoff")
		}
		if r.AgentID == actor {
			count++
		}
	}
	if count >= 8 {
		return BrowserHandoff{}, errors.New("too many active browser handoffs")
	}
	if len(e.handoffs) >= 128 {
		var oldest *BrowserHandoff
		for _, r := range e.handoffs {
			if !handoffActive(r.Status) && (oldest == nil || r.CreatedAt.Before(oldest.CreatedAt)) {
				oldest = r
			}
		}
		if oldest == nil {
			return BrowserHandoff{}, errors.New("browser handoff capacity reached")
		}
		delete(e.handoffs, oldest.ID)
	}
	now := e.now().UTC()
	r := BrowserHandoff{ID: newID("browser"), AgentID: actor, Target: target, LoginURL: s.LoginURL, Revision: e.state.Revision, Status: "waiting", CreatedAt: now, ExpiresAt: now.Add(BrowserHandoffTTL)}
	if err := e.record("browser.requested", actor, r.ID, "Human login requested for "+s.ID); err != nil {
		return BrowserHandoff{}, err
	}
	e.handoffs[r.ID] = &r
	return r, nil
}
func (e *Engine) GetBrowserHandoff(actor, id string) (BrowserHandoff, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	r := e.handoffs[id]
	if r == nil || r.AgentID != actor {
		return BrowserHandoff{}, false
	}
	return *r, true
}
func (e *Engine) BrowserDecision(id, decision string) (BrowserHandoff, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	r := e.handoffs[id]
	if r == nil || r.Status != "waiting" {
		return BrowserHandoff{}, errors.New("browser handoff is no longer waiting")
	}
	status := "verification_required"
	if decision == "reject" {
		status = "rejected"
	} else if decision != "done" {
		return BrowserHandoff{}, errors.New("choose done or reject")
	}
	if err := e.record("browser."+status, "operator", id, "Human decision recorded; no website operation authorized"); err != nil {
		return BrowserHandoff{}, err
	}
	r.Status = status
	return *r, nil
}
func (e *Engine) CancelBrowserHandoff(actor, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	r := e.handoffs[id]
	if r == nil || r.AgentID != actor || !handoffActive(r.Status) {
		return errors.New("active browser handoff not found")
	}
	if err := e.record("browser.cancelled", actor, id, "Browser handoff cancelled"); err != nil {
		return err
	}
	r.Status = "cancelled"
	return nil
}

// This records the cooperating browser adapter's observation, not server-side authentication proof.
func (e *Engine) VerifyBrowserHandoff(actor, id string, target BrowserTarget, origin string, authenticated bool) (BrowserHandoff, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.expire()
	r := e.handoffs[id]
	if r == nil || r.AgentID != actor || r.Status != "verification_required" {
		return BrowserHandoff{}, errors.New("human confirmation is required before verification")
	}
	a, s := e.agent(actor), e.browserSite(r.Target.SiteID)
	if a == nil || !a.Enabled || s == nil || !s.Enabled || r.Revision != e.state.Revision {
		return BrowserHandoff{}, errors.New("handoff invalidated")
	}
	if target != r.Target {
		return BrowserHandoff{}, errors.New("verification must use the original browser, tab and conversation")
	}
	u, err := browserURL(origin)
	if err != nil {
		return BrowserHandoff{}, err
	}
	expected, _ := url.Parse(r.LoginURL)
	if (u.Path != "" && u.Path != "/") || u.Host != expected.Host {
		return BrowserHandoff{}, errors.New("verify the configured website origin after login redirects finish")
	}
	status := "login_failed"
	if authenticated {
		status = "verified"
	}
	if err := e.record("browser."+status, actor, id, "Agent reported login verification; no purchase or account change approved"); err != nil {
		return BrowserHandoff{}, err
	}
	r.Status = status
	return *r, nil
}
