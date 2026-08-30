# RUNNERS

Per-runner reference. One H2 per image: purpose, endpoints, configuration,
build, deployment notes. Cross-cutting invariants live in
[`RUNNER-INVARIANTS.md`](./RUNNER-INVARIANTS.md); contract details in
[`BROKER-CONTRACT.md`](./BROKER-CONTRACT.md).

---

## openai-chat-runner

Streaming-aware OpenAI chat-completions proxy. Sits between the capability
broker and a local vLLM backend or a supported OpenAI-compatible vendor.
Its added value over a transparent proxy is **token counting for streaming
requests**:

- For `stream: true` requests, scans the upstream SSE response, accumulates
  the final `usage.total_tokens`, and reports it back to the broker via the
  `X-Livepeer-Work-Units` HTTP trailer.
- For non-streaming requests, passes the response body through unchanged. A
  model configured in `OUTPUT_TOKEN_WEIGHT` receives an
  `X-Livepeer-Work-Units` response header; otherwise the broker continues to
  read the configured `usage.*` field from the body with its existing
  `openai-usage` extractor.

### Endpoints

| Method | Path | Purpose |
|---|---|---|
| POST | `/v1/chat/completions` | Forward to upstream; emit work-units trailer on streaming |
| GET  | `/healthz` | 200 once upstream model discovery succeeds |
| GET  | `/<capability>/options` | Structured options for broker hydration |

### Configuration

| Env var | Default | Purpose |
|---|---|---|
| `RUNNER_ADDR` | `:8080` | HTTP bind |
| `UPSTREAM_URL` | (required) | e.g. `http://vllm_chat:8000/v1/chat/completions`; vendor endpoints are shown below. |
| `UPSTREAM_KIND` | `vllm` | Trimmed value; exactly one of `vllm`, `openai`, or `dashscope`. Any other value is a startup configuration error. Advertised in `/options` and used as bounded observability context. |
| `UPSTREAM_API_KEY` | empty | Operator-managed vendor secret. Surrounding whitespace is trimmed. When non-empty, outbound model-discovery and chat requests receive exactly `Authorization: Bearer <key>`; inbound authorization is never forwarded. |
| `MODEL_ALLOWLIST` | empty | Comma-separated, whitespace-trimmed exact model IDs. Empty/unset preserves discovery and requests. Empty entries and duplicates are startup errors. A configured set filters `/v1/models` and `/options` without reordering, and disallowed chat models receive 400 before proxying. |
| `OUTPUT_TOKEN_WEIGHT` | empty | Comma-separated `model=weight` map, with trimmed model IDs and base-10 non-negative integer weights. Empty/unset preserves existing accounting. Malformed, duplicate, fractional, negative, or values not representable as `uint64` are startup errors. |
| `CAPABILITY_NAME` | `openai-chat-completions` | Path segment for `/options` |
| `USAGE_FIELD` | `total_tokens` | Which `usage.*` field to bill on |
| `MODEL_DISCOVERY_RETRIES` | `10` | Startup retries against upstream `/v1/models` |

Discovery metadata (surfaced via `/<capability>/options`, all optional):

| Env var | Maps to broker `extra.*` |
|---|---|
| `SERVED_MODEL_NAME` | `served_model_name` (defaults to first model from upstream `/v1/models`) |
| `BACKEND_MODEL` | `backend_model` |
| `CONTEXT_LENGTH` | `context_length` (integer) |
| `REASONING_PARSER` | `reasoning_parser` |
| `TOOL_CALL_PARSER` | `tool_call_parser` |
| `QUANTIZATION` | `quantization` |

Derived `features` flags always present in `/options`: `streaming: true`,
`include_usage_required: true`; `reasoning: true` iff `REASONING_PARSER` set;
`tool_calling: true` iff `TOOL_CALL_PARSER` set.

### Client request shape

For streaming requests, the runner auto-injects
`stream_options.include_usage: true` if absent.

