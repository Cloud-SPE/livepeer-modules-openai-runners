# BROKER-CONTRACT

The contract between a runner image and the network, from the runner's side.
Since v2.0.0 the broker never dials a runner: a runner **declares itself** with
one JSON document, the pool member agent relays that document once per attach,
and the broker validates it, matches it to a catalog template, and freezes the
offer from it. Every paid request then arrives through the agent's tunnel.

The normative text lives in the `livepeer-network-modules` repo:

- `livepeer-network-protocol/protocols/runner-contract.md` (1.1.0) — the
  endpoint and body a runner serves.
- `livepeer-network-protocol/protocols/runner-attach.md` §3.2 — every
  capability-entry field, its type, and what the broker validates.
- `livepeer-network-protocol/protocols/paid-job.md` — transports, work-unit
  claim, response framing.

Where this file and those disagree, they win. This file exists so a runner
author in this repo can get it right without leaving.

## 1. The contract endpoint

Every image serves:

```
GET /.well-known/livepeer-runner
```

on the same port as its capability, answering `200` with
`Content-Type: application/json`. The path is fixed on both sides. The body is
either **one capability entry** (an object) or, for a container that serves
more than one capability or identity, a **JSON array of entries**. Each array
element is validated on its own; two entries must not share a `capability_id`
and `identity`.

The `GET /<capability>/options` route is gone on every image. Nothing reads
it. There is no compatibility path: a runner that does not serve the contract
is not sold.

### What the agent does with it

The pool member agent (one per host) fetches the contract **once per attach**
— at start, and again on every re-attach the desired-state loop triggers. It
is not polled. The fetch is one `GET` with a 5-second timeout, requires HTTP
200, and caps the body at 128 KiB. The agent then:

1. adds `local_id` (the compose service name), `devices[]` (the GPU UUIDs
   pinned to this service), and `draining` (from desired state);
2. relays the result verbatim as one `capabilities[]` entry of its attach
   document. For an array body, the first entry attaches under the container's
   `local_id` and the rest under `<local_id>.<n>`, all routed to the container.

A runner whose contract cannot be fetched or does not validate is **omitted
and named** in the agent's log. The host's other runners still serve. That log
line is the inventory of non-adhering runners.

### What is validated where

The agent checks only that the body could be relayed: `capability_id`,
`protocol`, `paths`, `work_unit.name`, and `readiness.type` present. The
broker validates everything else and names the field it rejects: extractor
type, readiness type, `schema_versions`, identity-key grammar, path rules,
`transports` values.

**Any key that is not in runner-attach §3.2 and does not start with `x-`
rejects the whole entry.** The body must not carry `local_id`, `devices`, or
`draining` — those are the agent's.

## 2. The body

The runner-owned half of a capability entry. Field by field:

| Field | Required | What this repo puts there |
|---|---|---|
| `capability_id` | ✔ | The colon-form id from [`CANONICAL-CAPABILITIES.md`](./CANONICAL-CAPABILITIES.md), overridable via `CAPABILITY_NAME`. |
| `protocol` | ✔ | `paid-job/v1` for every runner here. |
| `transports` | ✔ | Subset of `unary`, `stream`, `multipart`. Chat declares `unary` + `stream`; audio declares `multipart`; everything else `unary`. |
| `work_unit.name` | ✔ | The metering dimension: `tokens`, `audio_seconds`, `input_chars`, `images`, `documents`. Frozen into the offer. |
| `work_unit.extractor` | ✔ | `{ "type": ..., ...params }` naming a broker extractor. See §4. Frozen. |
| `paths.invoke` | ✔ | The runner's own endpoint path (`/v1/chat/completions`, ...). |
| `readiness` | ✔ | `{ "type", "path", "config"? }`. Go proxies use `http-openai-model-ready` against their own `/v1/models`; Python runners use `http-status` against `/healthz`. |
| `identity` | ✔ | Flat string map. `openai.model` for OpenAI-shaped capabilities, plain `model` for rerank, plus `provider`. This is what the catalog matches on. See §5. |
| `schema_versions` | ✔ | `{ "paid-job/v1": "1.0.15" }`. Only the major is compared. |
| `x-*` | opt | Anything else the runner advertises (backend model id, context length, quantization, voices, formats). Relayed verbatim to operator surfaces; never interpreted; reaches the manifest only if the offer lists the key in `extra_from_runner`. |

`requirements` (`gpu_vram_min_bytes`, `gpu_models[]`) is optional and not
emitted today. Never send price, capacity, offering ids, or certification
steps as core fields.

