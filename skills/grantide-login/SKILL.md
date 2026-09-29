---
name: grantide-login
description: Use Grantide (允界) for saved-credential website login, account-label suggestions, scoped login grants and cross-chat login handoff. Applies when a task explicitly uses Grantide or an existing Grantide integration, including CloudCone provider verification. Other websites need their own reviewed adapter.
---

# Grantide login integration

Reuse the controlled login component and its evidence across tasks. This skill conveys operating knowledge; it does not supply credentials, a grant, or a shared browser session.

## Locate current truth

Resolve this skill directory's symlink to find the Grantide checkout two levels above it. Read [the current integration contract](../../docs/CONTROLLED-LOGIN.md) before invoking login, and [NOW.md](../../NOW.md) for dated verification and limitations. If the checkout is unavailable, ask for the installed Grantide location rather than searching unrelated private state.

Use the calling task's configured agent CLI/wrapper. Do not copy another task's token or use the operator token as an agent credential. Read a referenced Codex chat with `read_thread` before relying on its state. Existing chats may need this file's explicit path because an earlier loaded skill catalog may not contain a newly installed skill.

## Choose the matching capability

| Need | Entry point | Result |
| --- | --- | --- |
| Saved website credentials filled by the controlled worker | `web-login` | Verified run status and optional authorized fields |
| Human login in an existing independent browser | `browser-login` | Cooperative handoff and reported verification |
| API key or HTTP Basic entry for an HTTP service | `credential` | Credential-entry status; execution remains separately authorized |

The manual handoff is not a substitute for a request to use saved credentials automatically. As of 2026-09-30, the controlled worker supports CloudCone only.

## Invoke without rediscovering credentials

1. Use the configured agent wrapper with `web-login --list`. Check the actual grant's owner, account, origin, read scope, remaining uses, revocation and expiry. A historical successful run is not a fresh grant.
2. If account setup is needed, suggest a nonsecret editable label using `web-login --setup --label 'CloudCone · server inventory'`. Open its returned local setup URL. Existing account labels and user drafts should be preserved. The user enters credentials in the GUI, never in chat.
3. When an applicable grant and the task authorization already exist, invoke `web-login --grant GRANT_REF --wait 30s`; do not ask for the same authorization again.
4. Keep the returned run ID and poll with `web-login --id RUN_REF --wait 30s`. A CLI wait timeout leaves the run active. Do not create duplicate runs to check progress.
5. Inspect the returned status and evidence before continuing the requested task.

`expires_at: null` means no expiry, not unlimited attempts or an unlimited browser lifetime. Uses remain separate; each run is capped at ten minutes. Failed attempts consume a use. Configuration changes invalidate grants. Never replenish a budget, reset state or repeatedly retry passwords as a workaround.

## Challenge handling

The built-in worker pauses at `human_required`; it has no CAPTCHA-solving API. Follow the current tool's challenge rules. External AI browser assistance is possible only when the current user authorization and live tool rules permit that particular action. An old successful CAPTCHA attempt is not authorization for a new challenge.

During human credential or MFA entry, do not observe the window. If explicitly authorized to help with the visible challenge, use the identified worker window and limit observations/actions to what that step needs; do not query password field values, decrypt the store, export cookies or unlock arbitrary browser access. Resume uses the operator surface, then the worker independently verifies authentication. Do not ask the user to repeat a confirmation already provided for the current action.

## Interpret results precisely

- `completed` with `authenticated: true` means that run verified a fresh session. The worker then closes the browser. General browser tools and other chats do not inherit that session.
- Missing/empty `fields` means no inventory result. Login success alone does not complete server verification, billing review or the caller's broader task.
- `human_required` is a pause, not failure or proof of login. Human confirmation also is not authentication proof.
- Failure, expiry, cancellation or adapter mismatch requires diagnosing that specific cause. Do not infer a wrong password from a blocked route or network error.

Real CloudCone login was verified on 2026-09-30 with external AI assistance for one image CAPTCHA. The `/cloud` landing page and logout/billing markers were observed. The subsequent redirect-transport fix passed local tests but was not retried against the real account. Live provider inventory extraction remains unverified; a real manage route is not covered by the current overview selector. Consult current code/docs before assuming these limits have changed.

## Carry knowledge across chats

Share the skill/contract path, dated evidence, current limitations and the calling task's nonsecret invocation entry point. Query current grant/run metadata when necessary; do not freeze volatile IDs or remaining budgets into general instructions. Message another chat only with user authorization for that chat or the ongoing coordination workflow.

Login permission does not authorize purchases, renewal, reboot, deletion or account changes. A new task needs its own applicable authorization; shared learning is not shared authority.

For adapter maintenance, consult [the development lessons](../../docs/LOGIN-LESSONS.md). Extend only the observed required routes and test actual network redirect behavior with fake credentials; mock fulfillment alone does not prove transport enforcement.