- **vLLM** honours `include_usage` on all supported releases. Works out of the box.
- **OpenAI and DashScope** accept the OpenAI-compatible
  `stream_options.include_usage` request shape used by the vendor overlays.

Clients can pre-set the flag explicitly (including `include_usage: false`,
which the runner honours).

When a request model matches `OUTPUT_TOKEN_WEIGHT`, work units are
`prompt_tokens + completion_tokens * weight`. Streaming and non-streaming use
the same checked integer calculation; streaming reports it in the existing
trailer and non-streaming reports it in the response header. Overflow is
reported as a runner error, never rounded. A model without a configured weight
follows `USAGE_FIELD` exactly as before. With all four vendor knobs unset,
request/response bytes, model ordering, headers/trailers, and existing
`USAGE_FIELD` behavior are unchanged, apart from the bounded
`upstream_kind=vllm` observability context.

### Vendor pass-through

OpenAI:

```bash
UPSTREAM_KIND=openai
UPSTREAM_URL=https://api.openai.com/v1/chat/completions
UPSTREAM_API_KEY=<operator-secret>
```

DashScope international OpenAI-compatible endpoint:

```bash
UPSTREAM_KIND=dashscope
UPSTREAM_URL=https://dashscope-intl.aliyuncs.com/compatible-mode/v1/chat/completions
UPSTREAM_API_KEY=<operator-secret>
```

The matching compose overlays are
`infra/compose/docker-compose.openai-chat-runner.openai.yml` and
`infra/compose/docker-compose.openai-chat-runner.dashscope.yml`. They expose
the optional settings through environment substitution and do not contain a
credential.

Vendor operation is an operator responsibility. The operator supplies,
rotates, scopes, and protects `UPSTREAM_API_KEY`, ensures the chosen models and
weights match the vendor account, and bears vendor charges, quota, availability,
and policy risk. Upstream 401, 403, 429, 5xx, and transport failures emit the
runner-error signal so the broker refunds the Livepeer job. That refund does
not reverse any charge independently assessed by the vendor. Logs may identify
the bounded upstream kind, HTTP status, and vendor request ID, but never the
secret or vendor response body.

### Build

Multi-arch (amd64 + arm64):

```bash
./build-images.sh build openai-chat-runner
```

### Broker wiring

```yaml
- id: "openai:chat-completions"
  offering_id: "vllm-qwen3.6-27b-stream"
  interaction_mode: "http-stream@v0"
  work_unit:
    name: "tokens"
    extractor:
      type: "response-trailer"
      trailer: "X-Livepeer-Work-Units"
      default: 0
  backend:
    transport: "http"
    url: "http://openai_chat_runner:8080/v1/chat/completions"
    auth: "none"
  extra:
    openai:
      model: "Qwen3.6-27B"
    provider: "openai-chat-runner"
```

---

## openai-embeddings-runner

Mirror of `openai-chat-runner` for embeddings. Reads `usage.total_tokens`
from the response body and emits it as the `X-Livepeer-Work-Units` response
header (no SSE, no trailer — embeddings is request/response).

### Endpoints

| Method | Path | Purpose |
|---|---|---|
| POST | `/v1/embeddings` | Forward to upstream; emit X-Livepeer-Work-Units header |
| GET  | `/healthz` | 200 once upstream model discovery succeeds |
| GET  | `/<capability>/options` | Structured options for broker hydration |

### Configuration

| Env var | Default | Purpose |
|---|---|---|
| `RUNNER_ADDR` | `:8080` | HTTP bind |
| `UPSTREAM_URL` | (required) | `http://vllm_embeddings:8000/v1/embeddings` or `http://ollama:11434/v1/embeddings` |
| `UPSTREAM_KIND` | `vllm` | `vllm` or `ollama` |
| `CAPABILITY_NAME` | `openai-text-embeddings` | Path segment for `/options` |
| `USAGE_FIELD` | `total_tokens` | `prompt_tokens` or `total_tokens` |
| `MODEL_DISCOVERY_RETRIES` | `10` | Startup retries against upstream `/v1/models` |

