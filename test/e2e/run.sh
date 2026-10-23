#!/usr/bin/env bash
# End-to-end test of the full stack (docs/ops/testing.md).
#
#   test/e2e/run.sh all        up, test, logs on failure, down (what `make e2e` runs)
#   test/e2e/run.sh up         generate throwaway secrets, build the image, start the stack
#   test/e2e/run.sh test       run the Go e2e tests against the running stack
#   test/e2e/run.sh logs       save service logs to $E2E_DIR/logs
#   test/e2e/run.sh down       stop the stack, delete its volumes and the secrets
#   test/e2e/run.sh admin ...  run tools/pluma-admin inside the teamgram container
#   test/e2e/run.sh compose .. any docker compose command on the test project
#
# Environment:
#   E2E_DIR      work directory for the generated secrets and logs
#                (default .e2e in the repository, which git and the Docker
#                build context ignore; it must be under a directory Docker can
#                bind-mount, e.g. the home directory with colima)
#   E2E_PROJECT  compose project name (default pluma-e2e)
#   E2E_KEEP=1   `all` leaves the stack running after the tests
#   E2E_GOTEST_FLAGS  extra flags for go test (e.g. -run TestX)
#
# The secrets (passwords, the server RSA key) are generated for one run and
# deleted by `down`; they are never written anywhere git tracks.

set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
E2E_DIR=${E2E_DIR:-$ROOT/.e2e}
E2E_PROJECT=${E2E_PROJECT:-pluma-e2e}
ENV_FILE=$E2E_DIR/env
KEY_FILE=$E2E_DIR/server_pkcs1.key
LOG_DIR=$E2E_DIR/logs

# Services the tests need; everything else in docker-compose-env.yaml
# (Elasticsearch, Kibana, filebeat, go-stash, metrics) is not started.
SERVICES=(mysql redis kafka kafka-init etcd minio minio-mc jaeger teamgram)

# Kafka consumer groups of msg and sync and the topic each reads, as
# group:topic (InboxConsumer in teamgramd/etc2/msg.yaml, SyncConsumer in
# teamgramd/etc2/sync.yaml).
CONSUMER_GROUPS=(Inbox-MainCommunity-S:Inbox-T Sync-MainCommunity-S:Sync-T)
CONSUMERS_TIMEOUT=120

compose() {
	docker compose \
		--project-name "$E2E_PROJECT" \
		--project-directory "$ROOT" \
		--env-file "$ENV_FILE" \
		-f "$ROOT/docker-compose-env.yaml" \
		-f "$ROOT/docker-compose.yaml" \
		-f "$ROOT/test/e2e/docker-compose.e2e.yaml" \
		"$@"
}

