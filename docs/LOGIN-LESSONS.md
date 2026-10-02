# Reusable login integration lessons

Date: 2026-09-30. Source: this repository and the CloudCone login development workflow. Evidence is bounded to the version and observations below.

## Product contract before implementation

The requested outcome was saved credentials used by an authorized AI task. The earlier manual browser handoff tracked a human sign-in but did not deliver that outcome. The correction was a controlled browser worker with a narrow result API. In another product, identify who performs each action and what the caller receives before choosing a handoff mechanism.

User-configurable metadata need not begin blank: the caller can suggest an editable account label. Avoid replacing an existing name or human draft. An explicit no-expiry grant can coexist with independent call budgets, revocation and per-run deadlines; do not collapse these into one duration setting.

## Carry the task through the verified boundary

Credential saving, granting, authentication, session availability and business-task completion are separate outcomes. A worker can verify login and close its private browser without giving the requesting chat a usable browser. Empty structured results do not complete a provider inventory audit. Future adapters should be designed around the caller's actual authorized task, not login alone.

Challenge handling should use the current action's authorization and the actual browser tool rules. The observed external AI-assisted image CAPTCHA was successful for one attempt; it did not add autonomous CAPTCHA support to the worker. MFA entry and unobservable secret entry retain their own boundaries. Avoid both unnecessary repeat confirmations and inferring perpetual permission from one successful attempt.

## Validate the transport, not only the route predicate

The live page navigated to `/cloud`, which was missing from the allowlist. Add the observed route rather than widening access to all paths. A separate HTTP redirect exposed that browser routing alone may miss later hops. The first repair used `route.fetch({maxRedirects: 0})`, but its Node TLS path disconnected on this desktop while Chromium's native path worked. The replacement continues browser requests natively and fails all 3xx at a CDP response-stage pause, before Chromium can follow. Same-origin HTTP redirects remain rejected, a deliberate compatibility limitation requiring review when the provider flow changes.

Use real local HTTP endpoints and fake secrets to check that a second-hop destination receives no request. A mocked redirect response alone did not establish that property. Also test actual provider resource loading with fake inputs: a green local test suite did not reveal the `route.fetch` TLS incompatibility. Installed Playwright 1.63.0 API and Chromium CDP `Fetch` behavior were checked on 2026-09-30.

## Evidence and remaining work

| Claim | Evidence | Limit |
| --- | --- | --- |
| Real authentication succeeded | Worker returned `completed`, `authenticated: true`; logout/billing markers observed | Historical single run, external AI-assisted CAPTCHA, browser closed afterward |
| `/cloud` route supported | Narrow policy edit and fake-browser login tests | Not a new live login after the edit |
| HTTP redirects blocked before second hop | Local HTTP tests for 301/302/303/307/308 with fake credentials | All 3xx denied; revised transport not retried with the real account |
| Core authorization checks passed | Go race tests and vet in the implementation turn | Reading this record does not rerun tests |
| Provider inventory complete | No evidence | No read scope or exported fields in the successful live run; exact manage route now allowed, live field layout still needs verification |

## Reuse entry point

Use [grantide-login](../skills/grantide-login/SKILL.md) for operating instructions. Installing or sharing a skill lets another chat read the method; it does not automatically load the chat, share cookies, copy credentials or issue grants. Keep runtime references and private account details out of shared lessons.

## Ordinary-browser extension lessons (2026-09-30)
The extension reuses a browser network/profile path but also inherits its shared session and observation boundary. Fixed login markers do not identify the account: an existing session must not be credited to a grant’s saved account without identity evidence. Keep the administrator token in a restricted local native host, not extension storage. A one-time credential claim is dispatch; cancellation cannot recall it or sign out a provider session. Fake DOM/API tests establish adapter behavior, while Chrome installation and real login remain separate gates.

Later direct Chromium probes failed in both headful and headless modes, despite an earlier native-path public-page success. Do not freeze a transient success into a complete TLS diagnosis. The native/CDP replacement is locally tested; live connectivity remains unresolved.

## Installation and pairing in a dedicated profile (2026-10-02)

Inspect the actual browser instance before loading an extension. The visible existing Chrome instance had `--disable-extensions`; a successful file-picker interaction did not install the extension. A separate persistent Chrome user-data directory without that flag showed the enabled extension card. Multiple Chrome processes with the same application name also made native control ambiguous; explicitly authorized AppleScript could address the visible process by its numeric application-process index. Resolve that index from the current PID inventory rather than saving it across sessions.

The native-host command could pass a direct framing/poll probe while Chrome still could not find it. Chromium resolves the user-level native-host directory relative to `DIR_USER_DATA`, so a custom `--user-data-dir` needs a manifest in that root's `NativeMessagingHosts`. The installer now accepts `--chrome-user-data-dir` for this case. Chrome's connected popup verified the repair. See the [Chromium directory implementation](https://chromium.googlesource.com/chromium/src/+/refs/heads/main/chrome/common/chrome_paths.cc), checked on 2026-10-02.

The subsequent real run claimed the saved credential once and reached a visible CAPTCHA. This confirms transport/credential handoff, not authentication; the current challenge confirmation and terminal verification remain separate steps. Keep process IDs, account IDs, grant references, screenshots and browser-profile data private.

Later that day, the initial wait expired without submission. After the user's current-action response, a reviewed replacement run used the same visible challenge and returned `completed` with `authenticated: true` through the agent API. The persistent Chrome session then opened the LA1 manage page through a separately authorized UI navigation. The login-only run returned no structured inventory fields. Use this dated terminal result as authentication evidence; keep the earlier setup/challenge stages as historical intermediate states. Reuse existing scoped grants when login is required, and avoid submitting or continuing a credential operation under an expired run.
