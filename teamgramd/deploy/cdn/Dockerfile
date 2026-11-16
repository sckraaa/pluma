# A CDN node of Pluma (app/service/cdn, task 102, ADR 0065): its own image,
# run on the edge VPS next to the SFU. Pure Go, no cgo. The builder matches
# the main Dockerfile (go.mod toolchain).
FROM golang:1.27.4-trixie@sha256:b54b2639de8c1bfe538ec5ffc45f29b1932235ac5f6d63c059853646677112d9 AS builder
WORKDIR /src
COPY . .
# The module and build caches persist between builds on the same machine
# (BuildKit cache mounts, not part of the image).
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /cdn ./app/service/cdn/cmd/cdn \
    && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=builder /cdn /cdn
COPY app/service/cdn/etc/cdn.yaml /etc/cdn/cdn.yaml
# The data directory belongs to nonroot (uid 65532): a new named volume
# takes its owner from here; a bind mount must be chowned on the host.
COPY --from=builder --chown=65532:65532 /out/data /var/lib/cdn
USER nonroot
# 8443 clients (MTProto), 20510 control (gRPC, mutual TLS), 20511 metrics.
EXPOSE 8443 20510 20511
VOLUME ["/var/lib/cdn"]
ENTRYPOINT ["/cdn", "-f", "/etc/cdn/cdn.yaml"]