Discovery metadata:

| Env var | Maps to broker `extra.*` |
|---|---|
| `SERVED_MODEL_NAME` | `served_model_name` |
| `BACKEND_MODEL` | `backend_model` |
| `EMBEDDING_DIMENSIONS` | `embedding_dimensions` |
| `MAX_INPUT_TOKENS` | `max_input_tokens` |
| `POOLING_MODE` | `pooling_mode` |

### Build

Multi-arch (amd64 + arm64):

```bash
./build-images.sh build openai-embeddings-runner
```

### Broker wiring

```yaml
- id: "openai:embeddings"
  offering_id: "bge-large-en-v1.5"
  interaction_mode: "http-reqresp@v0"
  work_unit:
    name: "tokens"
    extractor:
      type: "response-header"
      header: "X-Livepeer-Work-Units"
      default: 0
  health:
    probe:
      type: "http-status"
      config: { url: "http://openai_embeddings_runner:8080/healthz" }
  backend:
    transport: "http"
    url: "http://openai_embeddings_runner:8080/v1/embeddings"
    auth: "none"
  extra:
    openai:
      model: "bge-large-en-v1.5"
    provider: "openai-embeddings-runner"
```

---

## openai-audio-runner

Python FastAPI runner serving `/v1/audio/transcriptions` and
`/v1/audio/translations`. Loads Whisper at startup, keeps the model warm on GPU.

### Endpoints

| Method | Path | Capability |
|---|---|---|
| POST | `/v1/audio/transcriptions` | `openai-audio-transcriptions` |
| POST | `/v1/audio/translations` | `openai-audio-translations` |
| GET | `/healthz` | — |
| GET | `/openai-audio-transcriptions/options` | — |
| GET | `/openai-audio-translations/options` | — |
| GET | `/metrics` | opt-in via `METRICS_ENABLED=true` |

### Configuration

| Env var | Default | Purpose |
|---|---|---|
| `CAPABILITY_NAME` | `openai-audio-transcriptions` | Default capability identity |
| `MODEL_ID` | `openai/whisper-large-v3` | Whisper HF model id |
| `MODEL_DIR` | `/models` | Local model cache |
| `RUNNER_PORT` | `8080` | HTTP bind |
| `DEVICE` | `cuda` | torch device; fail-fast on cuda + no GPU |
| `DTYPE` | `bfloat16` | torch dtype |
| `MAX_QUEUE_SIZE` | `5` | 429 threshold |
| `MAX_AUDIO_MB` | `50` | Max upload size |
| `CHUNK_LENGTH_S` | `30` | Pipeline chunk length |
| `INFERENCE_BATCH_SIZE` | `16` | Pipeline batch size |
| `METRICS_ENABLED` | `false` | Opt-in `/metrics` |

Offering details (response formats, sample rate, chunking) live at
`/etc/runner/offering.yaml` (baked from `infra/offerings/openai-audio-runner.yaml`).

### GPU prerequisites

NVIDIA GPU. Whisper-large-v3 needs ~3–4 GB VRAM in bfloat16; runs comfortably
on Pascal+. Recommended driver: NVIDIA 535+ (matches CUDA 12.x PyTorch
wheels). The runtime image is on a CUDA 13 base, which forward-compats with
older driver versions.

### DEVICE=cpu fallback

CPU inference is ~50× slower; last-resort only. The fail-fast probe applies
if `DEVICE=cuda` is set without a GPU available.

### Model setup

Pre-pull weights via `image-model-downloader`:

```bash
docker run --rm \
  -v ai-whisper-models:/models \
  -e MODEL_IDS="openai/whisper-large-v3" \
  tztcloud/image-model-downloader:v1.3.0
```

Whisper-large-v3 is ~3 GB on disk.

### Build

amd64-only:

```bash
./build-images.sh build openai-audio-runner
```

