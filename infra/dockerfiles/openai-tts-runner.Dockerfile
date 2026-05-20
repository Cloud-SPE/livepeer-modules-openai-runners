# openai-tts-runner — Kokoro TTS on CUDA 13.
# Build context: repo root.

ARG REGISTRY=tztcloud
ARG LOCAL_REGISTRY=local
ARG TAG=v1.3.0
ARG BASE_IMAGE=${LOCAL_REGISTRY}/cuda13-python-base:${TAG}
ARG PYTORCH_INDEX_URL=https://download.pytorch.org/whl/cu128

FROM ${BASE_IMAGE} AS builder

# Build deps live only in the builder stage; runtime stage doesn't FROM builder
# so build-essential never ships in the final image.
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        build-essential \
        espeak-ng \
        ffmpeg \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

ARG PYTORCH_INDEX_URL
RUN uv pip install --no-cache torch torchaudio --index-url ${PYTORCH_INDEX_URL}

WORKDIR /build
COPY openai-tts-runner/pyproject.toml ./
COPY openai-tts-runner/src ./src
RUN uv pip install --no-cache \
        "fastapi>=0.115.0,<0.117.0" \
        "pydantic>=2.9.0,<3.0.0" \
        "pydantic-settings>=2.5.0,<3.0.0" \
        "structlog>=24.4.0,<25.0.0" \
        "uvicorn[standard]>=0.30.0,<0.33.0" \
        "prometheus-client>=0.21.0,<0.22.0" \
        "python-multipart>=0.0.12,<0.1.0" \
    && uv pip install --no-cache . \
    && python -m spacy download en_core_web_sm

FROM ${BASE_IMAGE} AS runtime

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        espeak-ng \
        ffmpeg \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder --chown=runner:runner /opt/venv /opt/venv
COPY --chown=runner:runner infra/offerings/openai-tts-runner.yaml /etc/runner/offering.yaml

RUN mkdir -p /models/huggingface && chown -R runner:runner /models /etc/runner

VOLUME /models

ENV CAPABILITY_NAME=openai-audio-speech \
    MODEL_ID=hexgrad/Kokoro-82M \
    MODEL_DIR=/models \
    RUNNER_PORT=8080 \
    DEVICE=cuda \
    MAX_QUEUE_SIZE=5 \
    LANG_CODE=a \
    MAX_INPUT_CHARS=4000 \
    DEFAULT_VOICE=af_bella \
    HF_HOME=/models/huggingface \
    METRICS_ENABLED=false

USER runner

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --retries=3 \
    CMD python -c "import urllib.request; urllib.request.urlopen('http://localhost:8080/healthz')" || exit 1

CMD ["python", "-m", "openai_tts_runner"]
