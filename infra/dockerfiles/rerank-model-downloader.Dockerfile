# rerank-model-downloader — one-shot HF puller for rerank-runner.
# Build context: repo root.

ARG REGISTRY=tztcloud
ARG LOCAL_REGISTRY=local
ARG TAG=v2.0.0
ARG BASE_IMAGE=${LOCAL_REGISTRY}/python-base:${TAG}

FROM ${BASE_IMAGE}

WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends git \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

COPY rerank-runner/model-downloader/pyproject.toml ./
RUN uv pip install --no-cache "huggingface_hub>=0.20.0"

COPY --chown=runner:runner rerank-runner/model-downloader/src/download.py ./

RUN mkdir -p /models && chown -R runner:runner /models /app

VOLUME /models
ENV MODEL_DIR=/models \
    MODEL_ID=zeroentropy/zerank-2

USER runner

CMD ["python", "download.py"]
