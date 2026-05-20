# PLANS

Forward-looking work. Active items at the top; completed items get a strikethrough
or move to [`CHANGELOG.md`](./CHANGELOG.md).

## Active

- **Shrink ML runner image sizes.** Each ML runner is ~10 GB today: the
  `nvidia/cuda:13.2.1-runtime` base provides CUDA libs AND the PyTorch wheels
  bundle their own copies — paying the CUDA storage cost twice. Low-risk
  reductions (~3 GB / image):
  - Switch base from `cuda:13.2.1-runtime-ubuntu24.04` to
    `cuda:13.2.1-base-ubuntu24.04` (PyTorch's bundled libs do the GPU work;
    the `base` variant keeps the nvidia-container env vars).
  - Strip `__pycache__/` from `/opt/venv` after install (bytecode regenerates
    on first import; +~2s cold start).
  - Strip `*.pyi` stub files + tests from PyTorch / transformers.

  Aggressive reduction (~4 GB / image, medium risk): drop the CUDA base
  entirely, use `python:3.13-slim`. Works on hosts with
  `nvidia-container-toolkit` but breaks anything that shells out to
  `nvidia-smi` or `/usr/local/cuda`.

- **Bump PyTorch wheel index to `cu130` once published.** Currently using
  `cu128` (latest CUDA 12.x wheels) on the CUDA 13 base via forward-compat.
  Switch is a one-line change to `PYTORCH_INDEX_URL` in
  [`build-images.sh`](./build-images.sh).
- **First validated end-to-end build.** Verify `./build-images.sh build` runs
  cleanly on a clean host with Docker + buildx installed. Open an issue with
  the first failure as the entry point.
- **GPU smoke gate.** The CI workflows don't run GPU smoke (no GPUs on
  GitHub-hosted runners). Decide whether to add a self-hosted runner or
  document a manual pre-release gate in [`RUNNERS.md`](./RUNNERS.md).
- **Lockfiles.** `pyproject.toml`s are not yet paired with `uv.lock` files.
  Add `uv lock` outputs per runner so production builds are deterministic.

## Considered, not committed

- **Consolidate Go proxies.** `openai-chat-runner` and `openai-embeddings-runner`
  share a lot of structure. A single binary with subcommands is an option;
  current decision is to keep them separate.
- **Distroless Go runtime.** Currently `alpine:3.20`. Could move to
  `gcr.io/distroless/static-debian12` for slightly leaner images.
- **Python 3.14 bump.** Current pin is 3.13. Re-evaluate once 3.14 ships and
  major deps (torch, diffusers, transformers) test green against it.
- **Custom lints for runner invariants.** Mechanical check that every
  Dockerfile sets `CAPABILITY_NAME`, exposes 8080, has a HEALTHCHECK, etc.

## Completed

See [`CHANGELOG.md`](./CHANGELOG.md).
