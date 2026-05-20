# SECURITY

Trust model + threat boundaries. The deep dive on what runners see vs. what
broker handles is in [`TRUST-MODEL.md`](./TRUST-MODEL.md); this file is the
defensive-posture summary.

## Trust boundary

```text
gateway  →  capability-broker  →  runner
            ^                     ^
            customer identity     paid HTTP request only
            payment validation
            mode dispatch
```

Runners sit **behind** the broker. They receive only HTTP requests that the
broker has already authenticated and validated payment for. Runners do not
see customer identity, API keys, or payment envelopes.

## What runners can be trusted with

- **Inputs to the model** (audio bytes, prompts, embeddings text). Treat as
  untrusted user input — already validated by the broker for size/type, but
  malicious payloads can still target the model itself (jailbreaks, prompt
  injection, oversized inputs that slip through).
- **Their own offering manifest** (`/etc/runner/offering.yaml`). Read-only,
  baked at image build time.
- **Their model weights** (in `MODEL_DIR` volume). Treat as immutable.

## What runners must NOT do

- **Decrypt or validate payment envelopes.** The `payment-daemon` upstream
  is the authority.
- **Authorize requests based on identity.** The broker passes only
  informational headers (`Livepeer-Capability`, `Livepeer-Offering`); these
  are not auth tokens.
- **Persist user inputs across requests.** Per-job state only; no DB.
- **Initiate outbound calls to the gateway or broker.** Runners answer
  inbound HTTP; they don't talk back upstream.

## Hardening checklist (each runner)

- [x] Runs as a non-root user. Every image — Python ML runners, Go proxies,
  Node tester, and downloaders — runs as `runner` (UID 1000). The shared
  bases create the user once; downstream runtime stages `USER runner` after
  apt/COPY.
- [x] No shell in the runtime image entrypoint.
- [x] No build-time secrets in image layers (HF tokens come from env at run time).
- [x] Healthcheck doesn't reveal internal state to unauthenticated callers.
- [x] `/metrics` is opt-in and exposes only operational counters, no input data.
- [ ] Per-runner SBOM published with each release (tracked in [`PLANS.md`](./PLANS.md)).

## Reporting

Security issues against the broker, gateway, or payment chain go upstream to
the Livepeer security team — see the upstream repo's `SECURITY.md`. Issues
against a runner Dockerfile or runner code go to this repo's issue tracker
with the `security` label.
