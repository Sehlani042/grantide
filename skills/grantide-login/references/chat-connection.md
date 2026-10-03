# Automatic local connection

Routine chats need no manual setup. `serve` writes nonsecret discovery metadata to `~/.config/grantide/local-service.json`: the port file, executable, and optional paired Chrome profile. `--discovery-file PATH` overrides this; an empty value disables registration for test instances. `--chrome-profile ROOT` records the existing profile and is retained across ordinary restarts. Registration contains no operator or Agent token.

The helper resolves discovery, then an optional exact legacy entry from `~/.config/grantide/codex-connections.json`, then explicit `GRANTIDE_URL` / `GRANTIDE_AGENT_TOKEN_FILE` / `GRANTIDE_TOKEN` overrides. It never searches another chat entry. New identities are cryptographically random, written atomically to mode-0600 files in `~/.config/grantide/agent-tokens/`, with filenames derived from the conversation ID. The credential is sent only in a header to the validated loopback service; proxies and redirects are disabled.

`invoke.py --check` checks service reachability and private identity storage. It does **not** claim an approved grant or website authentication. `login --url TARGET` creates the pending request. Operator approval chooses the actual saved account and starts the fixed extension adapter. `login --id ID` or `login --status` returns only owned metadata and bounded results, never credentials.

If discovery is missing, locate/start the installed Grantide service from its known project or installation location. Do not read operator.token to give a chat admin access. No requirement to manually copy a token remains. See the CLI's `serve --help` for registration options. Advanced explicit Agent configuration remains supported for external integrations.

A request survives a CLI timeout. Pending approvals survive service restart subject to their revision and 15-minute expiry; active login runs become interrupted and require review before another attempt. Completed extension results are persisted encrypted and remain retrievable. Codex must still be running a wait/poll or resume later to consume those results; this protocol does not secretly send a new message to an ended chat.
