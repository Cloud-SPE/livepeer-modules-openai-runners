# openai-chat-runner — streaming chat-completions proxy (Go).
# Build context: repo root. Multi-arch (amd64 + arm64).

ARG GO_VERSION=1.25.7
ARG ALPINE_VERSION=3.20

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
COPY openai-chat-runner/go.mod ./
COPY openai-chat-runner/ ./
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /bin/chat-runner ./cmd/runner

FROM alpine:${ALPINE_VERSION}
RUN apk add --no-cache curl ca-certificates \
    && adduser -D runner
USER runner
ENV RUNNER_ADDR=:8080 \
    CAPABILITY_NAME=openai-chat-completions \
    USAGE_FIELD=total_tokens
EXPOSE 8080
COPY --from=build /bin/chat-runner /runner
HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
    CMD curl -fsS http://localhost:8080/healthz || exit 1
ENTRYPOINT ["/runner"]