---

## openai-tts-runner

Kokoro TTS serving `/v1/audio/speech`. Loads `hexgrad/Kokoro-82M` at startup.

### Endpoints

| Method | Path | Capability |
|---|---|---|
| POST | `/v1/audio/speech` | `openai-audio-speech` |
| GET | `/healthz` | — |
| GET | `/openai-audio-speech/options` | — |
| GET | `/metrics` | opt-in |

### Configuration

| Env var | Default | Purpose |
|---|---|---|
| `CAPABILITY_NAME` | `openai-audio-speech` | |
| `MODEL_ID` | `hexgrad/Kokoro-82M` | Kokoro HF model id |
| `MODEL_DIR` | `/models` | |
| `RUNNER_PORT` | `8080` | |
| `DEVICE` | `cuda` | fail-fast on cuda + no GPU |
| `LANG_CODE` | `a` | Kokoro language code (`a`=American, `b`=British, ...) |
| `MAX_QUEUE_SIZE` | `5` | |
| `MAX_INPUT_CHARS` | `4000` | |
| `DEFAULT_VOICE` | `af_bella` | Fallback when request omits |
| `METRICS_ENABLED` | `false` | |

### Voice mapping

OpenAI voice names (`alloy`, `echo`, `fable`, `onyx`, `nova`, ...) are mapped
to Kokoro voices automatically; see `infra/offerings/openai-tts-runner.yaml`
for the full alias table. The `/openai-audio-speech/options` endpoint
returns `default_voice`, `voices.native`, and `voices.aliases` so the broker
can mirror the served voice inventory into manifest metadata.

### GPU prerequisites

Kokoro-82M needs ~1 GB VRAM; fits on any Pascal+ GPU. CPU is acceptable but
~10× slower.

### Model setup

```bash
docker run --rm \
  -v ai-kokoro-models:/models \
  -e MODEL_IDS="hexgrad/Kokoro-82M" \
  tztcloud/image-model-downloader:v1.3.0
```

Kokoro-82M is ~165 MB on disk.

### Build

amd64-only:

```bash
./build-images.sh build openai-tts-runner
```

The image installs `espeak-ng` and `ffmpeg` at runtime (phonemization +
audio encoding).

---

## openai-image-generation-runner

Diffusers-based image generation. Supports SDXL, RealVisXL, and FLUX
families.

### Endpoints

| Method | Path | Capability |
|---|---|---|
| POST | `/v1/images/generations` | `image-generation` |
| GET | `/healthz` | — |
| GET | `/options` and `/image-generation/options` | — |
| GET | `/metrics` | opt-in |

### Configuration

| Env var | Default | Purpose |
|---|---|---|
| `CAPABILITY_NAME` | `image-generation` | |
| `MODEL_ID` | (required) | HF diffusers model id |
| `MODEL_DIR` | `/models` | |
| `RUNNER_PORT` | `8080` | |
| `DEVICE` | `cuda` | fail-fast on cuda + no GPU |
| `DTYPE` | `float16` | |
| `MAX_QUEUE_SIZE` | `5` | |
| `USE_TORCH_COMPILE` | `false` | Toggle `torch.compile()` |
| `DEFAULT_WIDTH` | `1024` | |
| `DEFAULT_HEIGHT` | `1024` | |
| `DEFAULT_STEPS` | (model-dependent) | Inference steps |
| `DEFAULT_GUIDANCE` | (model-dependent) | Guidance scale |
| `METRICS_ENABLED` | `false` | |
| `TRITON_CACHE_DIR` | `/cache/triton` | Persistent kernel cache |

The runner detects the model family from `MODEL_ID` and applies sensible
defaults (FLUX vs RealVisXL vs SDXL).

### GPU prerequisites

NVIDIA GPU with Ada or Blackwell architecture recommended (RTX 4090 / 5090).
Driver: NVIDIA 545+. The runtime image is on a CUDA 13 base.

