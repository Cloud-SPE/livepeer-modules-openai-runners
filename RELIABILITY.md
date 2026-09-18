# RELIABILITY

Operational invariants every runner upholds. These are mechanical contracts
the broker depends on.

## Lifecycle

1. **Container start** — process boots; Python imports; logging configured.
2. **GPU probe** (ML runners only) — if `DEVICE=cuda`, verify CUDA visible.
   On failure, **exit non-zero** before model load. Operators must set
   `DEVICE=cpu` to fall back.
3. **Model load** — pull from `MODEL_DIR` (or HF cache on first run) into
   GPU/CPU memory.
4. **Listener up** — bind `:${RUNNER_PORT}` (default `8080`).
5. **Healthy** — `GET /healthz` returns 200. Before this, returns 503.

## HTTP endpoints (universal)

| Path | Behavior |
|---|---|
| `POST <capability-path>` | The work. Returns OpenAI/Cohere-shaped JSON. |
| `GET /healthz` | 200 once warm; 503 during load or after critical fault. |
| `GET /.well-known/livepeer-runner` | The runner contract: what this runner is and how to count its work. Read once per attach by the pool member agent. |
| `GET /v1/models` | Go proxies only: OpenAI list of discovered models, backing the readiness probe. |
| `GET /metrics` | Prometheus exposition. **Opt-in via `METRICS_ENABLED=true`.** |

Per-runner details in [`RUNNERS.md`](./RUNNERS.md); cross-cutting shape rules
in [`RUNNER-INVARIANTS.md`](./RUNNER-INVARIANTS.md).

## Failure modes

- **GPU absent with `DEVICE=cuda`** — exit non-zero at startup. Loud failure.
- **GPU architecture unsupported by the torch build** (e.g. a GTX 1080 on the
  default cu128 image) — exit non-zero at startup naming the `-pascal`
  flavor, instead of dying on the first kernel launch.
- **Model load failure** — log structured error; exit non-zero. Container
  restarts under orchestrator; broker's healthcheck flaps to unhealthy.
- **Request queue full** — return 429 with `Retry-After`. Threshold is
  `MAX_QUEUE_SIZE`.
- **OOM during inference** — return 503; rely on orchestrator to recycle the
  container. (Do not catch + continue — torch memory state is unrecoverable.)
- **Malformed input** — return 400 with a structured error body.
- **Upstream down** (Go proxies only) — return 503; `MODEL_DISCOVERY_RETRIES`
  controls the startup retry window.

## Work-unit reporting (billing-adjacent, but the broker handles billing)

| Runner | Declared extractor | Runner-side signal |
|---|---|---|
| `openai-chat-runner` | `openai-usage` (body `usage`, final SSE frame on stream) | `X-Livepeer-Work-Units` trailer on stream (informational) |
| `openai-embeddings-runner` | `openai-usage` | `X-Livepeer-Work-Units` header (informational) |
| `openai-audio-runner` | `response-header` on `X-Livepeer-Work-Units` | header = ceil(audio seconds) |
| `rerank-runner` | `response-header` on `X-Livepeer-Work-Units` | header = documents scored |
| `openai-tts-runner` | `request-formula` (code points of `input`) | none; the broker counts the request |
| `openai-image-generation-runner` | `request-formula` (`n`, default 1) | none; the broker counts the request |

The runner declares the extractor in its contract; the broker runs it. See
[`BROKER-CONTRACT.md`](./BROKER-CONTRACT.md) §4.

## Metrics surface (when `METRICS_ENABLED=true`)

Each runner exposes Prometheus exposition at `/metrics`. Baseline counters:

- `runner_requests_total{capability,status}`
- `runner_request_duration_seconds{capability}`
- `runner_queue_depth`
- `runner_model_loaded`

Custom per-runner metrics (e.g., `whisper_chunks_processed_total`) extend the
baseline as needed.