### Two things to know before copying a packet

- The `response-header` extractor's config key is **`header`**, not `name`.
  The migration packets for sibling runner families wrote `name`; the broker
  rejects that at config load with `extractor_config_invalid`.
- The array body form needs `pool-member-agent` at commit `3686b67` or later
  (runner-contract 1.1.0). An older agent fails the fetch with "contract is
  not valid" and omits the runner.

## 3. How requests arrive

Every dispatched request — paid work, readiness probes, certification traffic —
reaches the runner over the agent's tunnel carrying:

- `Livepeer-Runner-Local-Id` — the entry's `local_id`. The agent's routing
  key; the runner may ignore it.
- `Livepeer-Capability` and `Livepeer-Offering` — informational, as before.
- `Livepeer` — base64 JSON; when it carries `timeout_seconds > 0` the Go
  proxies bound the upstream call by it.

The runner is free to ignore all of these. None is an authentication token.
Customer identity, API keys, and payment envelopes never reach the runner
(see [`TRUST-MODEL.md`](./TRUST-MODEL.md)).

## 4. How work is counted

The runner declares the extractor; the broker runs it against the request and
response it relayed; the broker — not the runner — emits `Livepeer-Work-Units`
to the gateway. A runner must never set a `Livepeer-*` header: the broker
strips any it finds. The runner-side header, where one exists, is
`X-Livepeer-Work-Units`.

| Runner | `work_unit.name` | Extractor | Where the number comes from |
|---|---|---|---|
| `openai-chat-runner` | `tokens` | `openai-usage`, `field` = `USAGE_FIELD` (default `total_tokens`) | The response body's `usage` block — the final SSE `usage` frame on `stream` (the runner injects `stream_options.include_usage`), the JSON body on `unary`. |
| `openai-embeddings-runner` | `tokens` | `openai-usage`, `field` = `USAGE_FIELD` | Response body `usage`. |
| `openai-audio-runner` (both entries) | `audio_seconds` | `response-header`, `header: X-Livepeer-Work-Units` | The runner sets the header to `ceil(decoded duration in seconds)` on every response format. |
| `openai-tts-runner` | `input_chars` | `request-formula`, `expression: "chars"`, `text_fields: { chars: "$.input" }`, `default: 0` | The broker counts Unicode code points of the request's `input`. The runner does not count. |
| `openai-image-generation-runner` | `images` | `request-formula`, `expression: "n"`, `fields: { n: "$.n" }`, `default: 1` | The request's `n`. The runner does not count. |
| `rerank-runner` | `documents` | `response-header`, `header: X-Livepeer-Work-Units` | The runner sets the header to `len(documents)` on every successful `/v1/rerank` response. |

The chat runner's `X-Livepeer-Work-Units` trailer (on `stream`) and header (on
`unary`, only for models listed in `OUTPUT_TOKEN_WEIGHT`) and the embeddings
runner's header are **informational**: the declared extractor reads the body,
not those headers. In particular `OUTPUT_TOKEN_WEIGHT` does not change what
the broker bills today. If weighted output-token billing is ever wanted, it is
a broker-side option on `openai-usage`, not a runner-side count.

Certification runs the declared extractor over the template's smoke request
(`usage` step, `min_units`). A runner that extracts zero fails certification
and its offer never advertises — loudly, naming the step.

## 5. Identity

`identity` is a flat map of string → string. The catalog's templates match on
it with **exact, case-sensitive string equality**; a runner whose identity
matches no template attaches fine, sits unmatched, and is never sold. There is
no error for that. The values the catalog matches today:

| Runner | Key | Value |
|---|---|---|
| chat | `openai.model` | `SERVED_MODEL_NAME`, else each discovered model (e.g. `Qwen3.6-27B`) |
| embeddings | `openai.model` | `SERVED_MODEL_NAME`, else the discovered model (e.g. `Qwen3-Embedding-8B`) |
| audio | `openai.model` | `whisper-large-v3` (`MODEL_ALIAS`) |
| tts | `openai.model` | `kokoro` (`MODEL_ALIAS`) |
| image-generation | `openai.model` | `MODEL_ID` as-is (e.g. `black-forest-labs/FLUX.1-dev`) |
| rerank | `model` | `zerank-2` (`MODEL_ALIAS`) |

