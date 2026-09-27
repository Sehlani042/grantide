package gateway

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var validPath = regexp.MustCompile(`^/[A-Za-z0-9_./~\-]*$`)
var validID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)
var validHeader = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,63}$`)

func pathOK(p string) bool {
	if len(p) > 2048 || !validPath.MatchString(p) || strings.Contains(p, "//") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}
func methodOK(m string) bool {
	switch m {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
		return true
	}
	return false
}
func prefixMatches(prefix, path string) bool {
	return prefix == "/" || path == prefix || strings.HasPrefix(path, strings.TrimSuffix(prefix, "/")+"/")
}
func hasMethod(rule Rule, method string) bool {
	for _, m := range rule.Methods {
		if m == method {
			return true
		}
	}
	return false
}
func modeRank(mode string) int {
	switch mode {
	case "deny":
		return 4
	case "approval":
		return 3
	case "lease":
		return 2
	case "auto":
		return 1
	}
	return 4
}

// Expired matching rules become a denial so a broader auto rule cannot reopen access.
func evaluate(rules []Rule, actor string, call Call, now time.Time) (Rule, string) {
	var best Rule
	bestScore := -1
	for _, r := range rules {
		if !r.Enabled || (r.AgentID != "*" && r.AgentID != actor) || r.ServiceID != call.ServiceID || !hasMethod(r, call.Method) || !prefixMatches(r.PathPrefix, call.Path) {
			continue
		}
		mode := r.Mode
		if r.ExpiresAt != nil && !now.Before(*r.ExpiresAt) {
			mode = "deny"
		}
		score := modeRank(mode)*1000000 + len(r.PathPrefix)*100 + (8 - len(r.Methods))
		if r.AgentID != "*" {
			score += 300000
		}
		if score > bestScore || (score == bestScore && r.ID < best.ID) {
			best, bestScore = r, score
			best.Mode = mode
		}
	}
	if bestScore < 0 {
		return Rule{}, "No matching rule; default deny"
	}
	if best.Mode == "deny" {
		return best, "Explicit deny or expired rule"
	}
	return best, "Matched " + best.Name
}

func validateService(s Service, demo bool) error {
	if !validID.MatchString(s.ID) || strings.TrimSpace(s.Name) == "" || len(s.Name) > 100 {
		return errors.New("service needs a valid ID and name")
	}
	if demo && s.Origin == "demo://sandbox" {
		return nil
	}
	u, err := url.Parse(s.Origin)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.Opaque != "" {
		return errors.New("origin must be an HTTP(S) origin without path, query or credentials")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && s.AllowPrivate) {
		return errors.New("HTTPS required; internal HTTP requires private-network opt-in")
	}
	if s.AuthHeader != "" {
		if !validHeader.MatchString(s.AuthHeader) {
			return errors.New("invalid auth header")
		}
		switch strings.ToLower(s.AuthHeader) {
		case "host", "connection", "content-length", "transfer-encoding", "trailer", "upgrade", "proxy-authorization", "cookie", "content-type":
			return errors.New("reserved auth header")
		}
	}
	if len(s.Secret) > 8192 || len(s.AuthPrefix) > 64 || strings.ContainsAny(s.Secret+s.AuthPrefix, "\r\n") {
		return errors.New("invalid secret or prefix")
	}
	if s.Secret != "" && s.AuthHeader == "" {
		return errors.New("set an auth header for the credential")
	}
	return nil
}

func validateRule(r Rule) error {
	if !validID.MatchString(r.ID) || strings.TrimSpace(r.Name) == "" || len(r.Name) > 100 {
		return errors.New("rule needs a valid ID and name")
	}
	if !pathOK(r.PathPrefix) {
		return errors.New("use a canonical absolute path prefix (no encoding, query, dot segments or wildcards)")
	}
	if len(r.Methods) == 0 || len(r.Methods) > 7 {
		return errors.New("choose one or more HTTP methods")
	}
	seen := map[string]bool{}
	for _, m := range r.Methods {
		if !methodOK(m) || seen[m] {
			return errors.New("invalid or duplicate method")
		}
		seen[m] = true
	}
	switch r.Mode {
	case "auto", "approval", "lease", "deny":
	default:
		return errors.New("invalid permission mode")
	}
	if r.Mode == "lease" && (r.LeaseSeconds < 1 || r.LeaseSeconds > 3600 || r.MaxUses < 1 || r.MaxUses > 100) {
		return errors.New("lease must be 1–3600 seconds and 1–100 calls")
	}
	return nil
}
