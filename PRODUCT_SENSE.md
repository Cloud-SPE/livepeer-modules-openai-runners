# PRODUCT_SENSE

Who uses these runners and what they're optimizing for.

## Direct consumer: the capability broker

The capability broker is the only direct consumer, reached through the pool
member agent on the runner's host. The agent reads the runner's contract once
per attach and relays it; the broker validates it, matches it to a catalog
template, receives requests from the gateway tier, validates payment, and
dispatches to the path the runner declared. The runner sees only
fully-authenticated HTTP at its declared endpoint plus informational headers
(`Livepeer-Runner-Local-Id`, `Livepeer-Capability`, `Livepeer-Offering`).

What the broker needs from a runner:

- A stable HTTP surface per capability.
- A `GET /healthz` that returns 200 once warm.
- A `GET /.well-known/livepeer-runner` contract: capability id, transports,
  paths, readiness, identity, and the work-unit extractor to run.
- A body or `X-Livepeer-Work-Units` header/trailer the declared extractor can
  read, where the count is the runner's to know.
- Optional `/metrics` for operator observability.

## Indirect consumers

**Orch operators.** Run the broker + runners on their own hardware. They want
predictable builds, lean images, fail-fast startup, and clear `/metrics` when
opted in.

**Application developers.** Hit the gateway with OpenAI SDK calls. They
shouldn't need to know a Livepeer runner is on the other end — the OpenAI
shape is the product surface.

**Future runner authors.** When a new capability is added, the next author
should be able to copy a Dockerfile, fork a runner subdir, and follow
[`RUNNER-INVARIANTS.md`](./RUNNER-INVARIANTS.md) to hit the bar.

## What's NOT a product concern of this repo

- End-user identity, billing, payment cryptography. Lives upstream.
- Routing decisions, multi-region failover. Lives in the gateway.
- Long-term storage of inputs/outputs. Runners are stateless by design.
- UI / dashboards. The operator console is a sibling product.

## Quality bar

A runner is "good" when:

1. It comes up clean on a supported GPU within ~30 seconds.
2. `GET /healthz` flips to 200 once the model is loaded.
3. The first real request returns within model-typical latency.
4. Failure modes (out-of-memory, malformed input, upstream down) return
   appropriate HTTP status codes — not 500 stack traces.
5. `/metrics` (when enabled) emits the standard Prometheus exposition.
6. The Docker image is < 6 GB for ML runners, < 50 MB for Go proxies,
   < 200 MB for the Node tester.
