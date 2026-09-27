package gateway

import "time"

const Version = "0.1.0-alpha.1"
const MaxBody = 64 << 10
const PendingTTL = 5 * time.Minute

type Agent struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	TokenHash string `json:"token_hash,omitempty"`
}

type Service struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Origin       string `json:"origin"`
	Enabled      bool   `json:"enabled"`
	AllowPrivate bool   `json:"allow_private"`
	AuthHeader   string `json:"auth_header,omitempty"`
	AuthPrefix   string `json:"auth_prefix,omitempty"`
	Secret       string `json:"secret,omitempty"`
	SecretSet    bool   `json:"secret_set,omitempty"`
}

type Rule struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	AgentID      string     `json:"agent_id"`
	ServiceID    string     `json:"service_id"`
	Methods      []string   `json:"methods"`
	PathPrefix   string     `json:"path_prefix"`
	Mode         string     `json:"mode"`
	Enabled      bool       `json:"enabled"`
	LeaseSeconds int        `json:"lease_seconds"`
	MaxUses      int        `json:"max_uses"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

type Call struct {
	ServiceID string            `json:"service_id"`
	Method    string            `json:"method"`
	Path      string            `json:"path"`
	Query     map[string]string `json:"query,omitempty"`
	Body      string            `json:"body,omitempty"`
}

type Result struct {
	Status    int    `json:"status"`
	Body      string `json:"body"`
	Truncated bool   `json:"truncated"`
}

type Request struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Call      Call      `json:"call"`
	Origin    string    `json:"origin"`
	RuleID    string    `json:"rule_id,omitempty"`
	Mode      string    `json:"mode"`
	Revision  int       `json:"revision"`
	Status    string    `json:"status"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Result    *Result   `json:"result,omitempty"`
}

type Lease struct {
	ID         string    `json:"id"`
	AgentID    string    `json:"agent_id"`
	RuleID     string    `json:"rule_id"`
	ServiceID  string    `json:"service_id"`
	PathPrefix string    `json:"path_prefix"`
	Methods    []string  `json:"methods"`
	Revision   int       `json:"revision"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Remaining  int       `json:"remaining"`
	Total      int       `json:"total"`
	Revoked    bool      `json:"revoked"`
}

type Event struct {
	ID     string    `json:"id"`
	At     time.Time `json:"at"`
	Action string    `json:"action"`
	Actor  string    `json:"actor"`
	Target string    `json:"target"`
	Detail string    `json:"detail"`
}

type State struct {
	Schema   int       `json:"schema"`
	Revision int       `json:"revision"`
	Agents   []Agent   `json:"agents"`
	Services []Service `json:"services"`
	Rules    []Rule    `json:"rules"`
	Audit    []Event   `json:"audit"`
}

type Snapshot struct {
	Version    string    `json:"version"`
	Revision   int       `json:"revision"`
	Demo       bool      `json:"demo"`
	ServerTime time.Time `json:"server_time"`
	Agents     []Agent   `json:"agents"`
	Services   []Service `json:"services"`
	Rules      []Rule    `json:"rules"`
	Requests   []Request `json:"requests"`
	Leases     []Lease   `json:"leases"`
	Audit      []Event   `json:"audit"`
}