VRAM by model:

| Model | Family | VRAM | Notes |
|---|---|---|---|
| `SG161222/RealVisXL_V4.0_Lightning` | RealVisXL (SDXL) | ~6 GB | Lightning variant; 6 inference steps |
| `black-forest-labs/FLUX.1-dev` | FLUX | ~12–16 GB peak | Loads with `model_cpu_offload` |
| `stabilityai/stable-diffusion-xl-base-1.0` | SDXL | ~8 GB | Standard SDXL |

FLUX needs at least 24 GB VRAM (RTX 4090 / RTX 6000 Ada).

### Model setup

FLUX is gated; supply `HF_TOKEN`:

```bash
docker run --rm \
  -v ai-image-models:/models \
  -e MODEL_IDS="SG161222/RealVisXL_V4.0_Lightning,black-forest-labs/FLUX.1-dev" \
  -e HF_TOKEN=hf_xxx \
  tztcloud/image-model-downloader:v1.3.0
```

Pre-compile Triton kernels for faster cold starts via [`setup-models.sh`](./setup-models.sh).

### torch.compile + Triton cache

Set `USE_TORCH_COMPILE=true` to enable kernel compilation (faster
steady-state, slower cold start). The Triton cache lives at `/cache/triton`;
mount a volume to persist across restarts.

### Build

amd64-only:

```bash
./build-images.sh build openai-image-generation-runner
```

---

## rerank-runner

Cohere-compatible reranker (`/v1/rerank`). Loads `zeroentropy/zerank-2`, a
4-billion-parameter Qwen3-based CrossEncoder.

### Endpoints

| Method | Path | Purpose |
|---|---|---|
| POST | `/v1/rerank` | Score + reorder docs against a query |
| GET | `/healthz` | — |
| GET | `/rerank/options` | — |
| GET | `/metrics` | opt-in |

### Configuration

| Env var | Default | Purpose |
|---|---|---|
| `CAPABILITY_NAME` | `rerank` | |
| `MODEL_ID` | `zeroentropy/zerank-2` | CrossEncoder model id |
| `MODEL_DIR` | `/models` | |
| `RUNNER_PORT` | `8080` | |
| `DEVICE` | `cuda` | fail-fast on cuda + no GPU |
| `DTYPE` | `bfloat16` | |
| `MAX_QUEUE_SIZE` | `5` | 429 threshold |
| `MAX_BATCH_SIZE` | `1000` | Per-request doc cap |
| `INFERENCE_BATCH_SIZE` | `64` | Internal `model.predict()` batch |
| `METRICS_ENABLED` | `false` | |

### GPU prerequisites

NVIDIA GPU with Pascal+ architecture. Driver: NVIDIA 535+. zerank-2
(4-billion params, bfloat16) needs ~8 GB VRAM. Fits on RTX 3090 / 4080 /
A100 / L40 / RTX 4090.

### DEVICE=cpu fallback

CPU reranking is feasible but slow (~30s for 100 docs).

### Model setup

Use the companion downloader:

```bash
docker run --rm \
  -v ai-rerank-models:/models \
  -e MODEL_ID="zeroentropy/zerank-2" \
  tztcloud/rerank-model-downloader:v1.3.0
```

zerank-2 is ~8 GB on disk.

### Tuning

- `MAX_BATCH_SIZE` — per-request doc cap; raise for large-corpus workloads at
  the cost of memory pressure.
- `INFERENCE_BATCH_SIZE` — internal predict batch; raise on bigger GPUs for
  throughput.
- `MAX_QUEUE_SIZE` — concurrent request cap; 429 when exceeded.

### Build

amd64-only:

```bash
./build-images.sh build rerank-runner
```

### Dependency note

`sentence-transformers` is pinned `<5.0.0` and `transformers` is pinned
`<5.0.0` because zerank-2 was authored against the 3.x/4.x APIs. The 5.x
releases of either package break CrossEncoder inference at runtime
(`BatchEncoding` reaching `torch.embedding()`). The Dockerfile asserts the
pin at build time.

