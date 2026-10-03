# Grantide / 允界 · v0.1

## Goal
An independently developed, local-first permission gateway with a browser GUI. An operator can let an AI agent call an API automatically, require approval for each call, grant temporary access, or forbid an operation. Demonstrate every mode against a fake upstream with observable call counts.

## Users and flow
Developers running coding agents and automation. The operator adds an agent and an HTTP service, configures scoped rules, and gives the agent its own gateway token. The agent submits an exact request. The gateway executes, queues for review, or denies it. The operator reviews method, destination, query and body, grants one call or a bounded lease, and can revoke access. The agent polls for the result.

## MVP scope
- Self-contained Go binary, embedded responsive bilingual Web GUI.
- Agent identities/revocation; encrypted service credentials; service enable/disable.
- Rules scoped by agent, service, method and path prefix: auto, approval, lease, deny.
- Deny overrides other rules. Overlapping other rules select the most restrictive mode, then the narrowest scope. No match denies.
- Lease bound to agent and configuration revision, with expiry and atomic call limit; optional rule expiry for temporary pre-authorized access.
- Exact held-request approval, rejection, timeout, cancellation and result polling.
- GUI rules, approval inbox, active leases, services, agents, audit and sandbox playground.
- CLI integration, safe demo, tests and independent public GitHub repository.

## Boundaries
v0.1 brokers explicit JSON HTTP API requests. It does not intercept all desktop traffic, execute SSH/shell commands, provide MCP transport or protect against an agent with access to the operator's OS account/data directory. Keep the gateway and its files outside the agent sandbox. Hosted multi-user management and distributed leases are future work. No real provider credentials in development/demo.

## Acceptance
Auto calls reach upstream once. Approval calls reach it only after approval, with exactly the displayed payload. Denied/rejected/expired calls never reach it. Leases enforce actor, scope, time and concurrent use counts. Configuration changes invalidate pending authorizations and leases. Agents cannot use admin endpoints or see other agents' requests. No credentials in snapshots. Redirects cannot forward credentials. Private destinations require operator opt-in. Configuration persists; pending calls and leases do not survive restart. GUI exercises all modes. Race tests, vet and browser verification pass.

## Decisions / DoR
Go standard library plus native browser modules; no cloud dependency. Encrypted private local state; loopback binding. MIT license, experimental alpha. Project name changed from Agent Permit to Grantide at the user's request for a product name. Goal, scope, flow, constraints, risks and acceptance are defined; no blocking unanswered requirement.

## Credential handoff (2026-09-28)
Agents can request credential entry for a configured service and a policy-covered method/path. The operator sees the trusted origin and requesting agent, supplies a header token or HTTP Basic username/password in the local GUI, or rejects the request. Five-minute requests expire, cancel, and invalidate on configuration changes. Agent APIs return metadata/status only; entering credentials does not authorize execution. Saved service credentials apply to all authorized callers of that service. Generic browser form login, cookies and MFA are outside this increment.

Acceptance: separate operator authentication for entry; cross-agent status isolation; no plaintext in snapshots/audit/agent responses; no dispatch on entry; encrypted persistence; exact operation still follows policy; invalidation, expiry and failed saves fail closed.

## Browser login handoff (v0.3)
Goal: a browser agent pauses on login, records the exact browser/profile/tab and owning conversation, lets the human complete password/QR/MFA on the original website, then verifies the authenticated page before resuming. The GUI is a handoff inbox, not a password-proxy form. Operator-configured HTTPS sites and actor allowlists bound requests. No password, OTP, cookie, OAuth URL parameters or browser-state export enters Grantide. Human confirmation and agent verification are separate states. Fifteen-minute handoffs expire and are invalidated by configuration changes.

Acceptance: operator-only human confirmation; actor-only verification after confirmation; same browser/tab/site and allowed final origin on verification; cross-agent isolation; duplicate prevention; bounded queue; failed saves fail closed; cancellation/config change/expiry block continuation. UI distinguishes waiting, awaiting verification, verified, and failure. Verify the local flow with a fake website; record the selected provider's real login separately and never label simulated login as real completion. Existing browser tools must cooperate with the pause; this version cannot block independent screenshots/browser tools or automatically wake another Codex conversation. Login handoff grants no purchase or account-change permission.

