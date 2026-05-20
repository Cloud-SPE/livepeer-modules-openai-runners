# RUNNER-INVARIANTS

The HTTP surface every runner image upholds, regardless of language or model.
These are the rules a new runner author must follow.

## Required endpoints

| Method | Path | Status |
|---|---|---|
| `POST` | `<capability-path>` | 200 + capability-shaped body on success |
| `GET` | `/healthz` | 200 once warm; 503 during load / critical fault |
| `GET` | `/<capability>/options` | 200 + structured options payload |

## Conditional endpoints

| Method | Path | When |
|---|---|---|
| `GET` | `/metrics` | When `METRICS_ENABLED=true` |

## Required environment variables

Every image MUST accept (and honor) these. ML runners add per-capability vars
on top — see [`infra/env/`](./infra/env/) for templates.

| Var | Default | Purpose |
|---|---|---|
| `CAPABILITY_NAME` | (per image) | Canonical capability identity. See [`CANONICAL-CAPABILITIES.md`](./CANONICAL-CAPABILITIES.md). |
| `DEVICE` | `cuda` (ML) | torch device. ML runners exit non-zero on `cuda` + no GPU. |
| `METRICS_ENABLED` | `false` | Opt-in `/metrics` exposition. |
| `RUNNER_PORT` | `8080` | HTTP bind port. |

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

## What a non-conforming runner breaks

- **Broker liveness probe** if `/healthz` is missing or returns the wrong
  status during load.
- **Capability discovery** if `/<capability>/options` is missing — the
  orch-coordinator can't hydrate the broker's host-config.
- **Work-unit billing** if the runner doesn't report units in the agreed
  shape (body field for Python; header/trailer for Go proxies). See
  [`BROKER-CONTRACT.md`](./BROKER-CONTRACT.md).
- **Operator observability** if `/metrics` doesn't follow Prometheus exposition
  format.
