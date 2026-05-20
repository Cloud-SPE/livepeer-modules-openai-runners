# BROKER-CONTRACT

The HTTP contract between the capability broker and a runner. Both sides must
respect this — the broker is the client, the runner is the server.

## Request shape

The broker forwards a request to the runner with:

- **Method + path + body** — exactly what came from the gateway, after
  payment validation.
- **`Livepeer-Capability`** header — the canonical capability name the request
  is being dispatched against (informational).
- **`Livepeer-Offering`** header — the offering ID the broker selected
  (informational; used by the runner only for logging / metric labels).

The runner is free to ignore both headers. They are NOT authentication tokens.

## What the runner returns

For non-streaming requests:

- **HTTP status code** — 200 on success; standard error codes otherwise.
- **Response body** — capability-shaped JSON (OpenAI or Cohere shape per
  capability).
- **Work units** — either a body field (`usage.total_tokens` for OpenAI shape,
  or the equivalent per-capability field) or an `X-Livepeer-Work-Units`
  response header (for Go proxies).

For streaming requests (chat completions only):

- **SSE** body — pass-through from the upstream.
- **`X-Livepeer-Work-Units`** HTTP trailer — emitted by the runner after the
  final SSE frame. The broker uses this to bill streaming work.

## Health surface

| Endpoint | Behavior | Used by |
|---|---|---|
| `GET /healthz` | 200 once warm; 503 during load / fault | Broker liveness/readiness probe |
| `GET /<capability>/options` | Structured options payload | orch-coordinator discovery |
| `GET /metrics` | Prometheus exposition (opt-in) | Operator scrapers |

## Options payload shape

```json
{
  "capability": "<capability-name>",
  "served_model_name": "<resolved-model-id>",
  "extra": {
    "key_n": "value"
  },
  "features": {
    "streaming": true,
    "include_usage_required": true
  }
}
```

The broker reads `options` and merges its `extra` block into the broker's
host-config (operator-set values always win on conflict). `features` is a
flat map of booleans the broker uses to plan dispatch.

## Error contract

| Condition | Status | Body |
|---|---|---|
| Healthy + happy path | 200 | capability-shaped |
| Malformed input | 400 | `{"error": {"message": "...", "type": "invalid_request_error"}}` |
| Auth-related (should never happen — broker handles auth) | 401/403 | error shape |
| Queue full | 429 | error shape + `Retry-After` header |
| Loading or transient fault | 503 | empty or error shape |
| Internal bug | 500 | error shape (NOT a stack trace) |

## What the broker does NOT send to runners

- Customer identity / API keys / payment envelopes.
- Routing metadata (region, multi-tenant tags).
- Any Authorization header forwarded from the gateway.

The broker's job is to make the runner's life simple: just an HTTP request
the runner can answer.
