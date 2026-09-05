# CANONICAL-CAPABILITIES

The `capability_id` each image declares in its runner contract
(`GET /.well-known/livepeer-runner`), and the default `CAPABILITY_NAME` that
produces it. One image, one capability entry — the audio runner is the
documented exception. New capabilities require a doc change here AND a runner
image AND a catalog template on the network side.

| `capability_id` | Runner | Endpoint | Identity key |
|---|---|---|---|
| `openai:chat-completions` | `openai-chat-runner` | `POST /v1/chat/completions` | `openai.model` |
| `openai:embeddings` | `openai-embeddings-runner` | `POST /v1/embeddings` | `openai.model` |
| `openai:audio-transcriptions` | `openai-audio-runner` | `POST /v1/audio/transcriptions` | `openai.model` |
| `openai:audio-translations` | `openai-audio-runner` | `POST /v1/audio/translations` | `openai.model` |
| `openai:audio-speech` | `openai-tts-runner` | `POST /v1/audio/speech` | `openai.model` |
| `openai:images-generations` | `openai-image-generation-runner` | `POST /v1/images/generations` | `openai.model` |
| `text:rerank` | `rerank-runner` | `POST /v1/rerank` | `model` |

## The naming rule

From `runner-attach.md` §3.2 ("Capability id vocabulary") in
`livepeer-network-modules`:

- **Prefix** is the wire family when the capability implements a real,
  externally specified API: `openai:`. Otherwise it is the product domain
  (`text:`, `audio:`, `video:`, `vision:`). `livepeer:` is never a prefix.
- **Suffix** for `openai:` is the endpoint name with `/` folded to `-`
  (`/v1/chat/completions` → `chat-completions`, `/v1/images/generations` →
  `images-generations`). Otherwise it is what the capability does (`rerank`).
- Exactly one `:`, never `/`.

The prefix is a promise about the wire: a caller who sends an OpenAI request
to an `openai:` capability gets an OpenAI response. Rerank resembles Cohere,
not OpenAI, so it is `text:rerank` and its identity lives under the plain
`model` key rather than `openai.model`.

The broker treats `capability_id` as opaque — no enum, no shape check. The
form matters because the catalog's templates, both gateways, and the runner
have to agree on the string; a runner declaring the old hyphen form attaches
fine and matches no template.

## Rules

1. **Names are stable.** Renaming a capability is a breaking change (v2.0.0
   was one). Add a new capability instead.
2. **`CAPABILITY_NAME` overrides the default, one value per image.** It is
   the `capability_id` the contract carries. Comma-separated lists are not
   accepted.
3. **The audio exception is intentional.** `openai-audio-runner` serves
   `openai:audio-transcriptions` and `openai:audio-translations` from the
   same Whisper load and, with `CAPABILITY_NAME` unset, returns both as a
   two-element array from one port. Set `CAPABILITY_NAME` to one of the two
   to advertise only that entry (a pool host places one template per
   service); any other value is a startup error.
4. **Identity is what sells.** The catalog matches `identity.openai.model`
   (or `identity.model` for rerank) by exact string equality. The value is
   the name a caller puts in `"model"` — `whisper-large-v3`, `kokoro`,
   `zerank-2`, the served chat model — not the HuggingFace path, which goes
   in `x-backend-model`. Image generation is the exception: its templates
   match on the HF id (`black-forest-labs/FLUX.1-dev`), so `MODEL_ID` is the
   identity there.
5. **Adding a new capability** requires:
   - A new row in this table.
   - A new runner image (or a new contract entry in an existing one for
     related capabilities, as the audio runner does).
   - A catalog template in `livepeer-network-modules/templates/` whose
     `match` selects the identity the runner declares.
   - An update to [`RUNNERS.md`](./RUNNERS.md) and
     [`BROKER-CONTRACT.md`](./BROKER-CONTRACT.md).
