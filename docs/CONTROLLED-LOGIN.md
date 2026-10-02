# Controlled website login (experimental v0.4)

This is the automatic-credential path: **operator saves a password in Grantide → operator issues an account-scoped grant → agent invokes the grant → the trusted worker fills the website → CAPTCHA/MFA if required → the worker verifies authentication and returns a bounded result**. `browser-login` is the older manual handoff and does not meet this requirement.

## Run locally

The HTTP gateway remains a Go binary. Automatic website login additionally needs Node.js 22+ and the pinned Playwright Chromium runtime on a desktop with a display:

```sh
npm ci --prefix browser-worker --ignore-scripts
npx --prefix browser-worker playwright install chromium
go build -o bin/grantide ./cmd/grantide
./bin/grantide serve --data-dir /absolute/private/state \
  --browser-node /absolute/path/to/node \
  --browser-worker /absolute/path/to/grantide/browser-worker/entry.mjs
```

On Linux, install Playwright's documented browser system dependencies and use a desktop/display session. The worker enables the Chromium sandbox on Linux; environments without sandbox support fail rather than disable it. The Node worker is optional; existing HTTP/manual handoff features work without it.

## Operator

Open **Automatic login** (`?view=vault`). New accounts start with an editable `CloudCone` label. An agent can run `grantide web-login --setup --label "CloudCone · LA1"` and open the returned local `setup_url` to prefill a contextual, nonsecret label. Existing account names are preserved. Save the account with its email and password. Passwords are encrypted in the existing private store; snapshots return neither password nor email. Blank credentials while editing preserve previous values; delete removes the saved account from current state. Historical private backups may still contain encrypted copies.

Create a grant selecting one agent, 30 seconds–24 hours or **No expiry (revocable)**, and either 1–20 login attempts or **Unlimited uses (revocable)**. API `seconds: 0` requests no expiry; `expires_at: null` represents it. Unlimited attempts require `unlimited_uses: true` with `uses: 0`. Omitting the flag never upgrades an exhausted finite grant. Grant responses retain `remaining: 0` for unlimited grants; inspect `unlimited_uses` first. The origin is fixed to `https://app.cloudcone.com`. An optional exact `/compute/NUMBER`, `/vps/NUMBER` or `/vps/NUMBER/manage` path additionally allows a small fixed inventory-field extractor. Leave it blank to verify login only. Copy the nonsecret `login_grant_...` reference, or let the agent list its grants. Saving credentials does not start a login. Agent references and runtime grants are not passwords.

Grant expiry is a dispatch boundary and a worker deadline. A run lasts no more than ten minutes, capped by grant expiry. One browser may run at a time. Finite uses are consumed before dispatch and not refunded after network/login failure. Grants, remaining budgets and revocations are persisted in the encrypted store and survive restart. Active runs end on restart. **Any configuration mutation revokes grants and cancels active runs**; revision binding prevents old grants from reviving after a restart. Revocation/cancellation closes the worker browser; an already submitted website request cannot be undone. The service does not revoke a provider-side session remotely.

For both finite and unlimited grants, starts for the same saved account must be at least 60 seconds apart, including across different grants and service restarts. Grant listings expose `next_login_at` and `login_needs_review`. The executor sets the durable review guard before launching and clears it only after verified success. Failure, cancellation, expiry or process interruption leaves login paused. After checking the cause, the operator can click **Clear login pause** or call `POST /admin/web-accounts/ACCOUNT_ID/review-login`; this retains the cooldown and cannot run while a worker is active. Agent credentials cannot call it. An active `human_required` run uses the separate challenge-resume action. Unlimited uses do not create an automatic retry loop.

The CloudCone public login form checked on 2026-09-29 includes a CAPTCHA. The worker fills the saved credentials, then publishes `human_required`. Complete CAPTCHA/MFA and submit login in the **Chromium window opened by Grantide**, then click **Challenge finished — verify** in Grantide. The worker makes no page observations while it awaits this operator signal. This signal is not authentication proof. Do not let a separate agent/screen tool inspect the window during entry.

## Agent CLI

Only configure that agent's token, never the operator token:

```sh
grantide web-login --setup --label "CloudCone · LA1"
grantide web-login --list
grantide web-login --grant login_grant_EXAMPLE --wait 30s
grantide web-login --id login_EXAMPLE --wait 30s
grantide web-login --id login_EXAMPLE --cancel
```

- `executing`: keep the run ID; bounded polling only. CLI timeout leaves the run active.
- `human_required`: ask the user to finish the challenge in Grantide's window. Do not read any browser/screen/DOM/AX or request credentials in chat. Only the operator can resume the worker.
- `completed` and `authenticated:true`: the worker verified its fresh session, optionally read the exact approved overview, and closed the browser. `verified_at`, `origin` and `read_path` establish the observation provenance. An empty or missing `fields` value is **not** a completed inventory audit.
- `login_failed`, `adapter_mismatch`, `network_error`, `failed`, `cancelled`, `expired`: stop. Do not retry a password repeatedly. A layout or unapproved route fails closed and needs adapter review.

