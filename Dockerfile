# Images are pinned by tag and digest for reproducible builds. The builder's Go
# version must match the toolchain directive in go.mod (the image sets
# GOTOOLCHAIN=local, so a mismatch fails the build instead of downloading).
# The runtime glibc must not be older than the builder's: dfs links webp via cgo.
FROM golang:1.27.4-trixie@sha256:f18c3d73a184fb0582828510722e723b63a93f7188d11dd223d9be6f6880e65d AS builder
# The generated github.com/teamgram/proto/mtproto package is huge: with the
# default backend concurrency its compile peaks at ~5.3 GB RSS (Go 1.27.3),
# with -c=1 at ~4.3 GB, which keeps the image build within a 6 GB build VM.
ENV GOFLAGS=-gcflags=github.com/teamgram/proto/mtproto=-c=1
WORKDIR /app
COPY . .
# The module and build caches persist between builds on the same machine
# (BuildKit cache mounts, not part of the image): a rebuild after a small change
# recompiles only what changed instead of the whole tree.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    ./build.sh

FROM ubuntu:26.04@sha256:c098726dd9b9733f3e885f8de0ef375206a311b0a317eadc40d347b1115fb944
RUN apt-get update -y && apt-get install -y ffmpeg && apt-get clean && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /app/teamgramd/bin/ /app/bin/
COPY --from=builder /app/teamgramd/etc2/ /app/etc2/
# Schema migrations, applied by the migrate service (pluma-admin migrate up).
COPY --from=builder /app/teamgramd/deploy/sql/ /app/sql/
# One image for all services: docker-compose.yaml runs each binary as its own
# service (exec form, so SIGTERM reaches it).
WORKDIR /app/bin
