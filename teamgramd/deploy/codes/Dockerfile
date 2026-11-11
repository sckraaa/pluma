# The login code sender (app/ops/codes, task 073, ADR 0044). Its own image: it
# links TDLib, which nothing else needs.
#
# TDLib is built from the commit below (official tdlib/td, whose td_api.tl was
# read for this service; license BSL-1.0). It compiles for a long time and
# needs several GB of memory; the layer is cached until this commit changes.
FROM golang:1.27.4-trixie@sha256:b54b2639de8c1bfe538ec5ffc45f29b1932235ac5f6d63c059853646677112d9 AS tdlib
ARG TD_COMMIT=c0a540074fa449094340f76a3ea56f7096e0ad4c
RUN apt-get update -y \
 && apt-get install -y --no-install-recommends cmake gperf libssl-dev zlib1g-dev \
 && git init /td && cd /td \
 && git fetch --depth 1 https://github.com/tdlib/td.git "$TD_COMMIT" \
 && git checkout --detach FETCH_HEAD \
 && cmake -S . -B build -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX=/usr/local \
 && cmake --build build --target install -j2 \
 && rm -rf /td

FROM golang:1.27.4-trixie@sha256:b54b2639de8c1bfe538ec5ffc45f29b1932235ac5f6d63c059853646677112d9 AS builder
RUN apt-get update -y && apt-get install -y --no-install-recommends libssl-dev zlib1g-dev
COPY --from=tdlib /usr/local/ /usr/local/
RUN ldconfig
WORKDIR /src
COPY . .
RUN CGO_ENABLED=1 go build -mod=mod -trimpath -ldflags="-s -w" -tags tdlib -o /codes ./app/ops/codes

# The runtime glibc must not be older than the builder's, as in the main Dockerfile.
FROM ubuntu:26.04@sha256:c098726dd9b9733f3e885f8de0ef375206a311b0a317eadc40d347b1115fb944
RUN apt-get update -y \
 && apt-get install -y --no-install-recommends libssl3t64 zlib1g libstdc++6 ca-certificates \
 && apt-get clean && rm -rf /var/lib/apt/lists/* \
 && useradd -u 65532 -M -s /usr/sbin/nologin nonroot \
 && install -d -o nonroot -g nonroot -m 700 /data
COPY --from=tdlib /usr/local/lib/libtdjson.so* /usr/local/lib/
RUN ldconfig
COPY --from=builder /codes /app/codes
USER nonroot
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD ["/app/codes", "healthcheck"]
ENTRYPOINT ["/app/codes"]
