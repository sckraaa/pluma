# The SFU of group calls (app/service/sfu, task 093, ADR 0059): its own image,
# so that it can run on another machine than the core (ADR 0057 §4). Pure Go
# (pion), no cgo. The builder matches the main Dockerfile (go.mod toolchain).
FROM golang:1.27.4-trixie@sha256:b54b2639de8c1bfe538ec5ffc45f29b1932235ac5f6d63c059853646677112d9 AS builder
WORKDIR /src
COPY . .
# The module and build caches persist between builds on the same machine
# (BuildKit cache mounts, not part of the image).
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /sfu ./app/service/sfu/cmd/sfu

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=builder /sfu /sfu
COPY app/service/sfu/etc/sfu.yaml /etc/sfu/sfu.yaml
USER nonroot
# 20500 control (gRPC, mutual TLS), 20501 metrics, 20600/udp media,
# 20601/tcp ICE-TCP. The media ports are published with the host network.
EXPOSE 20500 20501 20600/udp 20601
ENTRYPOINT ["/sfu", "-f", "/etc/sfu/sfu.yaml"]
