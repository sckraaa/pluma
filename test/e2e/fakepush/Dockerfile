# fakepush: APNs and FCM for the end-to-end tests (test/e2e/fakepush).
# Standard library only, so the build needs no module downloads. The builder
# matches the main Dockerfile (go.mod toolchain).
FROM golang:1.27.3-trixie@sha256:fbb7ae40b62ff8afe31662df570d39b4f5052ac32b90dae8e60d7657a20a74f4 AS builder
WORKDIR /src
COPY go.mod ./
COPY test/e2e/fakepush ./test/e2e/fakepush
RUN CGO_ENABLED=0 go build -mod=mod -trimpath -ldflags="-s -w" -o /fakepush ./test/e2e/fakepush

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=builder /fakepush /fakepush
USER nonroot
EXPOSE 8080
ENTRYPOINT ["/fakepush"]