`identity.openai.model` is what a caller puts in `"model"`; the HuggingFace
path goes in `x-backend-model`. Rerank is not an OpenAI endpoint, so it takes
the plain `model` key. `provider` names the backend (`vllm`, `ollama`,
`openai`, `dashscope`, `transformers`, `kokoro`, `diffusers`,
`sentence-transformers`) and is not matched on.

The chat runner returns one entry per identity: with `SERVED_MODEL_NAME` set,
exactly one; otherwise an array with one entry per discovered model (after
`MODEL_ALLOWLIST`). Until model discovery completes, both `/v1/models` and the
contract endpoint answer `503`, and the agent's next re-attach picks the
runner up.

## 6. Readiness

`readiness` is a remote probe the broker runs before certification. Two types
are used here:

- `http-openai-model-ready`, `path: /v1/models`, `config.model: <identity
  model>` — passes when the path returns 200 and the body contains the model
  string. Both Go proxies now serve `GET /v1/models` in the OpenAI list shape
  (the discovered, allowlist-filtered models) for exactly this.
- `http-status`, `path: /healthz` — passes on any 2xx/3xx. The Python runners
  load their model in the FastAPI lifespan before binding, so `/healthz`
  answering at all means the model is loaded.

Probe cadence (`attempts`, `interval_ms`) belongs to the template author, not
the runner.

## 7. Response framing

The broker relays a runner's reply as a complete unit and length-delimits it
itself, so a runner **need not set `Content-Length`** on `unary` or
`multipart` responses. A runner that sets `Transfer-Encoding: chunked` keeps
it.

On `stream`, the chat runner's body must stay **length-unknown and chunked**
with `Trailer: X-Livepeer-Work-Units` advertised in the response headers, so
the trailer can follow the last SSE frame. A `Content-Length`-delimited
response drops trailers silently. The broker in turn advertises and emits its
own `Livepeer-Work-Units` trailer to the gateway.

## 8. Error contract

Unchanged. A runner answers with capability-shaped bodies and standard
statuses:

| Condition | Status | Body |
|---|---|---|
| Healthy + happy path | 200 | capability-shaped |
| Malformed input | 400 | `{"error": {"message": "...", "type": "invalid_request_error"}}` |
| Auth-related (should never happen — the broker handles auth) | 401/403 | error shape |
| Queue full | 429 | error shape + `Retry-After` header |
| Loading or transient fault / upstream down | 503 | empty or error shape |
| Out of memory | 507 | error shape |
| Internal bug | 500 | error shape (NOT a stack trace) |

The chat runner additionally marks refundable upstream failures (401, 403,
429, 5xx, transport errors) with `X-Livepeer-Runner-Error: true`.

## 9. Example contracts

What each image serves at `GET /.well-known/livepeer-runner` with default
configuration. Values in angle brackets come from the environment.

### openai-chat-runner

```json
{
  "capability_id": "openai:chat-completions",
  "protocol": "paid-job/v1",
  "transports": ["unary", "stream"],
  "work_unit": { "name": "tokens",
                 "extractor": { "type": "openai-usage", "field": "total_tokens" } },
  "paths": { "invoke": "/v1/chat/completions" },
  "readiness": { "type": "http-openai-model-ready", "path": "/v1/models",
                 "config": { "model": "<served model>" } },
  "identity": { "openai.model": "<served model>", "provider": "vllm" },
  "schema_versions": { "paid-job/v1": "1.0.15" },
  "x-backend-model": "<BACKEND_MODEL>",
  "x-context-length": 32768,
  "x-quantization": "<QUANTIZATION>",
  "x-reasoning-parser": "<REASONING_PARSER>",
  "x-tool-call-parser": "<TOOL_CALL_PARSER>"
}
```

The `x-*` keys appear only when the corresponding env var is set. With
`SERVED_MODEL_NAME` unset and several models discovered, the body is an array
of such objects differing only in `identity.openai.model` and
`readiness.config.model`.

### openai-embeddings-runner

```json
{
  "capability_id": "openai:embeddings",
  "protocol": "paid-job/v1",
  "transports": ["unary"],
  "work_unit": { "name": "tokens",
                 "extractor": { "type": "openai-usage", "field": "total_tokens" } },
  "paths": { "invoke": "/v1/embeddings" },
  "readiness": { "type": "http-openai-model-ready", "path": "/v1/models",
                 "config": { "model": "<served model>" } },
  "identity": { "openai.model": "<served model>", "provider": "vllm" },
  "schema_versions": { "paid-job/v1": "1.0.15" },
  "x-backend-model": "<BACKEND_MODEL>",
  "x-embedding-dimensions": 1024,
  "x-max-input-tokens": 512,
  "x-pooling-mode": "<POOLING_MODE>"
}
```

