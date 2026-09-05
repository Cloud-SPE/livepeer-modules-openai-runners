# DESIGN

One-page mental model. Read [`ARCHITECTURE.md`](./ARCHITECTURE.md) for the
structural picture.

## What this repo is

A bundle of Docker images that present OpenAI-shaped (and one Cohere-compatible)
HTTP endpoints to the Livepeer capability broker. Each image is a workload
binary that runs as one process per broker-dispatched container; the broker is
the client, the runner is the server.

## Mental model

**One image per capability.** The broker forwards a paid request to the runner;
the runner does the work and returns the response. Runners are stateless:
per-job in-memory state plus a per-process model load. No DB.

**Three control knobs per runner** (see [`CORE-BELIEFS.md`](./CORE-BELIEFS.md)):

- `CAPABILITY_NAME` — image-tag-pinned canonical capability identity.
- `DEVICE` — torch device for ML runners (default `cuda`; fail-fast if no GPU).
- `METRICS_ENABLED` — opt-in `/metrics` exposition.

Plus per-capability keys (see [`RUNNERS.md`](./RUNNERS.md) for the full list
per runner, or [`infra/env/`](./infra/env/) for copy-able templates).

## Shared Python base images

`python-base` is the CPU Python base inheriting from `python:3.13-slim` with
`uv` preinstalled and an empty `/opt/venv` ready for downstream installs.

`cuda13-python-base` is the CUDA 13 Python base inheriting from
`nvidia/cuda:13.2.1-runtime-ubuntu24.04` with `uv`-managed Python 3.13 and
an empty `/opt/venv`.

Each runner's Dockerfile multi-stages on top: the builder stage installs
PyTorch + runner deps + the runner itself into the venv; the runtime stage
copies the venv into a fresh base image and adds only runtime apt packages
(`ffmpeg`, `espeak-ng`) where needed.

The Go runners (`openai-chat-runner`, `openai-embeddings-runner`) are
independent — they use `golang:1.25.7-alpine` for build and `alpine:3.20` for
runtime. No shared base.

The Node tester (`openai-tester`) uses a two-stage `node:22-alpine` build.

## Why uv

Replaces pip in every Python image. Faster installs, lockfile-friendly, manages
standalone Python builds — meaning the CUDA base can host Python 3.13 cleanly
without depending on Ubuntu's system Python.

## Why CUDA 13 + cu128 PyTorch wheels (today)

We pin the runtime base to CUDA 13 because the target hardware is current
(RTX 4090 / 5090) and we want to be future-proof. PyTorch's CUDA 13 wheels
were not stable at lift time; we install the latest CUDA 12.x wheels
(`cu128`) on the CUDA 13 base — CUDA forward-compat handles the gap. Bumping
to `cu130` once PyTorch publishes it is a one-line change in
[`infra/build/image-versions.env`](./infra/build/image-versions.env) (`PYTORCH_INDEX_URL_DEFAULT`). Tracked in
[`PLANS.md`](./PLANS.md).

## What stays out of this repo

- **Customer auth + billing.** Lives upstream of the broker.
- **Payment validation.** Broker-side; the runner sees only fully-authenticated requests.
- **Capability registration.** The runner declares itself at
  `GET /.well-known/livepeer-runner`; the pool member agent relays that once
  per attach and the broker validates, matches, and freezes the offer. The
  runner owns the declaration, not the registration.
- **Work-unit counting.** The runner declares the extractor in its contract;
  the capability broker runs it.
- **Wire-protocol middleware / gateway adapters.** Gateway-tier, not runner-tier.
