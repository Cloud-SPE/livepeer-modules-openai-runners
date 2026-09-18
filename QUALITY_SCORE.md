# QUALITY_SCORE

Per-image grade and known gaps. Update in the same PR that changes the bar.

Grade scale:

- **A** — production-bar; gaps documented; on-call signed off.
- **B** — works end-to-end; gaps known and tracked.
- **C** — scaffold; builds; not yet exercised under real load.
- **D** — known-broken or missing critical path; do not deploy.

## Current grades (v2.0.0 — runner contract)

| Image | Grade | Notes |
|---|---|---|
| `python-base` | C | Built fresh; not yet validated as a base by all consumers |
| `cuda13-python-base` | C | CUDA 13 + uv-managed Python 3.13; PyTorch via cu128 wheels (sm_75+); `-pascal` flavor on cu126 unvalidated on real sm_61 hardware |
| `openai-chat-runner` | B | Contract, `/v1/models`, vendor pass-through and weighted accounting covered by unit + integration tests run in CI; not yet certified against a live broker |
| `openai-embeddings-runner` | B | Contract and usage-header tests run in CI; not yet certified against a live broker |
| `openai-audio-runner` | C | Two-entry contract and GPU probe unit-tested; needs end-to-end Whisper smoke on a GPU |
| `openai-tts-runner` | C | Contract unit-tested; needs Kokoro voice-mapping smoke + espeak-ng runtime presence verification |
| `openai-image-generation-runner` | C | Contract unit-tested; xformers + diffusers compat with cu128 on CUDA 13 unverified |
| `rerank-runner` | C | Contract + `X-Livepeer-Work-Units` header unit-tested; sentence-transformers `<5.0.0` pin survives the rewrite |
| `image-model-downloader` | C | Simple HF puller; needs end-to-end pull validation |
| `rerank-model-downloader` | C | Same as image-model-downloader |
| `openai-tester` | C | Now 2-stage; the eight test scripts unchanged |

## Cross-cutting gaps tracked here

- **End-to-end smoke against a running broker** — none yet executed; the
  contract has been validated only against the agent's parser rules and the
  catalog's match values, not a live attach + certification.
- **GPU runtime validation** — Dockerfiles syntax-checked only. The Python
  FastAPI apps have no runtime tests (they load a model at startup); only the
  pure `contract.py` / `gpu_probe.py` modules are unit-tested.
- **Pascal flavor** — built by the release workflow, never run on an sm_61
  card.
- **Image size measurement** — not yet recorded; should land before any grade promotes above C.
- **Reproducible builds** — no `uv.lock` files yet (see [`PLANS.md`](./PLANS.md)).
