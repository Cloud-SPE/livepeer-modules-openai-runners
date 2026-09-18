# Shared CUDA 13 + Python base image.
# Provides: CUDA 13 runtime, Python 3.13 (via uv-managed standalone build), uv,
# an empty /opt/venv, common apt essentials, and a non-root `runner` user
# (UID 1000) that downstream Dockerfiles switch to.
# Downstream Dockerfiles install PyTorch + their own deps in the builder stage.

ARG CUDA_VERSION=13.2.1
ARG UBUNTU_VERSION=24.04
ARG PYTHON_VERSION=3.13

FROM nvidia/cuda:${CUDA_VERSION}-runtime-ubuntu${UBUNTU_VERSION}
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
    UV_PYTHON_INSTALL_DIR=/opt/python

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /usr/local/bin/

# Ubuntu 24.04 ships a default `ubuntu` user at UID 1000; remove it before
# creating our own. Install Python 3.13 standalone via uv into /opt/python.
ARG PYTHON_VERSION
RUN (userdel -r ubuntu 2>/dev/null || true) \
    && groupadd -g 1000 runner \
    && useradd -u 1000 -g runner -s /usr/sbin/nologin -m -d /home/runner runner \
    && uv python install ${PYTHON_VERSION} \
    && uv venv --python ${PYTHON_VERSION} "$VIRTUAL_ENV" \
    && chown -R runner:runner "$VIRTUAL_ENV" /opt/python

EXPOSE 8080
