# fakecodes: the login code sender for the end-to-end tests (test/e2e/fakecodes).
# Standard library only, so the build needs no module downloads. The builder
# matches the main Dockerfile (go.mod toolchain).
FROM golang:1.27.4-trixie@sha256:b54b2639de8c1bfe538ec5ffc45f29b1932235ac5f6d63c059853646677112d9 AS builder
WORKDIR /src
COPY go.mod ./
COPY test/e2e/fakecodes ./test/e2e/fakecodes
RUN CGO_ENABLED=0 go build -mod=mod -trimpath -ldflags="-s -w" -o /fakecodes ./test/e2e/fakecodes

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=builder /fakecodes /fakecodes
USER nonroot
EXPOSE 8080
ENTRYPOINT ["/fakecodes"]
