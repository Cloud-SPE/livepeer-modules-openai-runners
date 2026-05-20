# image-model-downloader — one-shot HF puller for image-gen + audio runners.
# Build context: repo root.

ARG REGISTRY=tztcloud
ARG LOCAL_REGISTRY=local
ARG TAG=v1.3.0
ARG BASE_IMAGE=${LOCAL_REGISTRY}/python-base:${TAG}

FROM ${BASE_IMAGE}

WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends git \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

COPY image-model-downloader/pyproject.toml ./
RUN uv pip install --no-cache "huggingface_hub>=0.23.0"

COPY --chown=runner:runner image-model-downloader/src/download.py ./

RUN mkdir -p /models && chown -R runner:runner /models /app

VOLUME /models
ENV MODEL_DIR=/models

USER runner

CMD ["python", "download.py"]
