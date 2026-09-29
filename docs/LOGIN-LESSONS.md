# Reusable login integration lessons

Date: 2026-09-30. Source: this repository and the CloudCone login development workflow. Evidence is bounded to the version and observations below.

## Product contract before implementation

The requested outcome was saved credentials used by an authorized AI task. The earlier manual browser handoff tracked a human sign-in but did not deliver that outcome. The correction was a controlled browser worker with a narrow result API. In another product, identify who performs each action and what the caller receives before choosing a handoff mechanism.

User-configurable metadata need not begin blank: the caller can suggest an editable account label. Avoid replacing an existing name or human draft. An explicit no-expiry grant can coexist with independent call budgets, revocation and per-run deadlines; do not collapse these into one duration setting.

## Carry the task through the verified boundary

Credential saving, granting, authentication, session availability and business-task completion are separate outcomes. A worker can verify login and close its private browser without giving the requesting chat a usable browser. Empty structured results do not complete a provider inventory audit. Future adapters should be designed around the caller's actual authorized task, not login alone.

Challenge handling should use the current action's authorization and the actual browser tool rules. The observed external AI-assisted image CAPTCHA was successful for one attempt; it did not add autonomous CAPTCHA support to the worker. MFA entry and unobservable secret entry retain their own boundaries. Avoid both unnecessary repeat confirmations and inferring perpetual permission from one successful attempt.

## Validate the transport, not only the route predicate

The live page navigated to `/cloud`, which was missing from the allowlist. Add the observed route rather than widening access to all paths. A separate HTTP redirect exposed that browser routing alone may miss later hops. The revised transport uses `route.fetch({maxRedirects: 0})` and rejects all 3xx responses before the browser can follow them. This also rejects same-origin HTTP redirects, a deliberate compatibility limitation requiring review when the provider flow changes.

Use real local HTTP endpoints and fake secrets to check that a second-hop destination receives no request. A mocked redirect response alone did not establish that property. Official reference: [Playwright route.fetch](https://playwright.dev/docs/api/class-route#route-fetch), checked 2026-09-30; also checked the installed Playwright 1.63.0 API notes about routing redirects.

## Evidence and remaining work

| Claim | Evidence | Limit |
| --- | --- | --- |
| Real authentication succeeded | Worker returned `completed`, `authenticated: true`; logout/billing markers observed | Historical single run, external AI-assisted CAPTCHA, browser closed afterward |
| `/cloud` route supported | Narrow policy edit and fake-browser login tests | Not a new live login after the edit |
| HTTP redirects blocked before second hop | Local HTTP tests for 301/302/303/307/308 with fake credentials | All 3xx denied; revised transport not retried with the real account |
| Core authorization checks passed | Go race tests and vet in the implementation turn | Reading this record does not rerun tests |
| Provider inventory complete | No evidence | No read scope or exported fields in the successful live run; live manage layout still needs adaptation |

## Reuse entry point

Use [grantide-login](../skills/grantide-login/SKILL.md) for operating instructions. Installing or sharing a skill lets another chat read the method; it does not automatically load the chat, share cookies, copy credentials or issue grants. Keep runtime references and private account details out of shared lessons.
