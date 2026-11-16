#!/bin/sh
# Starts llama-server with the model of the image and the settings of the
# environment (docker-compose.translate.yaml, docs/ops/translation.md). The
# flags are those of llama.cpp release b11913 (common/arg.cpp).
#
#   TRANSLATE_THREADS    CPU threads of generation and of prompt processing (6)
#   TRANSLATE_SLOTS      requests served at once, --parallel (4); bff's
#                        TRANSLATE_SLOTS must be the same
#   TRANSLATE_CTX        context of the whole engine in tokens (16384), split
#                        evenly between the slots; bff's TRANSLATE_CTX must be
#                        the same
#   TRANSLATE_API_TOKEN  the bearer token bff sends, at least 32 characters;
#                        none if empty
set -eu

fail() {
	echo "translate: $*" >&2
	exit 64
}

# number NAME VALUE MIN MAX: VALUE is a whole number from MIN to MAX.
number() {
	case "$2" in
	'' | *[!0-9]*) fail "$1 must be a whole number, got '$2'" ;;
	esac
	if [ "$2" -lt "$3" ] || [ "$2" -gt "$4" ]; then
		fail "$1 must be from $3 to $4, got $2"
	fi
}

threads=${TRANSLATE_THREADS:-6}
slots=${TRANSLATE_SLOTS:-4}
ctx=${TRANSLATE_CTX:-16384}
number TRANSLATE_THREADS "$threads" 1 64
number TRANSLATE_SLOTS "$slots" 1 16
number TRANSLATE_CTX "$ctx" 512 262144
# The same bound as bff's (pkg/translate.Config.Validate): a slot holds the
# prompt of an item and its answer.
if [ $((ctx / slots)) -lt 512 ]; then
	fail "TRANSLATE_CTX must give each of the TRANSLATE_SLOTS at least 512 tokens"
fi

# The token goes to llama-server in its environment (LLAMA_API_KEY), not on
# the command line, where any process of the host could read it.
unset LLAMA_API_KEY
token=${TRANSLATE_API_TOKEN:-}
unset TRANSLATE_API_TOKEN
if [ -n "$token" ]; then
	if [ "${#token}" -lt 32 ]; then
		fail "TRANSLATE_API_TOKEN must be at least 32 characters (openssl rand -hex 32)"
	fi
	# llama-server splits LLAMA_API_KEY at commas into several keys
	# (common/arg.cpp, --api-key), so a token with one would never match
	# what bff sends.
	case "$token" in
	*,*) fail "TRANSLATE_API_TOKEN must not contain a comma (openssl rand -hex 32)" ;;
	esac
	export LLAMA_API_KEY="$token"
fi

# --no-kv-unified: each slot has its own TRANSLATE_CTX / TRANSLATE_SLOTS
#   tokens, the context bff bounds an answer by (pkg/translate.Config.SlotContext).
# --load-mode none: the model is read into memory rather than mapped, so the
#   memory limit of the container holds all of it and it is never dropped and
#   read again under pressure.
# --cache-ram 0: no cache of prompts; every request is another text, and the
#   cache would keep message texts in memory after their answer.
# --no-slots: GET /slots would show what the slots are working on.
# --no-webui: only the API is served.
# --fit off: the context is the one configured, not one fitted to free memory.
# --metrics: GET /metrics for Prometheus (it needs the token as well).
exec /app/llama-server \
	--model /model/model.gguf \
	--alias "$(cat /model/name)" \
	--host 0.0.0.0 \
	--port 8080 \
	--threads "$threads" \
	--threads-batch "$threads" \
	--ctx-size "$ctx" \
	--parallel "$slots" \
	--no-kv-unified \
	--load-mode none \
	--cache-ram 0 \
	--no-slots \
	--no-webui \
	--fit off \
	--metrics
