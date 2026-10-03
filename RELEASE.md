# Release v0.3.0-alpha.1

Build: `go build -o bin/grantide ./cmd/grantide`. Safe demo: `./bin/grantide serve --demo --data-dir .local/demo`. Start an empty default-deny configuration with `serve --data-dir <private-directory>`.

Loopback only in this alpha. Keep state outside agent workspaces. Restart cancels requests/leases. Stop the process and back up its private data directory before replacing a binary. Future schema downgrades are unsupported: restore matching binary and backup. Gate: race tests, vet, GUI verification, cross-compiles, source review, GitHub push and CI. Experimental alpha; no independent security audit claimed.

This increment adds a cooperative browser login handoff protocol and GUI, alongside API credential entry and HTTP Basic. Codex integration is through the CLI and the original conversation’s browser tool; no global password interception or browser lock is installed. Existing alpha state migrates additively; new request queues are ephemeral. Real Tencent Cloud account verification is pending human login and is not claimed by this release.

## v0.4 development increment
Controlled website login adds the optional `browser-worker` Node/Playwright package; it is not included in earlier single-binary release archives. Install using the pinned lockfile and browser install command in docs/CONTROLLED-LOGIN.md. Back up private encrypted state before upgrade. The source supports one reviewed-origin adapter (CloudCone); one real login was verified, while live inventory extraction and the later redirect transport still need real-account validation. Existing manual browser handoffs and active browser runs end on restart. Login grants, remaining budgets and revocations now persist in encrypted state, including no-expiry grants. No stable security or universal website support claim.

Unlimited-use grants require the new explicit boolean and remain scoped/revocable. Before upgrade, back up encrypted state. Accounts now persist retry guards and a 60-second start interval; a failed/interrupted login requires operator review even after restart. Do not downgrade to a binary that ignores these guards.

## Chrome extension development increment
Source includes an unpacked Chrome 120+ Manifest V3 adapter and a macOS/Linux native-host installer. It requires an updated Grantide binary; native-host pairing adds persistent Chrome access to CloudCone and the local bridge. Keep the extension source/binary paths stable and pair the exact ID. Back up the private directory before service upgrade. No Chrome Web Store publication, Windows installer, other-browser compatibility, or real extension login is claimed. See docs/CHROME-EXTENSION.md.

## Operator password/session development increment

Back up the private data directory and matching binary before upgrade. Additive `operator-auth.json` contains a salted password hash and hashed session records; it does not replace `operator.token`, encrypted provider state or Agent credentials. The local stdin password CLI runs with the server stopped. Downgrading loses cookie-login support; old native bearer integrations continue. Remembered cookies last 30 days; explicit logout revokes the browser session.

## Automatic local chat connection development increment

Back up encrypted state/binary before upgrade. Connection requests and completed extension metadata are additive encrypted state fields; new identities are website-only. Do not downgrade to a version that ignores that identity restriction. Pending approvals retain revision/expiry, active runs do not resume after service restart. `serve` now publishes nonsecret local discovery by default; use `--discovery-file ''` for isolated test services. The distributed archives include the Python 3 Skill/helper. No token copying is required for its CloudCone workflow. Per-instance cookie names prevent loopback test services from replacing remembered production console sessions. This release does not wake ended Codex conversations or add SSH/universal browser control.
