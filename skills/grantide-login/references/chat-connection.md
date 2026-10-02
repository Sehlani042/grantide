# Calling-chat connection

On Sehlani's machine the installed skill symlinks to `/Users/sehlani/开发/grantide/skills/grantide-login`. The persistent Chrome user-data root is `/Users/sehlani/开发/grantide/.local/chrome-profile`. Keep that running session; do not launch a temporary Chrome profile instead.

## Resolve the connection

Run `scripts/invoke.py --check`. It accepts `GRANTIDE_URL` plus `GRANTIDE_TOKEN` or `GRANTIDE_AGENT_TOKEN_FILE`. Otherwise it selects only the current `CODEX_THREAD_ID` entry in `~/.config/grantide/codex-connections.json`. There is no fallback to another chat.

Private registry format:

```json
{
  "threads": {
    "CALLING_THREAD_ID": {
      "token_file": "/absolute/private/this-agent.token",
      "port_file": "/absolute/private/running-service/port",
      "chrome_user_data_dir": "/absolute/persistent/chrome-profile"
    }
  }
}
```

An explicit `url` can replace `port_file`. The port file lets the service restart on a different loopback port without changing instructions. Store only file references, never raw tokens/passwords. Keep the registry/token files private (0600, parent directory 0700) and outside Git.

## Complete a missing binding

1. Open the existing operator GUI using the configured loopback URL or explicitly identified running-service metadata. Do not start a second service with empty state or inspect private backups.
2. In Connections, prepare an Agent for the calling chat. The operator creates its token, shown once, and configures it privately for this chat. Follow current tool confirmation requirements when creating new access. Never print the token or substitute the operator token.
3. Once authorized, register the token-file reference under the actual calling `CODEX_THREAD_ID`, preserving other entries. Do not infer token ownership from a filename or silently alias another chat's connection.
4. In Automatic login, reuse the saved account and issue an applicable grant to that Agent. Do not ask for the password again. No-expiry/unlimited uses are selected only when authorized. Configuration mutations can invalidate existing grants; inspect state before changing it.
5. Run `--check` and `web-login --list`, then invoke the matching grant. If operator entry/selection is necessary, show that exact GUI step and ask for only the missing input. Do not leave the user to reconstruct a CLI tutorial.

This registry is connection convenience, not authority or an OS sandbox. Grantide checks the token/grant owner. Cross-chat operating knowledge does not transfer another Agent's grant.