log() { printf '[e2e %s] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }

secret() { openssl rand -hex "$1"; }

# The steps below run where `set -e` does not apply (cmd_all calls them in
# `||` lists), so every step that can fail returns explicitly.

# gen_secrets writes the env file and the server RSA key for this run.
gen_secrets() {
	if [[ -f $ENV_FILE && -f $KEY_FILE ]]; then
		return
	fi
	rm -f "$ENV_FILE" "$KEY_FILE"
	mkdir -p "$E2E_DIR" && chmod 700 "$E2E_DIR" || return
	local fp
	fp=$(cd "$ROOT" && go run ./tools/genserverkey -out "$KEY_FILE" |
		sed -n 's/^ *KeyFingerprint ([^)]*): *\([0-9][0-9]*\)$/\1/p')
	if [[ -z $fp ]]; then
		log "genserverkey printed no fingerprint"
		return 1
	fi
	(
		umask 077
		cat >"$ENV_FILE" <<EOF
MYSQL_ROOT_PASSWORD=$(secret 24)
MYSQL_PASSWORD=$(secret 24)
REDIS_PASSWORD=$(secret 24)
MINIO_ROOT_USER=e2e$(secret 4)
MINIO_ROOT_PASSWORD=$(secret 24)
GRAFANA_ADMIN_PASSWORD=$(secret 24)
LOGIN_CODE_HMAC_KEY=$(secret 32)
SERVER_RSA_KEY_FILE=$KEY_FILE
SERVER_RSA_KEY_FINGERPRINT=$fp
EOF
	)
}

cmd_up() {
	gen_secrets || return
	log "building the teamgram image"
	compose build teamgram || return
	log "starting ${SERVICES[*]}"
	compose up --detach --wait --wait-timeout 300 "${SERVICES[@]}" || return
	wait_consumers || return
	log "stack is up; gateway at $(gateway_addr)"
}

# wait_consumers waits until the Kafka consumer groups of msg and sync own
# every partition of their topics. The container is healthy once the processes
# run and the gateway listens, but a group gets its partitions a little later.
# Nothing is lost meanwhile (the groups start from the oldest offset, the
# events wait in mq_outbox), but the first pushes of a test would come late.
wait_consumers() {
	local deadline=$((SECONDS + CONSUMERS_TIMEOUT)) entry group topic out rows owned
	for entry in "${CONSUMER_GROUPS[@]}"; do
		group=${entry%%:*}
		topic=${entry#*:}
		while :; do
			# Rows: GROUP TOPIC PARTITION CURRENT-OFFSET LOG-END-OFFSET LAG
			# CONSUMER-ID HOST CLIENT-ID; CONSUMER-ID is "-" for a partition
			# nobody owns. A group that is rebalancing says so instead.
			out=$(compose exec -T kafka /opt/kafka/bin/kafka-consumer-groups.sh \
				--bootstrap-server localhost:9094 --describe --group "$group" 2>&1) || true
			rows=$(awk -v t="$topic" '$2 == t { n++ } END { print n + 0 }' <<<"$out")
			owned=$(awk -v t="$topic" '$2 == t && $7 != "-" { n++ } END { print n + 0 }' <<<"$out")
			if [[ $rows -gt 0 && $owned == "$rows" && $out != *rebalancing* ]]; then
				log "consumer group $group owns all $rows partitions of $topic"
				break
			fi
			if ((SECONDS >= deadline)); then
				log "consumer group $group does not own the partitions of $topic after ${CONSUMERS_TIMEOUT}s:"
				printf '%s\n' "$out" >&2
				return 1
			fi
			sleep 1
		done
	done
}

gateway_addr() {
	compose port teamgram 10443
}

cmd_test() {
	local addr
	addr=$(gateway_addr) || return
	log "running the e2e tests against $addr"
	(
		cd "$ROOT"
		# shellcheck disable=SC2086 # E2E_GOTEST_FLAGS is a list of flags
		PLUMA_E2E_ADDR=$addr \
			PLUMA_E2E_SERVER_KEY=$KEY_FILE \
			PLUMA_E2E_RUN="$ROOT/test/e2e/run.sh" \
			go test -tags e2e -count=1 -timeout 10m -v ${E2E_GOTEST_FLAGS:-} ./test/e2e/...
	)
}

cmd_logs() {
	mkdir -p "$LOG_DIR"
	compose ps --all >"$LOG_DIR/ps.txt" 2>&1 || true
	local s
	for s in "${SERVICES[@]}"; do
		compose logs --no-color --timestamps "$s" >"$LOG_DIR/$s.log" 2>&1 || true
	done
	log "service logs saved to $LOG_DIR"
}

cmd_down() {
	if [[ -f $ENV_FILE ]]; then
		compose down --volumes --remove-orphans --timeout 20 || true
	fi
	rm -f "$ENV_FILE" "$KEY_FILE"
	rmdir "$E2E_DIR" 2>/dev/null || true # kept while it holds logs
}

cmd_all() {
	local start status=0
	start=$(date +%s)
	trap 'cmd_down' EXIT
	if [[ ${E2E_KEEP:-} == 1 ]]; then
		trap - EXIT
	fi
	cmd_up || status=$?
	if [[ $status == 0 ]]; then
		cmd_test || status=$?
	fi
	if [[ $status != 0 ]]; then
		cmd_logs
	fi
	log "finished in $(($(date +%s) - start))s with status $status"
	return "$status"
}

case ${1:-} in
up) cmd_up ;;
test) cmd_test ;;
logs) cmd_logs ;;
down) cmd_down ;;
all) cmd_all ;;
admin)
	shift
	compose exec -T teamgram /app/bin/pluma-admin -f /app/etc2/bff.yaml "$@"
	;;
compose)
	shift
	compose "$@"
	;;
*)
	sed -n '2,22p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
	exit 2
	;;
esac
