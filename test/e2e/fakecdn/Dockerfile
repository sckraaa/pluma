# fakecdn: the Gcore CDN resource of the HTTPS CDN for the end-to-end tests
# (test/e2e/fakecdn). Standard library only, so the build needs no module
# downloads. The builder matches the main Dockerfile (go.mod toolchain).
FROM golang:1.27.4-trixie@sha256:b54b2639de8c1bfe538ec5ffc45f29b1932235ac5f6d63c059853646677112d9 AS builder
WORKDIR /src
COPY go.mod ./
COPY test/e2e/fakecdn ./test/e2e/fakecdn
RUN CGO_ENABLED=0 go build -mod=mod -trimpath -ldflags="-s -w" -o /fakecdn ./test/e2e/fakecdn

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=builder /fakecdn /fakecdn
USER nonroot
# 8080: the CDN, for the clients of the stand; 8081: the control API, for
# the tests only.
EXPOSE 8080 8081
ENTRYPOINT ["/fakecdn"]
