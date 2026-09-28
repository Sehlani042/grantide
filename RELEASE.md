# Release v0.2.0-alpha.1

Build: `go build -o bin/grantide ./cmd/grantide`. Safe demo: `./bin/grantide serve --demo --data-dir .local/demo`. Start an empty default-deny configuration with `serve --data-dir <private-directory>`.

Loopback only in this alpha. Keep state outside agent workspaces. Restart cancels requests/leases. Stop the process and back up its private data directory before replacing a binary. Future schema downgrades are unsupported: restore matching binary and backup. Gate: race tests, vet, GUI verification, cross-compiles, source review, GitHub push and CI. Experimental alpha; no independent security audit claimed.

This increment adds local credential entry requests and HTTP Basic support. Codex integration is through the CLI; no global password interception or browser-login adapter is installed. Existing alpha state migrates additively; new request queues are ephemeral.
