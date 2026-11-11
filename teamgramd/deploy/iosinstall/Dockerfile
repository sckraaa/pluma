# iosinstall: the iPhone app from the site (app/ops/iosinstall, task 029),
# with rcodesign for re-signing. Standard library only, so the build needs
# no module downloads. The builder matches the main Dockerfile (go.mod
# toolchain).
FROM golang:1.27.4-trixie@sha256:b54b2639de8c1bfe538ec5ffc45f29b1932235ac5f6d63c059853646677112d9 AS builder
WORKDIR /src
COPY go.mod ./
COPY app/ops/iosinstall ./app/ops/iosinstall
RUN CGO_ENABLED=0 go build -mod=mod -trimpath -ldflags="-s -w" -o /iosinstall ./app/ops/iosinstall
# rcodesign 0.29.0 (indygreg/apple-platform-rs, release apple-codesign/0.29.0),
# a static musl build; its SHA-256 is the release's .sha256.
ARG RCODESIGN_URL=https://github.com/indygreg/apple-platform-rs/releases/download/apple-codesign/0.29.0/apple-codesign-0.29.0-x86_64-unknown-linux-musl.tar.gz
ARG RCODESIGN_SHA256=dbe85cedd8ee4217b64e9a0e4c2aef92ab8bcaaa41f20bde99781ff02e600002
RUN curl -fsSL -o /tmp/rcodesign.tgz "$RCODESIGN_URL" \
	&& echo "$RCODESIGN_SHA256  /tmp/rcodesign.tgz" | sha256sum -c - \
	&& tar -xzf /tmp/rcodesign.tgz -C /tmp --strip-components=1 \
	&& install -m 0755 /tmp/rcodesign /rcodesign \
	&& mkdir -p /data

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=builder /iosinstall /iosinstall
COPY --from=builder /rcodesign /usr/local/bin/rcodesign
# The volume takes the owner of the image's directory on its first use.
COPY --from=builder --chown=65532:65532 /data /data
USER nonroot
EXPOSE 9098
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD ["/iosinstall", "healthcheck"]
ENTRYPOINT ["/iosinstall"]
