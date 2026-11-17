# The translation engine (task 101, ADR 0064, docs/ops/translation.md):
# llama-server of llama.cpp with one translation model, behind
# POST /v1/chat/completions, which bff calls (pkg/translate).
#
# The model is baked in: the build downloads one pinned GGUF file and checks
# its SHA-256, so a start never depends on the network and a deployment is
# one image. Another model or quantization is other build arguments
# (docs/ops/translation.md, "Замена модели"):
#
#   MODEL_URL, MODEL_SHA256      the GGUF file
#   LICENSE_URL, LICENSE_SHA256  the license of the model, shipped with it
#   MODEL_NAME                   the name the engine answers to (TRANSLATE_MODEL of bff)
#
# The default is HY-MT1.5-1.8B (Tencent) Q8_0, the official GGUF of
# huggingface.co/tencent/HY-MT1.5-1.8B-GGUF at revision 265b2e61; the
# quantization is chosen by measurement (docs/ops/translation.md, "Замер").
# Q6_K of the same revision, the fallback if Q8_0 is too slow:
#   MODEL_URL=.../HY-MT1.5-1.8B-Q6_K.gguf
#   MODEL_SHA256=c3819200ab9a79cb29b9a05ce8920e2eb01ae7ce520094fc5b57356494f3c641
#   MODEL_NAME=hy-mt1.5-1.8b-q6_k
FROM ubuntu:26.04@sha256:c098726dd9b9733f3e885f8de0ef375206a311b0a317eadc40d347b1115fb944 AS model
ARG MODEL_URL=https://huggingface.co/tencent/HY-MT1.5-1.8B-GGUF/resolve/265b2e615a7dc9b06c435dc878829ad99a512ba2/HY-MT1.5-1.8B-Q8_0.gguf
ARG MODEL_SHA256=6789b06d0902f2f5312c0e1703d56ccbddfcfb6c653d22519b7c720f7db9a98e
ARG LICENSE_URL=https://huggingface.co/tencent/HY-MT1.5-1.8B-GGUF/resolve/265b2e615a7dc9b06c435dc878829ad99a512ba2/License.txt
ARG LICENSE_SHA256=d7d9db858500ac9073f4b5decef8e208454357226f535f65079ce4376047569f
ARG MODEL_NAME=hy-mt1.5-1.8b-q8_0
RUN apt-get update -y \
 && apt-get install -y --no-install-recommends ca-certificates curl \
 && mkdir /model \
 && curl -fsSL --retry 3 -o /model/model.gguf "$MODEL_URL" \
 && echo "$MODEL_SHA256  /model/model.gguf" | sha256sum -c - \
 && curl -fsSL --retry 3 -o /model/License.txt "$LICENSE_URL" \
 && echo "$LICENSE_SHA256  /model/License.txt" | sha256sum -c - \
 && case "$MODEL_NAME" in *[!A-Za-z0-9._-]*|'') echo "MODEL_NAME: letters, digits, '.', '_' and '-' only" >&2; exit 1;; esac \
 && printf '%s\n' "$MODEL_NAME" > /model/name \
 && chmod -R a+rX /model

# llama-server of the official server image of llama.cpp (MIT), CPU build:
# it carries every CPU variant of ggml and loads the best one for the
# processor it runs on (GGML_CPU_ALL_VARIANTS, .devops/cpu.Dockerfile of the
# release). Release b11913 of 2026-10-27; HY-MT1.5 is the "hunyuan-dense"
# architecture of llama.cpp. Only /app is taken: that image also carries
# ffmpeg (about 780 MB), which the server does not link.
FROM ghcr.io/ggml-org/llama.cpp:server-b11913@sha256:8ee4ec7ce58a921313d07390c6699af13aa5ec083f3cdf67e5f4155767047373 AS llama

# The libraries /app links (ldd: libgomp, libssl, libstdc++) and curl for the
# health check. The binaries are built on Ubuntu 24.04; this glibc is newer.
FROM ubuntu:26.04@sha256:c098726dd9b9733f3e885f8de0ef375206a311b0a317eadc40d347b1115fb944
RUN apt-get update -y \
 && apt-get install -y --no-install-recommends libgomp1 libssl3t64 libstdc++6 curl \
 && apt-get clean && rm -rf /var/lib/apt/lists/* \
 && useradd -u 65532 -M -s /usr/sbin/nologin nonroot
COPY --from=llama /app/ /app/
# The libraries of llama.cpp are next to its binaries, which carry no search
# path for them; the official image finds them only from its working
# directory.
RUN echo /app > /etc/ld.so.conf.d/llama.cpp.conf && ldconfig
COPY teamgramd/deploy/translate/licenses/ /licenses/
COPY teamgramd/deploy/translate/NOTICE /licenses/NOTICE
COPY --from=model /model/ /model/
COPY --chmod=0755 teamgramd/deploy/translate/entrypoint.sh /app/translate-entrypoint.sh
USER nonroot
EXPOSE 8080
# /health answers 503 while the model loads and 200 once it serves; it needs
# no token (tools/server/server-http.cpp of the release, public endpoints).
HEALTHCHECK --interval=30s --timeout=5s --start-period=120s --retries=3 CMD ["curl", "-fsS", "-o", "/dev/null", "http://127.0.0.1:8080/health"]
ENTRYPOINT ["/app/translate-entrypoint.sh"]
