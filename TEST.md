# Verification

Run `go test -race ./...` and `go vet ./...`. Use fake credentials and httptest upstreams.

Cases: default deny, deny precedence, path/method/agent separation, exact one-time approval, timeout/rejection/cancel, rule/service/agent invalidation, lease TTL/use count/concurrency, revoked tokens, admin authentication, secret-free snapshots, encrypted persistence, corrupt-state failure, no upstream before approval, redirect refusal, private IP rejection, path ambiguity, size limits and cross-origin rejection.

Browser: exercise four modes, approve/reject, grant/revoke lease, edit a rule, add agent/service, switch language, mobile layout and console. Cross-compile macOS, Linux and Windows. OS sandbox enforcement is outside this alpha.

## Credential handoff verification (v0.2)
`go test -race ./...`, `go vet ./...`, and JS syntax checks cover the new workflow. Tests exercise default-deny eligibility, deduplicated requests, cross-agent isolation, operator-only entry, sensitive-input error suppression, concurrent single fulfillment, encrypted persistence, save rollback, expiry/rejection/cancellation/config invalidation, no dispatch on entry, approval after entry, and Basic injection/redaction against a fake upstream. GUI verification uses only fake credentials.

## Browser handoff verification (v0.3)
Test operator-only confirmation, agent-only verification, actor allowlists, immutable context, exact final origin, duplicate/concurrent tab claims, expiry in both active states, cancellation, failed login, config invalidation, persistence failures and no dispatch. Browser checks must cover human-confirmation UI and status distinctions. A fake lifecycle and a real Tencent Cloud sign-in must be reported separately; manual completion remains required for the real account.

## Controlled login (v0.4)
Go race tests cover encrypted account persistence and sanitized snapshots, cross-agent isolation, atomic budgets, audit-save failure, configuration/revocation cancellation, deadline expiry, operator-only resume, restart invalidation, secret/error protocol filtering and credential transport via stdin. `npm test --prefix browser-worker` exercises the actual workflow against browser-routed fake CloudCone pages: automatic ordinary login, CAPTCHA pause, false human confirmation, credential-bearing redirect rejection and fixed inventory result filtering. No fake credentials are sent to the real provider.

Local GUI: save fake website account, inspect masked/cleared input behavior and grant form. Remove the fake account afterward. Real CloudCone login and live inventory extraction remain separate acceptance gates requiring user credential entry and CAPTCHA, and are not claimed by these tests.