The agent cannot change the site, account, expiry, destination path or task when invoking a grant. The API exposes no arbitrary actions, raw page text, screenshots, JavaScript evaluation, cookies, password retrieval or browser connection. There is no automatic wakeup of a completed Codex turn; resume the owning conversation to collect results.

## Scope of CloudCone adapter

The login form and POST destination were directly inspected on the unauthenticated official site. **Real CloudCone authentication was verified on 2026-09-30**: the worker filled the saved credentials, an external AI browser tool submitted the image CAPTCHA with the user's authorization for that attempt, and the worker confirmed both logout and billing controls before returning `completed` with `authenticated: true`. The browser was then closed. This external assistance is not a CAPTCHA-solving API or autonomous CAPTCHA implementation inside Grantide. The normal `human_required` contract remains unchanged.

The observed authenticated landing page is `/cloud`, now explicitly allowed. Ordinary login, CAPTCHA handoff and response filtering use fake-browser regression tests; the transport separately uses real local HTTP servers with fake secrets to verify redirect blocking. The revised transport has not yet been retried with the real account. **Live inventory extraction remains unverified**: the exact provider manage route is now supported, but its real field layout has not been validated against the extractor. The successful login returned no inventory fields. Unsupported fields remain absent; do not fabricate expiry, region, price or auto-renew values.

The worker imports no existing Safari/Codex cookies and does not export its own session. Consequently, ordinary browser tools do not inherit the login. Further tasks must be implemented and authorized in this worker or another equally controlled adapter. The current optional inventory extractor recognizes only numeric/date/boolean values for CPU, memory, disk, bandwidth, traffic, price, next due date and auto renew in two-column tables/definition lists. It does not automate billing changes, purchase, renewal, reboot, deletion or account modification.

## Security boundary

Passwords travel over a local child-process stdin pipe, not CLI arguments or environment. Worker stderr is discarded and stdout is a validated finite event protocol. No tracing, HAR/video, screenshot API, browser debug listener or storage-state import/export is enabled. Contexts are ephemeral and closed after each run. The network adapter checks HTTPS origin, allowed route/method, login payload and a three-submission cap. Requests use Chromium's native network path. A CDP response-stage guard rejects every HTTP 3xx before Chromium follows it, including same-origin redirects; an unexpected request-stage pause is continued only for the pinned origin. A site flow that requires HTTP redirects needs adapter review. JavaScript navigation is checked as a new request. Other write endpoints and WebSockets are blocked. Secrets are omitted from exported results; only reviewed field formats pass.

This protects the **normal Agent API/model-output path**, not the whole machine. Grantide, Chromium and the target website necessarily hold plaintext briefly. A process with the same OS user's file, memory, screen or debugger access can bypass this boundary, as can a compromised trusted worker or website. A browser password input is readable to its origin's scripts. Do not claim that a password can never reach a model on this shared desktop. Run operator state and worker under a separate OS account or appropriately isolated runtime when strong separation is required. No independent security audit is claimed.

Primary references checked 2026-09-29: [Playwright isolated contexts](https://playwright.dev/docs/api/class-browser#browser-new-context), [browser launch and profile constraints](https://playwright.dev/docs/api/class-browsertype), [network routing and service workers](https://playwright.dev/docs/network), and [CloudCone official login entry](https://help.cloudcone.com/en-us/article/how-to-access-your-partner-dashboard-nzqmnb/).

Redirect behavior checked 2026-09-30 against the installed Playwright 1.63.0 routing API and Chromium CDP `Fetch` response interception. Browser request routing alone is not a guarantee that every server redirect hop is intercepted. A live retry exposed the `route.fetch` Node TLS limitation; fake-site tests and unauthenticated official-page inspection validate the replacement, while real login success after replacement remains unverified.

Subsequent live navigation still failed in both direct headful/headless Chromium, and new worker runs returned `adapter_mismatch`. Native networking is not a verified complete fix for this desktop’s connectivity. The [Chrome extension adapter](CHROME-EXTENSION.md) is an alternate transport/profile path with separate session boundaries; its real-account verification remains pending.

Update 2026-10-02: real authentication through the paired Chrome extension is now verified. The extension filled saved credentials; external AppleScript assistance handled the current image CAPTCHA with user authorization, and the agent API returned `completed`, `transport: "extension"`, `authenticated: true`. The persistent Chrome profile kept the session, and a separately authorized UI navigation opened the LA1 manage page. The grant used for this verification was login-only and returned no inventory fields. This does not establish structured inventory extraction, fix the isolated worker transport, add autonomous CAPTCHA solving, or share Chrome cookies with IAB/other chats. See the extension document for the custom user-data-directory pairing fix.
