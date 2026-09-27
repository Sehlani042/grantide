# HTTP API v0.1

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
