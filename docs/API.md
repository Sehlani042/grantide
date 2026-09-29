# HTTP API v0.2

The server listens on `http://127.0.0.1:<port>`. Every endpoint except `/healthz` and static GUI assets requires `Authorization: Bearer <token>`. Operator and agent tokens are separate. JSON rejects unknown fields. No CORS access is enabled.

## Agent requests

`POST /v1/requests`

```json
{
  "service_id": "service_123",
  "method": "POST",
  "path": "/v1/deploy",
  "query": {"region": "eu"},
  "body": "{\"version\":\"v1.2.0\"}"
}
```

`body` is a string containing valid JSON, not a nested object. GET/HEAD cannot have bodies. Paths contain no query, fragment, encoding or dot segments. Service origin and authentication headers come exclusively from operator configuration.

Response: 200 for a completed attempt, 202 for pending review, 403 for policy denial. Read the `status` field: `pending`, `executing`, `completed`, `denied`, `rejected`, `expired`, `invalidated`, `cancelled` or `failed`. A completed response includes `result: {status, body, truncated}`; `result.status` is the upstream HTTP status and may be a 4xx/5xx. There are no automatic retries.

`GET /v1/requests/{id}` polls a request belonging to this agent (other agents receive 404). `POST /v1/requests/{id}/cancel` cancels a pending request. Poll at one-second intervals; requests may be pruned after completion to keep memory bounded.

## Operator endpoints

| Endpoint | Input / output |
| --- | --- |
| `GET /admin/state` | Configuration, pending/recent requests, grants and audit. Secrets and agent token hashes omitted. |
| `POST /admin/agents` | `{name, enabled}` → `{agent, token}`. Raw token returned once. |
| `PUT /admin/agents/{id}` | `{name, enabled}`. Disable to revoke identity. |
| `POST /admin/services` | `{service: Service, clear_secret: false}`. Empty ID creates; existing ID replaces. Blank secret retains old value unless `clear_secret`. |
| `POST /admin/rules` | `Rule`. Empty ID creates; existing ID replaces. |
| `DELETE /admin/rules/{id}` | Delete policy; be mindful that removing a deny may expose a broader allow. |
| `POST /admin/requests/{id}/decision` | `{decision: "once" \| "lease" \| "reject"}`. Lease decision only for a lease rule; consumes current request once. |
| `POST /admin/leases/{id}/revoke` | Revoke future use of a grant. |
| `POST /admin/demo` | `{mode: "auto" \| "approval" \| "lease" \| "deny"}`; only when started with `--demo`. Fixed sandbox requests. |

Service example:

```json
{
  "service": {
    "id": "example",
    "name": "Example API",
    "origin": "https://api.example.com",
    "enabled": true,
    "allow_private": false,
    "auth_header": "Authorization",
    "auth_prefix": "Bearer ",
    "secret": "EXAMPLE_ONLY"
  }
}
```

Rule example:

```json
{
  "name": "Temporary diagnostics",
  "agent_id": "agent_123",
  "service_id": "example",
  "methods": ["POST"],
  "path_prefix": "/v1/diagnostics",
  "mode": "lease",
  "enabled": true,
  "lease_seconds": 900,
  "max_uses": 3
}
```

`agent_id: "*"` matches all agents, but a resulting grant always belongs to the requesting agent. Optional `expires_at` is an RFC3339 timestamp. Expired matching rules deny; disabling a rule removes it from evaluation. Configuration mutations invalidate all pending approvals and leases, even when the changed item is unrelated. This conservative behavior is intentional in the alpha.

## Credential entry requests
Agent token endpoints:
- `POST /v1/credential-requests` accepts `{service_id, method, path}` only (no body/query). Requires a matching non-deny policy and configured authentication. Returns HTTP 202 with `{id, agent_id, service_id, method, path, origin, auth_type, revision, status, created_at, expires_at}`.
- `GET /v1/credential-requests/{id}` returns only that agent's request metadata.
- `POST /v1/credential-requests/{id}/cancel` cancels a pending request.

Operator endpoint: `POST /admin/credential-requests/{id}/decision` accepts `{decision: "save", username: "", secret: "..."}` or `{decision: "reject"}`. `username` is only used for Basic. Never submit credentials via the agent endpoints, chat, command arguments or logs. Responses contain metadata only. `GET /admin/state` includes `credential_requests`.

Statuses: `pending`, `fulfilled`, `rejected`, `expired`, `invalidated`, `cancelled`. Requests expire after at most five minutes, deduplicate while pending, and disappear on restart. `fulfilled` means stored, not authenticated with the upstream and not authorized to execute. Saving changes the service-wide credential and invalidates prior grants/requests. Other agents with matching service policies also use the new credential.

Service adds `auth_type: "header" | "basic"` (omission retains legacy header semantics), and `username`. Basic requires `auth_header: "Authorization"` and an empty `auth_prefix`; `secret` is the password. Browser HTML forms, cookies, MFA, SSH and sudo are not supported.

## Controlled website login (separate v0.4 worker)

See [the login contract](CONTROLLED-LOGIN.md) for credential entry and worker setup. Operator grants use `POST /admin/login-grants` with `{account_id, agent_id, read_path, seconds, uses, unlimited_uses}`. Finite grants accept 1–20 uses; unlimited grants require `unlimited_uses: true, uses: 0`. Duration is independent: `seconds: 0` means no expiry. `POST /admin/login-grants/{id}/revoke` revokes further use and cancels an active worker.

Agent routes are `GET /v1/login-grants`, `POST /v1/logins` with `{grant_id}`, `GET /v1/logins/{id}`, and `POST /v1/logins/{id}/cancel`. Grant metadata includes `unlimited_uses`, `remaining`, `next_login_at` and `login_needs_review`; zero remaining alone never indicates unlimited permission. Account-level cooldown is 60 seconds between starts and persists across grants/restarts. A failed or interrupted run leaves the account paused. Operator-only `POST /admin/web-accounts/{id}/review-login` clears that pause while retaining cooldown; it cannot be called during an active worker. `POST /admin/logins/{id}/resume` remains the distinct action for an active CAPTCHA/MFA handoff.
