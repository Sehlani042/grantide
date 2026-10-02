# Chrome extension (experimental)

**Agent invokes an account grant → Grantide reserves its use and retry guard → a paired native host hands the credential to the extension once → the extension fills the fixed CloudCone form in Chrome → website CAPTCHA/MFA → fixed authentication markers and optional inventory fields are reported.**

The first version supports Chrome 120+ with a macOS/Linux native-host installer. Load the source as an unpacked Manifest V3 extension. It is not published to the Chrome Web Store; other browsers and Windows installation are not validated. Chrome and Codex's in-app browser have separate sessions.

## Install and pair

Build this checkout and restart the server with its original private data directory. The extension path does not require the Node/Chromium worker. Keep the server running.

```sh
go build -o bin/grantide ./cmd/grantide
```

1. In Chrome, open `chrome://extensions`, enable Developer mode and choose **Load unpacked**. Select this checkout's `extension` directory. Chrome requests CloudCone and native-application access. No all-sites, cookies, debugger, clipboard or broad tabs permission is declared.
2. Copy the extension ID from its card or popup.
3. Pair that exact ID with the running server's private directory and the absolute binary path:

```sh
python3 scripts/install-native-host.py \
  --extension-id YOUR_32_CHARACTER_EXTENSION_ID \
  --binary /absolute/path/to/grantide/bin/grantide \
  --data-dir /absolute/path/to/private-grantide-state
```

4. Reload the extension, then use **Check login task**. The popup reports a missing bridge. An unpacked extension moved to a different path may have a new ID and need pairing again.

The installer writes Chrome's per-user `com.grantide.login` native-host manifest and a private launcher in the selected data directory. Its allowlist contains only the supplied extension origin. To uninstall the bridge, remove that manifest and `grantide-native-host.sh`; remove the extension in Chrome. Saved Grantide accounts/grants remain.

When Chrome runs with a custom `--user-data-dir`, also pass `--chrome-user-data-dir /absolute/persistent/chrome-profile` to the installer. This is the user-data root, not its `Default` subdirectory. Chrome looks for the user-level manifest in that root's `NativeMessagingHosts` directory; registering only the normal macOS/Linux directory does not pair the custom instance. Use a persistent profile without `--disable-extensions`; temporary automation profiles may disable extension loading and do not provide durable login sessions. Chromium's [native messaging directory implementation](https://chromium.googlesource.com/chromium/src/+/refs/heads/main/chrome/common/chrome_paths.cc) was checked on 2026-10-02.

## Invoke

Use the calling agent's normal token and applicable grant. The extension receives no operator token.

```sh
grantide web-login --list
grantide web-login --grant login_grant_EXAMPLE --extension --wait 30s
grantide web-login --id login_EXAMPLE --wait 30s
```

`POST /v1/extension-logins` accepts only `grant_id`. Agent polling/cancellation use the existing `/v1/logins/ID` endpoints. `transport: "extension"` identifies this adapter. Pending/claimed runs are active and capped at ten minutes or grant expiry. Finite uses are reserved before queuing, even if Chrome is closed; the persisted account-wide 60-second cooldown and unsuccessful-run review guard apply across both adapters. Grants can be created without configuring the isolated worker.

The extension checks roughly every 30 seconds while Chrome runs; its popup button checks immediately. It prefers an active approved CloudCone tab, reuses another matching tab, or opens the pinned login origin. It exposes no arbitrary action, URL, JavaScript or screenshot command to agents.

| Badge | Meaning |
| --- | --- |
| `CAP` | Credentials filled; handle CAPTCHA/MFA and submit on CloudCone. |
| `ACCT` | Existing session account identity cannot yet be matched; sign out before using this saved-account grant. |
| `READ` | Loading the exact approved instance overview. |
| `✓` | Authentication markers verified and result reported. |
| `ERR` | Bridge/page/scope problem; no automatic password retry. |

Authentication markers do not identify an account. This version waits for a login form and fills the grant's account rather than crediting an existing session. Matching existing sessions needs a separately observed account-identity adapter. The trusted user and website remain part of the login boundary.

## Credential and session boundary

The native host reads the operator token locally and uses it only for three fixed loopback operations: poll, one-time claim and bounded completion. It never returns that token to the extension; host messages cannot choose another gateway route. A claim is operator-authenticated and serialized. Agents cannot call it. Polls, snapshots, run status and audit omit credentials. Passwords are not written to extension storage/logs; they travel through native-message pipes and the renderer to the website form.

The extension checks pinned origin, allowed paths and fixed selectors before filling. It checks logout and billing controls after the login form disappears. Inventory extraction accepts only the exact grant path and fixed numeric/date/boolean formats, validated again by Go. The gateway trusts the installed extension's report; it cannot independently inspect Chrome.

This uses an ordinary Chrome profile. It does not sandbox website traffic or stop another tool, extension, debugger or same-user process from inspecting a field/session. Chrome and CloudCone hold plaintext while filling/submitting. The isolated worker remains available for separate ephemeral sessions.

Revocation/expiry/cancellation stop future claims and actions discovered at the next poll. A claimed credential or submitted website request cannot be recalled. The extension leaves the tab and provider session in Chrome; Grantide cancellation is not website sign-out. Login grants expose no purchase, renewal, reboot, deletion or account-change actions.

CAPTCHA/MFA stays on the website. This extension contains no CAPTCHA solver or OTP retrieval API. External AI assistance follows the current browser tool's challenge rules, including action-time confirmation where required.

## Evidence

Local tests cover owner/budget isolation, one concurrent claim, operator-only HTTP access, cross-origin rejection, secret-free public metadata, cancellation/revocation/configuration invalidation/expiry, failure guards, native framing and restricted routes. Fake-page browser tests exercise DOM filling, tab reuse, challenge pause, exact overview navigation, filtered results and unidentified-session rejection.

Real CloudCone extension login was verified on 2026-10-02. Chrome displayed the enabled extension, its paired popup connected, and an agent login-only grant claimed/fill-dispatched the saved credentials. External AppleScript assistance submitted the visible image CAPTCHA with the user's current-action authorization. The agent API returned `completed`, `authenticated: true`, and `transport: "extension"`; the Chrome session remained available. A separately authorized UI navigation then opened the selected LA1 manage page. No structured inventory fields were requested/exported by this run, so the live extractor remains unverified. The initial confirmation wait expired without a login submission; its guard was reviewed before the replacement run. This adds no built-in CAPTCHA solver or automatic session identity matching.

Chrome primary references checked 2026-09-30: [Native Messaging](https://developer.chrome.com/docs/extensions/develop/concepts/native-messaging), [Scripting](https://developer.chrome.com/docs/extensions/reference/api/scripting), [Tabs/host permissions](https://developer.chrome.com/docs/extensions/reference/api/tabs), [Alarms](https://developer.chrome.com/docs/extensions/reference/api/alarms), [Permissions](https://developer.chrome.com/docs/extensions/develop/concepts/declare-permissions).
