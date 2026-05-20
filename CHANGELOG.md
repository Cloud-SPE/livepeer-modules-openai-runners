# Changelog

All notable changes to this repo. Format roughly follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [v1.3.0] — 2026-05-19

Initial release of the standalone runner repo. Includes:

- **OpenAI-shaped runners**: `openai-chat-runner` (Go), `openai-embeddings-runner` (Go),
  `openai-audio-runner` (Python — Whisper STT), `openai-tts-runner` (Python — Kokoro TTS),
  `openai-image-generation-runner` (Python — diffusers).
- **Cohere-compatible runner**: `rerank-runner` (Python — sentence-transformers CrossEncoder).
- **Helpers**: `image-model-downloader`, `rerank-model-downloader`, `openai-tester` (Node smoke harness).
- **Shared bases**: `python-base` (CPU; Python 3.13) and `cuda13-python-base` (CUDA 13 + Python 3.13).
- **Build orchestration**: single `build-images.sh` at repo root.
- **Infra layout**: docker-compose overlays, offering manifests, and env templates centralized under `infra/`.
- **CI**: GitHub Actions workflows for PR builds, tagged releases, and weekly doc-gardening.

[v1.3.0]: https://github.com/Cloud-SPE/livepeer-modules-openai-runners/releases/tag/v1.3.0
