# Changelog

## 0.3.0-alpha.1 · 2026-09-29

- Added browser login handoff inbox, operator-managed website allowlist and CLI.
- Bound handoffs to agent, browser/profile, tab, site and owning conversation; separated human completion from agent verification.
- Added bounded lifecycle tests and documented cooperative pause limitations, restart/expiry behavior and original-tab integration.

## 0.2.0-alpha.1 · 2026-09-28

- Added actor-scoped credential requests, local GUI entry/rejection, expiry/cancellation and CLI polling.
- Added HTTP Basic username/password injection and redaction alongside header credentials.
- Saving credentials never dispatches, invalidates existing requests/grants and keeps policy enforcement intact.
- Added authorization, replay, encryption, rollback and fake Basic upstream coverage; documented Codex CLI integration and browser-login limits.

## 0.1.0-alpha.1 · 2026-09-27
- Independent Grantide / 允界 project at user's request.
- Four permission modes and a browser operator console.
- Explicit JSON HTTP gateway with local single-binary distribution.
- Encrypted configuration, separate operator/agent tokens, bounded audit and safe demo.
- Exact request review, default deny, policy expiry, revocable grants and atomic call budgets.
- Responsive Chinese/English console and CLI result polling.
- Race/integration verification and macOS/Linux/Windows release archives.
