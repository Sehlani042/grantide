# Release v0.3.0-alpha.1

Build: `go build -o bin/grantide ./cmd/grantide`. Safe demo: `./bin/grantide serve --demo --data-dir .local/demo`. Start an empty default-deny configuration with `serve --data-dir <private-directory>`.

Loopback only in this alpha. Keep state outside agent workspaces. Restart cancels requests/leases. Stop the process and back up its private data directory before replacing a binary. Future schema downgrades are unsupported: restore matching binary and backup. Gate: race tests, vet, GUI verification, cross-compiles, source review, GitHub push and CI. Experimental alpha; no independent security audit claimed.

This increment adds a cooperative browser login handoff protocol and GUI, alongside API credential entry and HTTP Basic. Codex integration is through the CLI and the original conversation’s browser tool; no global password interception or browser lock is installed. Existing alpha state migrates additively; new request queues are ephemeral. Real Tencent Cloud account verification is pending human login and is not claimed by this release.

## v0.4 development increment
Controlled website login adds the optional `browser-worker` Node/Playwright package; it is not included in earlier single-binary release archives. Install using the pinned lockfile and browser install command in docs/CONTROLLED-LOGIN.md. Back up private encrypted state before upgrade. The source supports one reviewed-origin adapter (CloudCone); live account validation is pending. Existing manual browser handoffs and runtime grants are lost on restart. No stable security or universal website support claim.
