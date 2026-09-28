# Browser login handoff

Grantide coordinates a human login on the **original website and browser tab**. It does not proxy website passwords, embed login forms, export cookies, control browser tools, or grant permission to buy/change anything after login. Integration is cooperative: an agent with independent browser access can bypass the pause. Enforced isolation requires a browser adapter that gates every observation/action, outside this alpha.

## Operator setup

In **Browser login**, add a trusted HTTPS entry URL and select the agent allowed to request login. Use a stable console entry URL without OAuth codes, query parameters, fragments, or account credentials. Use a separate agent identity for each independently controlled conversation. The website allowlist is separate from HTTP API policies and allows only asking for human login assistance.

## Original conversation workflow

1. In the conversation that owns the browser, identify the existing browser/profile and tab from its browser tool. Do not create a new browser context or copy authentication state to another conversation.
2. Before asking the human to enter anything, stop screenshot, DOM, accessibility, recording and network inspection of the login browser. Create a handoff, retaining the exact metadata below. Browser identifiers should include the profile/context and owning conversation, not just a recyclable tab number.
3. Show/hand off the original tab to the human using the browser tool's supported handoff action. The human enters a password, scans a QR code and handles MFA on the original website, then clicks **I finished logging in** in Grantide.
4. Poll only the handoff API while waiting. `waiting` means no browser inspection. `verification_required` means the human finished; it is not authentication proof. In Codex, the human must resume the original chat if its turn has ended. Grantide does not silently send messages to other chats or install a scheduler.
5. After human confirmation, inspect the **same** tab once. Check an account-specific logged-in control or the intended authenticated console page; a URL change alone is insufficient. Submit verification with the same metadata and the observed HTTPS origin (no path/query). If still logged out, record failure and ask for a new handoff. Purchases, legal acceptance and account changes retain their separate authorization requirements.

Example using fake metadata; replace IDs with values actually observed by the browser tool:

```sh
grantide browser-login --site cloud --browser 'Codex IAB: owning-chat-id' \
  --tab actual-tab-id --thread owning-chat-id --title 'Cloud console check'
# Retain browser_... returned above. Poll metadata only:
grantide browser-login --id browser_EXAMPLE --wait 30s
# Only after verification_required and a visible authenticated page:
grantide browser-login --id browser_EXAMPLE --verify --authenticated \
  --site cloud --browser 'Codex IAB: owning-chat-id' --tab actual-tab-id \
  --thread owning-chat-id --title 'Cloud console check' \
  --origin https://console.example.com
# Still logged out: omit --authenticated. To stop:
grantide browser-login --id browser_EXAMPLE --cancel
```

With a zero wait, exit code 0 may mean a request was merely submitted. Always inspect `status`. A positive wait timeout exits nonzero but leaves the handoff active until expiry or cancellation; never treat timeout as permission to inspect the browser. Handoffs last at most 15 minutes including verification, disappear on server restart, and invalidate on any configuration change. Rejected, cancelled, expired, invalidated or missing handoffs cannot be used to resume.

## API contract

- Operator: `POST /admin/browser-sites` with `{id?, name, login_url, agent_id, enabled}`. Updating a site invalidates existing handoffs/grants. Disabled or unmatched sites reject agent requests.
- Agent: `POST /v1/browser-handoffs` with `{site_id, browser, tab_id, thread_id?, thread_title?}`.
- Agent: `GET /v1/browser-handoffs/{id}`; only the owner may read.
- Operator: `POST /admin/browser-handoffs/{id}/decision` with `{decision: "done" | "reject"}`.
- Agent: `POST /v1/browser-handoffs/{id}/verify` with `{target: <original metadata>, origin: "https://console.example.com", authenticated: true | false}`. Verification is an agent report, not independent server-side proof. The agent cannot submit the human confirmation endpoint.
- Agent: `POST /v1/browser-handoffs/{id}/cancel`.
- Operator snapshot adds `browser_sites` and `browser_handoffs`.

State sequence: `waiting` → `verification_required` → `verified` or `login_failed`. Reject/cancel/expiry/configuration changes terminate the handoff. Eight active requests per agent, 128 retained globally; repeated requests for the same target deduplicate. Concurrent claims on the same named browser/tab are refused. Matching identifiers depend on honest adapter reporting.

## Evidence and limits

Backend tests verify state transitions, operator/agent separation, origin/context binding, concurrency, expiry, cancellation, invalidation and save failures. GUI tests and a real vendor login are recorded separately in NOW.md. No mobile keyboard or real-account success is implied by a desktop test.

References checked 2026-09-28: [Playwright authentication](https://playwright.dev/docs/auth) explains browser context isolation and the sensitivity of saved authentication state; [CSP frame-ancestors](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Content-Security-Policy/frame-ancestors) explains why sites can disallow embedding. These inform the choice to retain the original website tab rather than collect its password in a generic form.
