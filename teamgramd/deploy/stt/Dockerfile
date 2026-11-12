# The speech recognition service (app/ops/stt, task 090, ADR 0056,
# docs/ops/transcription.md). Its own image: it links sherpa-onnx and holds
# the speech model, which nothing else needs.
#
# The model is baked in: the build downloads one pinned archive and checks its
# SHA-256, so a start never depends on the network and a deployment is one
# image. Another model is other build arguments (docs/ops/transcription.md,
# "Замена модели"):
#
#   MODEL_URL, MODEL_SHA256  the archive, a .tar.bz2 with one directory
#   MODEL_KIND               nemo_transducer, nemo_ctc or whisper
#   MODEL_NAME               the name the service answers to (STT_MODEL of bff)
#   MODEL_LANGUAGE           the language of the speech, for Whisper
#
# The default is GigaAM-v3 e2e_rnnt (punctuation and normalization), int8,
# as exported by sherpa-onnx; MIT, its LICENSE stays in /model.
FROM ubuntu:26.04@sha256:c098726dd9b9733f3e885f8de0ef375206a311b0a317eadc40d347b1115fb944 AS model
ARG MODEL_URL=https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/sherpa-onnx-nemo-transducer-punct-giga-am-v3-russian-2025-12-16.tar.bz2
ARG MODEL_SHA256=f9620a0099019c6afcee26525ef9ed3297fa50dd5691c1902af0c948fc1a470b
ARG MODEL_KIND=nemo_transducer
ARG MODEL_NAME=gigaam-v3-e2e-rnnt
ARG MODEL_LANGUAGE=ru
# Silero VAD cuts the speech into the pieces a model takes; MIT
# (github.com/snakers4/silero-vad).
ARG VAD_URL=https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/silero_vad.onnx
ARG VAD_SHA256=9e2449e1087496d8d4caba907f23e0bd3f78d91fa552479bb9c23ac09cbb1fd6
RUN apt-get update -y \
 && apt-get install -y --no-install-recommends ca-certificates curl bzip2 \
 && mkdir /model \
 && curl -fsSL --retry 3 -o /tmp/model.tar.bz2 "$MODEL_URL" \
 && echo "$MODEL_SHA256  /tmp/model.tar.bz2" | sha256sum -c - \
 && tar -xjf /tmp/model.tar.bz2 -C /model --strip-components=1 \
 && find /model -mindepth 1 \( -name '*.py' -o -name '*.sh' -o -name 'test_wavs' \) -prune -exec rm -rf {} + \
 && curl -fsSL --retry 3 -o /model/silero_vad.onnx "$VAD_URL" \
 && echo "$VAD_SHA256  /model/silero_vad.onnx" | sha256sum -c - \
 && printf 'name: %s\nkind: %s\nlanguage: "%s"\n' "$MODEL_NAME" "$MODEL_KIND" "$MODEL_LANGUAGE" > /model/model.yaml \
 && chmod -R a+rX /model \
 && rm /tmp/model.tar.bz2

FROM golang:1.27.4-trixie@sha256:b54b2639de8c1bfe538ec5ffc45f29b1932235ac5f6d63c059853646677112d9 AS builder
WORKDIR /src
COPY . .
# The shared libraries of sherpa-onnx and onnxruntime come with the Go module
# (github.com/k2-fsa/sherpa-onnx-go-linux, Apache-2.0; onnxruntime is MIT):
# the version of go.mod is the version of the libraries. Their licenses and
# that of Silero VAD go into /licenses.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 go build -mod=mod -trimpath -ldflags="-s -w" -tags sherpa -o /stt ./app/ops/stt \
 && dir=$(go list -mod=mod -m -f '{{.Dir}}' github.com/k2-fsa/sherpa-onnx-go-linux) \
 && mkdir /sherpa \
 && cp "$dir/lib/x86_64-unknown-linux-gnu/libsherpa-onnx-c-api.so" "$dir/lib/x86_64-unknown-linux-gnu/libonnxruntime.so" /sherpa/ \
 && mkdir /licenses \
 && cp "$dir/LICENSE" /licenses/sherpa-onnx.LICENSE \
 && cp teamgramd/deploy/stt/licenses/* /licenses/

# The runtime glibc must not be older than the builder's, as in the main Dockerfile.
FROM ubuntu:26.04@sha256:c098726dd9b9733f3e885f8de0ef375206a311b0a317eadc40d347b1115fb944
RUN apt-get update -y \
 && apt-get install -y --no-install-recommends libstdc++6 \
 && apt-get clean && rm -rf /var/lib/apt/lists/* \
 && useradd -u 65532 -M -s /usr/sbin/nologin nonroot
COPY --from=builder /sherpa/*.so /usr/local/lib/
RUN ldconfig
COPY --from=builder /licenses/ /licenses/
COPY --from=model /model/ /model/
COPY --from=builder /stt /app/stt
USER nonroot
EXPOSE 9379
HEALTHCHECK --interval=30s --timeout=5s --start-period=60s --retries=3 CMD ["/app/stt", "healthcheck"]
ENTRYPOINT ["/app/stt"]