### openai-audio-runner

Two entries, one array, one port. With `CAPABILITY_NAME` set to one of the two
ids, only that entry is returned (as a bare object).

```json
[
  {
    "capability_id": "openai:audio-transcriptions",
    "protocol": "paid-job/v1",
    "transports": ["multipart"],
    "work_unit": { "name": "audio_seconds",
                   "extractor": { "type": "response-header", "header": "X-Livepeer-Work-Units" } },
    "paths": { "invoke": "/v1/audio/transcriptions" },
    "readiness": { "type": "http-status", "path": "/healthz" },
    "identity": { "openai.model": "whisper-large-v3", "provider": "transformers" },
    "schema_versions": { "paid-job/v1": "1.0.15" },
    "x-backend-model": "openai/whisper-large-v3",
    "x-formats": { "input": ["mp3", "wav", "m4a", "flac"],
                   "output": ["json", "srt", "text", "verbose_json", "vtt"] }
  },
  {
    "capability_id": "openai:audio-translations",
    "protocol": "paid-job/v1",
    "transports": ["multipart"],
    "work_unit": { "name": "audio_seconds",
                   "extractor": { "type": "response-header", "header": "X-Livepeer-Work-Units" } },
    "paths": { "invoke": "/v1/audio/translations" },
    "readiness": { "type": "http-status", "path": "/healthz" },
    "identity": { "openai.model": "whisper-large-v3", "provider": "transformers" },
    "schema_versions": { "paid-job/v1": "1.0.15" },
    "x-backend-model": "openai/whisper-large-v3",
    "x-formats": { "input": ["mp3", "wav", "m4a", "flac"],
                   "output": ["json", "srt", "text", "verbose_json", "vtt"] }
  }
]
```

### openai-tts-runner

```json
{
  "capability_id": "openai:audio-speech",
  "protocol": "paid-job/v1",
  "transports": ["unary"],
  "work_unit": { "name": "input_chars",
                 "extractor": { "type": "request-formula", "expression": "chars",
                                "text_fields": { "chars": "$.input" }, "default": 0 } },
  "paths": { "invoke": "/v1/audio/speech" },
  "readiness": { "type": "http-status", "path": "/healthz" },
  "identity": { "openai.model": "kokoro", "provider": "kokoro" },
  "schema_versions": { "paid-job/v1": "1.0.15" },
  "x-backend-model": "hexgrad/Kokoro-82M",
  "x-formats": { "output": ["aac", "flac", "mp3", "opus", "pcm", "wav"] },
  "x-default-voice": "af_bella",
  "x-voices": { "native": ["af_bella", "am_michael", "..."],
                "aliases": { "alloy": "af_bella", "echo": "am_michael", "...": "..." } }
}
```

### openai-image-generation-runner

```json
{
  "capability_id": "openai:images-generations",
  "protocol": "paid-job/v1",
  "transports": ["unary"],
  "work_unit": { "name": "images",
                 "extractor": { "type": "request-formula", "expression": "n",
                                "fields": { "n": "$.n" }, "default": 1 } },
  "paths": { "invoke": "/v1/images/generations" },
  "readiness": { "type": "http-status", "path": "/healthz" },
  "identity": { "openai.model": "black-forest-labs/FLUX.1-dev", "provider": "diffusers" },
  "schema_versions": { "paid-job/v1": "1.0.15" },
  "x-default-size": "1024x1024",
  "x-formats": { "output": ["b64_json"] }
}
```

### rerank-runner

```json
{
  "capability_id": "text:rerank",
  "protocol": "paid-job/v1",
  "transports": ["unary"],
  "work_unit": { "name": "documents",
                 "extractor": { "type": "response-header", "header": "X-Livepeer-Work-Units" } },
  "paths": { "invoke": "/v1/rerank" },
  "readiness": { "type": "http-status", "path": "/healthz" },
  "identity": { "model": "zerank-2", "provider": "sentence-transformers" },
  "schema_versions": { "paid-job/v1": "1.0.15" },
  "x-backend-model": "zeroentropy/zerank-2",
  "x-max-documents": 1000
}
```

## 10. What the broker does NOT send to runners

Unchanged from v1:

- Customer identity / API keys / payment envelopes.
- Routing metadata (region, multi-tenant tags).
- Any Authorization header forwarded from the gateway.

The runner's job is to answer HTTP and to say, once, what it is.