## Controlled website login (v0.4 scope correction)
The operator saves a website account and password in Grantide, then creates a grant bound to the fixed CloudCone origin, saved account, one agent, an expiry and a use budget. The agent submits only a grant ID. A separately launched local browser worker receives the password over stdin, opens its own ephemeral browser context, fills the provider form and submits ordinary login. CAPTCHA/MFA pauses the worker until the operator confirms completion; only the worker checks authentication. Credentials, cookies, raw DOM, screenshots and browser handles never appear in its agent API. A grant can additionally permit extracting fixed inventory fields from one operator-selected instance overview URL. No general browser actions, arbitrary JavaScript, account changes or purchases are exposed.

Acceptance: encrypted persistence, secret-free API/audit/errors, owner isolation, atomic use budgets, expiry/revocation/configuration cancellation, operator-only challenge resume, no credential-bearing redirect, strict adapter destinations/actions, fake-browser end-to-end coverage. CloudCone real-account verification requires operator credential entry and CAPTCHA. Existing Safari/IAB sessions are not imported. Same-OS-user filesystem/debugger/screen access is outside this boundary; use a separate OS account/container for stronger isolation. Saving credentials alone never dispatches a login.

## Login setup refinements (2026-09-30)
Account labels are suggested and editable. Agents can prepare a local account-entry link with a nonsecret contextual label; existing saved names and human-entered credentials are not replaced. Login grants offer explicit no-expiry duration, retaining independent use limits and per-run deadlines. Grants, budgets and revocations survive restart; configuration changes invalidate them.

## Unlimited-use login grants (2026-09-30)
Operators can explicitly choose unlimited login uses independently of expiry. Existing exhausted grants remain exhausted. Account, agent, fixed origin and exact optional read scope remain bound; revocation and ten-minute run ceilings still apply. Starts for one account are at least 60 seconds apart across grants and restarts. Each dispatch durably pauses further account logins until verified success; failure, interruption or crash leaves the pause for operator review. Review preserves the cooldown and grants no new scope. CAPTCHA/MFA still pauses the active worker.

Acceptance: explicit opt-in only, finite/unlimited scope isolation, persistent per-account throttle and failure pause, one concurrent worker, atomic save rollback, operator-only review, live GUI selection, and exact `/vps/NUMBER/manage` path checks. Path support does not establish that the live page's fields are extractable.

## Chrome extension increment (2026-09-30)
Goal: execute a grant-scoped CloudCone login in the operator's ordinary Chrome profile. Flow: install and pair an exact extension ID with the local native host, invoke a grant with `web-login --extension`, fill the fixed form, handle website CAPTCHA/MFA, and return bounded verification/fields. Account, actor, use, expiry, cooldown and failure controls apply across both adapters.

Acceptance: one-time operator-authenticated credential claim; no operator token in the extension; no credential in agent responses/audit/extension storage; fixed origin/form/read scope; shared durable retry guard; cancellation/revocation/expiry/configuration invalidation; validated results; native host restricted to an exact extension ID and three operations; fake-page and protocol verification; dated real-login evidence before provider-success claims. Initial distribution is unpacked Chrome 120+ with macOS/Linux native-host installation.

The Chrome session remains in Chrome and is separate from Codex's in-app browser. This is not the isolated worker's network/session boundary. Existing authenticated pages cannot be credited to the saved account without an account-identity adapter, so this version waits for a login form. Store publication and other browsers/platforms require later validation.

## Operator password and remembered login (2026-10-03)

The operator can use a configurable human password to enter the local console, selecting remembered login for 30 days. A browser session cookie replaces manual repeated entry of the native/API operator token. Password setup happens through a local stdin CLI, never a tracked default. The native bridge retains its existing operator token and Agent credentials/grants remain unchanged.

Acceptance: salted password hashing, no plaintext in tracked/runtime auth records, failed-login throttle, HttpOnly/SameSite cookie, persistent remembered sessions, session expiry and logout revocation, cross-origin rejection, Agent/admin separation, and GUI login/reopen verification. Password reconfiguration clears console sessions without mutating provider accounts or Agent grants.