---

## image-model-downloader

One-shot Docker image that pre-downloads HuggingFace diffusers / Whisper /
Kokoro models into a shared volume. Run once per host before the actual
runner containers start; not needed during normal operation.

### Configuration

| Env var | Default | Purpose |
|---|---|---|
| `MODEL_IDS` | `SG161222/RealVisXL_V4.0_Lightning` | Comma-separated list of HF model ids |
| `MODEL_DIR` | `/models` | Download directory |
| `HF_TOKEN` | — | Required for gated models (e.g. FLUX) |

### Usage

```bash
docker run --rm \
  -v ai-image-models:/models \
  -e MODEL_IDS="SG161222/RealVisXL_V4.0_Lightning,black-forest-labs/FLUX.1-dev" \
  tztcloud/image-model-downloader:v1.3.0
```

Or via compose:

```bash
docker compose -f infra/compose/docker-compose.audio.yml run --rm image_model_downloader
```

### Build

```bash
./build-images.sh build image-model-downloader
```

---

## rerank-model-downloader

Same shape as `image-model-downloader` but for the rerank model. Defaults to
`zeroentropy/zerank-2`.

### Configuration

| Env var | Default | Purpose |
|---|---|---|
| `MODEL_ID` | `zeroentropy/zerank-2` | HF model id |
| `MODEL_DIR` | `/models` | Download directory |
| `HF_TOKEN` | — | For gated models |

### Build

```bash
./build-images.sh build rerank-model-downloader
```

---

## openai-tester

Node.js integration test harness that exercises every runner through the
OpenAI SDK. One test script per capability. It also contains the deterministic
authenticated `mockvendor` Go package used by the chat runner integration test;
the fixture implements model discovery and streaming/non-streaming chat without
contacting an external vendor.

### Test scripts

| Script | Capability |
|---|---|
| `generate-test-audio.sh` | Generate a local spoken audio fixture for transcription tests |
| `test-gateway-chat.mjs` | OpenAI SDK smoke against an OpenAI-compatible gateway |
| `test-chat-completion.mjs` | `openai-chat-completions` |
| `test-text-embedding.mjs` | `openai-text-embeddings` |
| `test-audio-transcription.mjs` | `openai-audio-transcriptions` |
| `test-audio-translation.mjs` | `openai-audio-translations` |
| `test-audio-speech.mjs` | `openai-audio-speech` |
| `test-image-generation.mjs` | `image-generation` |
| `mockvendor/` | Authenticated OpenAI-compatible fixture for Go integration tests |

### Configuration

| Env var | Default | Purpose |
|---|---|---|
| `OPENAI_BASE_URL` | `http://localhost:8090/v1` | Runner endpoint |
| `OPENAI_API_KEY` | *(required at run time; image has no default)* | Bearer token. Runners ignore the value — `local-dev-no-auth` is fine for local smoke; a real key is only needed when hitting a remote gateway. |
| `MODEL` | varies per script | Model alias |

### Run

Local Node:

```bash
cd openai-tester
npm install
node test-chat-completion.mjs

./generate-test-audio.sh test.ogg
OPENAI_BASE_URL=http://localhost:8090/v1 \
OPENAI_API_KEY=local-dev-no-auth \
MODEL=whisper-large-v3 \
AUDIO_FILE=test.ogg \
node test-audio-transcription.mjs

OPENAI_BASE_URL=https://openai-gw-sea.cloudspe.com/v1 \
OPENAI_API_KEY=sk-live-... \
MODEL=gpt-4.1-mini \
node test-gateway-chat.mjs
```

Or via Docker:

```bash
docker run --rm \
  -e OPENAI_BASE_URL=http://broker:8090/v1 \
  local/openai-tester:v1.3.0 \
  node test-chat-completion.mjs
```

### Build

```bash
./build-images.sh build openai-tester
```
