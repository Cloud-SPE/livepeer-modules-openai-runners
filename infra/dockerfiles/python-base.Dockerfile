# Shared CPU Python base image.
# Provides: Python 3.13, uv (Astral), an empty /opt/venv, common apt essentials,
# and a non-root `runner` user (UID 1000) that downstream Dockerfiles switch to.
# Downstream Dockerfiles install their own deps via uv into /opt/venv during the builder stage.

ARG PYTHON_VERSION=3.13

FROM python:${PYTHON_VERSION}-slim
ARG VERSION=dev
LABEL org.opencontainers.image.source="https://github.com/Cloud-SPE/livepeer-modules-openai-runners" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}"

ENV DEBIAN_FRONTEND=noninteractive \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    VIRTUAL_ENV=/opt/venv \
    PATH=/opt/venv/bin:$PATH \
    UV_LINK_MODE=copy \
    UV_PYTHON=/usr/local/bin/python

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /usr/local/bin/

RUN groupadd -g 1000 runner \
    && useradd -u 1000 -g runner -s /usr/sbin/nologin -m -d /home/runner runner \
    && uv venv "$VIRTUAL_ENV" \
    && chown -R runner:runner "$VIRTUAL_ENV"

EXPOSE 8080
