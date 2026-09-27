# Security model

Grantide is an experimental explicit HTTP gateway. Its trusted computing base is the operator, OS account, data directory, server binary and configured upstreams. It does not enforce OS/network isolation of the calling agent. Run untrusted agents under separate filesystem/network restrictions. Do not give an agent the operator token, state directory, master key, Docker socket or access to the operator browser profile.

## What is enforced

- Separate operator and agent bearer tokens. Agent tokens are random 256-bit values; only their SHA-256 hashes are persisted. Disabled agents cannot authenticate. Only operator routes configure policies and approve requests.
- Default deny, explicit deny precedence, canonical path/method matching, immutable pending request bodies, one-time decisions, configuration invalidation and atomic lease budgets.
- Operator-configured origins only, no agent-supplied host/header override, no redirects, no ambient proxy, standard TLS verification. DNS results are checked at connection time and the validated address is dialed directly. Private/reserved destinations are blocked unless the operator explicitly opts that service into private access.
- Loopback server, expected Host and Origin checks, strict CSP, no third-party browser scripts, no credential values in snapshots or audit records. Upstream response bodies are displayed as text.
- AES-256-GCM authenticated state encryption, private file permissions, atomic state replacement and single-process lock. Failure to persist authorization audit blocks dispatch.
- Bounded JSON inputs, per-agent pending/executing limits, bounded request/result memory, capped responses and finite upstream timeouts.

## What requires care

Encryption cannot protect state if the attacker can read both it and the key. Operator login links contain a bearer token in their URL fragment; they must remain private. Browser session storage remains accessible to scripts in the same origin and browser-profile users. Do not expose the console through a public reverse proxy without designing TLS/authentication and a deployment boundary.

Policies match paths and methods, not business semantics, body/query predicates or GraphQL operation types. A temporary grant permits different request bodies within its scope. The approval GUI shows the initial request and full grant scope. Use one-time approval for sensitive payloads; never assume GET means harmless.

Configured upstreams are trusted with injected credentials. Literal credential echoes are redacted from responses, but transformed echoes or a malicious upstream are not generally preventable by this broker. Private-network opt-in intentionally permits the configured private destination; review it carefully.

Authorization is reserved while holding the engine lock immediately before dispatch. Revoking afterward does not cancel an operation already admitted. Timeouts/disconnections may leave uncertainty about whether an upstream applied a request. There are no automatic retries and lease usage is not refunded on failure. Completed requests are not replayed.

Pending approvals and grants are memory-only and disappear on restart. The audit is a bounded local record (last 500 metadata events), not an append-only compliance ledger. Completed request bodies/results are pruned when the in-memory capacity reaches 256; do not rely on indefinite polling. A crash may leave `server.lock`; verify the prior process is stopped before removing it. Back up the whole private data directory while the server is stopped. Lost keys cannot be recovered.

Current interface limits: canonical ASCII paths without percent encoding, separate query map with single values, JSON body up to 64 KiB, response up to 64 KiB, max 16 outstanding requests per agent, 256 total retained requests, 100 agents/services, 500 rules and 256 retained grants. Upstream requests time out after 20 seconds. No streaming or WebSocket support.

Report vulnerabilities through the repository's private security advisory feature when enabled. Do not post actual credentials in issues. No independent security audit is claimed for v0.1.
