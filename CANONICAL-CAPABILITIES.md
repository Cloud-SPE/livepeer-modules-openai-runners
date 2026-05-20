# CANONICAL-CAPABILITIES

The allowed `CAPABILITY_NAME` env-var values. One value per image (the audio
runner is the documented exception). New capabilities require a doc change
here AND a runner image AND a broker dispatch entry.

| Capability | Runner | Endpoint |
|---|---|---|
| `openai-chat-completions` | `openai-chat-runner` | `POST /v1/chat/completions` |
| `openai-text-embeddings` | `openai-embeddings-runner` | `POST /v1/embeddings` |
| `openai-audio-transcriptions` | `openai-audio-runner` | `POST /v1/audio/transcriptions` |
| `openai-audio-translations` | `openai-audio-runner` | `POST /v1/audio/translations` |
| `openai-audio-speech` | `openai-tts-runner` | `POST /v1/audio/speech` |
| `image-generation` | `openai-image-generation-runner` | `POST /v1/images/generations` |
| `rerank` | `rerank-runner` | `POST /v1/rerank` |

## Rules

1. **Names are stable.** Renaming a capability is a breaking change. Add a
   new capability instead.
2. **One value per env var.** `CAPABILITY_NAME=openai-chat-completions` is
   valid; comma-separated lists are not.
3. **The audio exception is intentional.** `openai-audio-runner` serves both
   `openai-audio-transcriptions` and `openai-audio-translations` from the same
   Whisper model load. The `CAPABILITY_NAME` env var picks the *default* for
   that image; both endpoints are always available on the same container.
4. **`/options` endpoint name follows the capability.** A runner with
   `CAPABILITY_NAME=rerank` exposes `GET /rerank/options`.
5. **Adding a new capability** requires:
   - A new entry in this table.
   - A new runner image (or extension to an existing one for related
     capabilities).
   - A new entry in the broker's `host-config.yaml` mapping the offering to
     the runner.
   - An update to [`RUNNERS.md`](./RUNNERS.md).
