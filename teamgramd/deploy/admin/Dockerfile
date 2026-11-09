# The moderation panel (app/ops/admin, task 025, ADR 0040). A small static
# binary; the builder matches the main Dockerfile (go.mod toolchain).
FROM golang:1.27.3-trixie@sha256:fbb7ae40b62ff8afe31662df570d39b4f5052ac32b90dae8e60d7657a20a74f4 AS builder
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -mod=mod -trimpath -ldflags="-s -w" -o /admin ./app/ops/admin

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
WORKDIR /app
COPY --from=builder /admin /app/admin
COPY teamgramd/etc/admin.yaml /app/etc/admin.yaml
USER nonroot
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD ["/app/admin", "healthcheck"]
ENTRYPOINT ["/app/admin", "-f", "/app/etc/admin.yaml"]
