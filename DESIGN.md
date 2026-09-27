# Design

Agent → authenticated request API → policy decision → approval/lease → HTTP executor → upstream.
Operator → browser GUI → separate admin API → configuration, review, revocation and audit.

The executor accepts a service ID, never an arbitrary destination. The operator configures service origins. Canonical absolute ASCII paths only; query values are separate. No redirects or ambient proxies. DNS results are checked at dial time, and the checked IP is dialed directly. Public services require HTTPS; internal HTTP/private targets need per-service opt-in. Credentials are injected only after authorization and omitted from snapshots.

A mutex serializes decisions, lease consumption and configuration mutations. Network I/O runs outside it after final reservation. Revocation blocks future reservations; already-dispatched requests cannot be undone. Configuration changes invalidate pending approvals and leases. Deny wins; other modes rank approval, lease, auto. Ties use agent/path/method specificity and stable ID. Rule expiration causes default deny rather than falling through to a weaker rule.

AES-256-GCM encrypts persistent configuration and a bounded metadata audit using a random key file. Atomic replacement and fsync precede dispatch. Directory/files use 0700/0600. A single-instance lock prevents concurrent writers. Tokens and key files must remain outside agent access; encryption is not an OS isolation boundary. Pending bodies, responses and leases are bounded in process memory and disappear on restart. Only agent token hashes are persisted.

The GUI uses a separate bearer token, same-origin calls, no third-party assets and text-only rendering of untrusted values. Strict CSP and Origin/Host checks protect the local surface. Inputs reject unknown fields and enforce size limits. Demo requests use a fixed demo agent and a fake executor.

Go 1.25+. Primary references checked 2026-09-27: https://pkg.go.dev/net/http#Client, https://pkg.go.dev/crypto/cipher#NewGCM, https://pkg.go.dev/net/netip.
