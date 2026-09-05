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

- **Pin PyTorch.** `torch` is unpinned in every CUDA Dockerfile; the cu128
  index resolved 2.11 and the cu126 index 2.14 on 2026-09-04. With two
  flavors floating independently the Pascal build can drift from the default
  one. Pin per flavor.
- **Validate the `-pascal` flavor on real sm_61 hardware.** Built by the
  release workflow from cu126 wheels (which carry sm_60 kernels) but never
  run on a GTX 1080. The GPU probe's architecture check is the only guard.
- **End-to-end attach + certification.** Point a real pool member agent
  (`livepeer-network-modules` ≥ 3686b67) at each image, confirm the exception
  queue is empty and the offer freezes with the identity the contract
  declared. The contract shapes are unit-tested against the agent's parser
  rules and the catalog's match values only.
- **Python runtime tests.** The FastAPI apps load a model at import; only the
  pure `contract.py` / `gpu_probe.py` modules are tested. A model-free app
  fixture would let the routes be exercised in CI.
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

- **Weighted output-token billing through the broker.** `OUTPUT_TOKEN_WEIGHT`
  only affects the chat runner's own header/trailer; the declared
  `openai-usage` extractor reads the body. If weighted billing is wanted it is
  a broker-side option on `openai-usage`, not a runner-side count (agreed with
  the network-modules team, 2026-09-04).

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

- ~~**Runner contract migration (v2.0.0).**~~ Every image serves
  `GET /.well-known/livepeer-runner`; `/options` removed; colon-form ids;
  `BROKER-CONTRACT.md` rewritten; tests in CI. See [`CHANGELOG.md`](./CHANGELOG.md).
