# RUNNER-INVARIANTS

The HTTP surface every runner image upholds, regardless of language or model.
These are the rules a new runner author must follow.

## Required endpoints

| Method | Path | Status |
|---|---|---|
| `POST` | `<capability-path>` | 200 + capability-shaped body on success |
| `GET` | `/healthz` | 200 once warm; 503 during load / critical fault |
| `GET` | `/.well-known/livepeer-runner` | 200 + `application/json`: the runner contract — one capability entry, or an array of entries for a container that serves several. See [`BROKER-CONTRACT.md`](./BROKER-CONTRACT.md). |

The contract body carries only the runner-owned fields of runner-attach §3.2
plus `x-*` extensions. Any other key rejects the entry; `local_id`, `devices`
and `draining` are the agent's and must be absent.

## Conditional endpoints

| Method | Path | When |
|---|---|---|
| `GET` | `/v1/models` | Go proxies (chat, embeddings): OpenAI list shape of the discovered, allowlist-filtered models. Backs the `http-openai-model-ready` readiness probe the contract declares. 503 until discovery completes. |
| `GET` | `/metrics` | When `METRICS_ENABLED=true` |

## Required environment variables

Every image MUST accept (and honor) these. ML runners add per-capability vars
on top — see [`infra/env/`](./infra/env/) for templates.

| Var | Default | Purpose |
|---|---|---|
| `CAPABILITY_NAME` | (per image) | The `capability_id` the contract declares. See [`CANONICAL-CAPABILITIES.md`](./CANONICAL-CAPABILITIES.md). Audio: unset serves both entries. |
| `MODEL_ALIAS` | (per image; Python runners) | The `identity` value the catalog matches on (`whisper-large-v3`, `kokoro`, `zerank-2`). Go proxies use `SERVED_MODEL_NAME`. |
| `DEVICE` | `cuda` (ML) | torch device. ML runners exit non-zero on `cuda` + no GPU, and on a GPU whose compute capability the torch build has no kernels for. |
| `METRICS_ENABLED` | `false` | Opt-in `/metrics` exposition. |
| `RUNNER_PORT` | `8080` | HTTP bind port. |

## Work-unit signal

The contract declares the extractor; the broker counts. Where the runner
itself carries the number, the header is `X-Livepeer-Work-Units` — never
`Livepeer-Work-Units`, which is the broker's own header to the gateway and is
stripped from anything a runner sends. On a streamed response the value is an
HTTP trailer on a chunked, length-unknown body with `Trailer` advertised.

## HEALTHCHECK in the Dockerfile

Every image MUST declare a `HEALTHCHECK` that probes `GET /healthz`. The
canonical form:

```dockerfile
HEALTHCHECK --interval=30s --timeout=10s --retries=3 \
    CMD <python|curl> ... http://localhost:8080/healthz || exit 1
```

## EXPOSE 8080

Every image MUST `EXPOSE 8080`. Operators remap externally; the image always
binds internally to 8080.

## Logging

Structured logs only (via `structlog` for Python, `slog` for Go). One log
line per request at INFO. Errors at ERROR with structured fields, NOT
backtraces in the message.

## Image labels

Every image SHOULD set:

```dockerfile
LABEL org.opencontainers.image.source="https://github.com/Cloud-SPE/livepeer-modules-openai-runners"
LABEL org.opencontainers.image.licenses="MIT"
```

(Not enforced today; tracked in [`PLANS.md`](./PLANS.md) as a mechanical lint
to add once the build is stable.)

## Tests

`./build-images.sh test` runs every Go module's `go vet` + `go test` and every
Python runner's `unittest` (the contract and GPU-probe modules) inside Docker.
The build workflow gates on it. A change to a contract shape without a test
change is a red flag.

## What a non-conforming runner breaks

- **Attach** if `/.well-known/livepeer-runner` is missing, not 200, larger
  than 128 KiB, or carries an unknown key — the pool member agent omits the
  runner and names it in its log. The host's other runners still serve.
- **Matching** if `capability_id` or `identity` disagree with the catalog —
  the runner attaches, sits unmatched, and is never sold. No error is raised.
- **Certification** if the declared extractor yields zero on the template's
  smoke request — the offer never advertises, loudly, naming the step.
- **Readiness** if `/healthz` (or `/v1/models` for the Go proxies) is missing
  or returns the wrong status during load.
- **Operator observability** if `/metrics` doesn't follow Prometheus exposition
  format.
