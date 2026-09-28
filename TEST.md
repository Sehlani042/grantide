# Verification

Run `go test -race ./...` and `go vet ./...`. Use fake credentials and httptest upstreams.

Cases: default deny, deny precedence, path/method/agent separation, exact one-time approval, timeout/rejection/cancel, rule/service/agent invalidation, lease TTL/use count/concurrency, revoked tokens, admin authentication, secret-free snapshots, encrypted persistence, corrupt-state failure, no upstream before approval, redirect refusal, private IP rejection, path ambiguity, size limits and cross-origin rejection.

Browser: exercise four modes, approve/reject, grant/revoke lease, edit a rule, add agent/service, switch language, mobile layout and console. Cross-compile macOS, Linux and Windows. OS sandbox enforcement is outside this alpha.

## Credential handoff verification (v0.2)
`go test -race ./...`, `go vet ./...`, and JS syntax checks cover the new workflow. Tests exercise default-deny eligibility, deduplicated requests, cross-agent isolation, operator-only entry, sensitive-input error suppression, concurrent single fulfillment, encrypted persistence, save rollback, expiry/rejection/cancellation/config invalidation, no dispatch on entry, approval after entry, and Basic injection/redaction against a fake upstream. GUI verification uses only fake credentials.
