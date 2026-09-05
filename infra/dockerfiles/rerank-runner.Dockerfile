# rerank-runner — Cohere-compatible CrossEncoder on CUDA 13.
# Build context: repo root.

ARG REGISTRY=tztcloud
ARG LOCAL_REGISTRY=local
ARG TAG=v2.0.0
ARG BASE_IMAGE=${LOCAL_REGISTRY}/cuda13-python-base:${TAG}
ARG PYTORCH_INDEX_URL=https://download.pytorch.org/whl/cu128

FROM ${BASE_IMAGE} AS builder

# Build deps live only in the builder stage; runtime stage doesn't FROM builder
# so build-essential never ships in the final image.
RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

ARG PYTORCH_INDEX_URL
RUN uv pip install --no-cache torch --index-url ${PYTORCH_INDEX_URL}

WORKDIR /build
COPY rerank-runner/pyproject.toml ./
COPY rerank-runner/src ./src
RUN uv pip install --no-cache \
        "fastapi>=0.115.0,<0.117.0" \
        "pydantic>=2.9.0,<3.0.0" \
        "pydantic-settings>=2.5.0,<3.0.0" \
        "structlog>=24.4.0,<25.0.0" \
        "uvicorn[standard]>=0.30.0,<0.33.0" \
        "prometheus-client>=0.21.0,<0.22.0" \
        "python-multipart>=0.0.12,<0.1.0" \
    && uv pip install --no-cache . \
    && python -c "from importlib.metadata import version; assert int(version('sentence-transformers').split('.', 1)[0]) < 5, version('sentence-transformers'); assert int(version('transformers').split('.', 1)[0]) < 5, version('transformers')"

FROM ${BASE_IMAGE} AS runtime

COPY --from=builder --chown=runner:runner /opt/venv /opt/venv
COPY --chown=runner:runner infra/offerings/rerank-runner.yaml /etc/runner/offering.yaml

RUN mkdir -p /models && chown -R runner:runner /models /etc/runner

VOLUME /models

ENV CAPABILITY_NAME=text:rerank \
    MODEL_ID=zeroentropy/zerank-2 \
    MODEL_DIR=/models \
    RUNNER_PORT=8080 \
    DEVICE=cuda \
    DTYPE=bfloat16 \
    MAX_QUEUE_SIZE=5 \
    MAX_BATCH_SIZE=1000 \
    INFERENCE_BATCH_SIZE=64 \
    METRICS_ENABLED=false

USER runner

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --retries=3 \
    CMD python -c "import urllib.request; urllib.request.urlopen('http://localhost:8080/healthz')" || exit 1

CMD ["python", "-m", "rerank_runner"]
