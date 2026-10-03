---
name: grantide-login
description: Use Grantide (允界) to connect a Codex chat automatically, request operator approval and fill saved CloudCone credentials in the paired Chrome profile. Use for 允界登录, CloudCone/LA1 login, or a request to reuse the login workflow across chats. Other sites require adapter assessment or manual login handoff.
---

# Grantide login

Use `scripts/invoke.py` from this skill. It discovers the running local service and creates/stores a private identity for the calling `CODEX_THREAD_ID`. **Do not ask the user to copy tokens, create an Agent manually, paste a terminal command or register the chat.** Never borrow another chat's token, read the operator credential for agent calls, or search backups for credentials.

## Default workflow

1. Resolve the skill directory (including its symlink). With an authorized CloudCone destination, invoke:

   ```sh
   python3 /ABSOLUTE/SKILL/scripts/invoke.py login \
     --url https://app.cloudcone.com/vps/INSTANCE/manage \
     --name 'Codex · descriptive task label' --wait 30
   ```

   Use the actual user-requested instance, or `https://app.cloudcone.com/` for login only. `--account-label` is an optional account suggestion. Do not invent an instance. Keep the real `CODEX_THREAD_ID`; it selects the chat's private identity.
2. A missing grant creates one approval card. Open the returned `approval_url` using the supported browser tool. The user selects the saved account and presses **Approve and connect**. They may allow future use of this exact account/page without expiry or a use limit. If a matching grant already exists, the server starts directly. The operator password supports remembered login.
3. Approval itself creates the identity/grant and queues the Chrome extension, even if the CLI wait has ended. No second command is needed to dispatch. Record the returned `connect_...` ID and poll the same request:

   ```sh
   python3 /ABSOLUTE/SKILL/scripts/invoke.py login --id connect_ID --wait 30
   ```

   For an interrupted conversation, `login --status --wait 0` retrieves this identity's latest request. A CLI timeout leaves the request/run active; do not submit duplicate jobs while waiting. Persisted completed results can be read after a gateway restart. Pending approvals expire after 15 minutes; runs last at most 10 minutes. Server restart interrupts unfinished runs; never call that success.
4. `status: completed` and `result.authenticated: true` verify that run. `result.verified_at` dates the evidence. Fields are the fixed, authorized extraction only; missing fields are unverified. The website session stays in the paired Chrome profile identified in the helper output, not Codex's IAB. Continue the user's authorized work in that same profile.

If no account is saved, prepare `web-login --setup --label 'CloudCone · task'` or open Automatic login and suggest an editable label. The user enters credentials there, never in chat. Saving an account changes configuration and invalidates pending requests: submit a new login request afterward. Other errors should be reported with their actual cause, not replaced by token-copy instructions. See [connection details](references/chat-connection.md) only for discovery/startup issues.

## Existing sessions and challenges

Inspect the paired Chrome session before requesting another login when prior verification exists. Reuse a verified account and destination in that profile. The current extension pauses with `ACCT` for an unidentified pre-existing session; it cannot prove that a generic logged-in page belongs to the saved account. Do not repeatedly consume grants or silently sign out to work around that check.

For CAPTCHA/MFA, follow current browser-tool rules. The user prefers an AI first attempt at a visible CAPTCHA; action-time confirmation still applies when the tool requires it. Once that exact action is confirmed, do not ask again. During human credential/MFA entry, stop observing the window. Never inspect password field values, decrypt saved accounts, export cookies, or print tokens. The extension has no CAPTCHA solver and does not obtain MFA codes.

When native Chrome control fails, consult [browser recovery](references/browser-recovery.md). The observed AppleScript recovery is an alternative only when explicitly authorized in the calling chat. A different chat's UI-control permission is not transferable.

## Scope and results

- Automatic enrollment creates a website-only identity; it cannot invoke HTTP services through wildcard API rules. Existing manually configured API identities retain their explicit policies.
- Automatic filling supports CloudCone only. Other websites require a reviewed adapter; manual handoff via `browser-login` is separate. This does not implement SSH, shell, purchases, renewals, reboots or arbitrary website actions.
- Successful login is distinct from completing the user's larger infrastructure task. Verify the actual requested management page and relevant fields before claiming that task complete.
- A 60-second account cooldown and durable failed/interrupted-run review guard remain in force. Diagnose a failure; do not replenish/reset budgets or retry indefinitely.
- Configuration changes/revocation can invalidate authority. Auto-enrolling a new identity does not revoke unrelated login grants. Repeated calls reuse only an unambiguous grant with the exact same identity, account and page.
- Local conversation labels are not platform-attested identities. This is local convenience and a gateway boundary, not an OS sandbox against another process running as the same OS user.
- Do not message or wake another chat unless the user explicitly authorized messaging it. A waiting helper observes approval/results; a finished Codex turn is not automatically restarted. The approved login task itself executes independently in Grantide.

Read [the integration contract](../../docs/CONTROLLED-LOGIN.md), [extension setup](../../docs/CHROME-EXTENSION.md), and [NOW.md](../../NOW.md) only when the requested operation needs details or dated verification. Cross-chat learning is supplied by this installed skill; permission is held separately by each identity.
