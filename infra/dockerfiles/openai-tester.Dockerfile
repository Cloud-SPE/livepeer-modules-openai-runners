# openai-tester — Node integration smoke harness.
# Build context: repo root. Two-stage: install deps in build, copy to a lean runtime.

ARG NODE_VERSION=22

FROM node:${NODE_VERSION}-alpine AS build
WORKDIR /app
COPY openai-tester/package.json openai-tester/package-lock.json ./
RUN npm ci --omit=dev
COPY openai-tester/test-*.mjs openai-tester/generate-test-audio.sh openai-tester/test.ogg ./

FROM node:${NODE_VERSION}-alpine
WORKDIR /app
RUN apk add --no-cache ffmpeg \
    && adduser -D runner
USER runner
COPY --from=build --chown=runner /app /app
ENV OPENAI_BASE_URL=http://localhost:8090/v1
# OPENAI_API_KEY must be provided at run time (-e OPENAI_API_KEY=... or --env-file).
# See infra/env/openai-tester.env.example. The broker ignores the value;
# `local-dev-no-auth` is a fine placeholder for local smoke against the runners.
CMD ["node", "--test", "test-chat-completion.mjs"]
