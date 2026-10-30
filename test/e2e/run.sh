#!/usr/bin/env bash
# End-to-end test of the full stack (docs/ops/testing.md).
#
#   test/e2e/run.sh all        up, test, logs on failure, down (what `make e2e` runs)
#   test/e2e/run.sh up         generate throwaway secrets, build the image, start the stack
#   test/e2e/run.sh test       run the Go e2e and conformance tests against the running stack
#   test/e2e/run.sh wait       start stopped services, wait until the stack serves
#   test/e2e/run.sh logs       save service logs to $E2E_DIR/logs
#   test/e2e/run.sh down       stop the stack, delete its volumes and the secrets
#   test/e2e/run.sh admin ...  run tools/pluma-admin inside the bff container
#   test/e2e/run.sh lan [ADDR] make the gateway reachable at ADDR:10443 (default:
#                              this machine's address on its default route) for a
#                              phone on the same network, until Ctrl-C
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
#   E2E_GATEWAY_PORT  the host port of the gateway's TCP transport on
#                127.0.0.1 (default: an ephemeral one)
#   E2E_GATEWAY_WS_PORT  the same for its WebSocket transport
#   E2E_SERVER_KEY    a server private key to use instead of a new one, e.g.
#                the development key of the client repositories: their
#                development builds can then sign in to the stand
#                (docs/ops/clients.md)
#   E2E_FCM_CREDENTIALS  a real FCM service account key: the stand pushes to
#                Google instead of test/e2e/fakepush (a phone or an emulator
#                with Google Play services gets the notifications)
#   E2E_APNS_KEY, E2E_APNS_KEY_ID, E2E_APNS_TEAM_ID  the same for APNs
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
# Push providers of the stand (task 009): throwaway keys and the config of
# the push service pointing at test/e2e/fakepush, or the real keys.
APNS_KEY_FILE=$E2E_DIR/apns_key.p8
FCM_CREDENTIALS_FILE=$E2E_DIR/fcm_credentials.json
PUSH_CONFIG=$E2E_DIR/push.yaml
FAKEPUSH_URL=http://fakepush:8080

# Teamgram services (docker-compose.yaml) and the infrastructure the tests
# need; everything else in docker-compose-env.yaml (Elasticsearch, Kibana,
# filebeat, go-stash, metrics) is not started.
TEAMGRAM_SERVICES=(idgen status authsession dfs media biz msg sync push bff session gnetway)
SERVICES=(mysql redis kafka kafka-init etcd garage jaeger migrate storage-init fakepush "${TEAMGRAM_SERVICES[@]}")

# One image for all teamgram services, removed by `down` unless E2E_KEEP_IMAGE=1
# (the Go build cache stays in BuildKit, so the next build is quick).
export TEAMGRAM_IMAGE=${TEAMGRAM_IMAGE:-pluma-e2e-teamgram:local}
# A crash must fail the test, not be hidden by a restart.
export TEAMGRAM_RESTART=${TEAMGRAM_RESTART:-no}

# Kafka consumer groups of msg, sync and push and the topic each reads, as
# group:topic (InboxConsumer in teamgramd/etc2/msg.yaml, SyncConsumer in
# teamgramd/etc2/sync.yaml, PushConsumer in teamgramd/etc2/push.yaml).
CONSUMER_GROUPS=(Inbox-MainCommunity-S:Inbox-T Sync-MainCommunity-S:Sync-T Push-MainCommunity-S:Sync-T)
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
		if [[ -n ${E2E_SERVER_KEY:-} ]] && ! cmp -s "$E2E_SERVER_KEY" "$KEY_FILE"; then
			log "E2E_SERVER_KEY is not the key of the existing stand; run down first"
			return 1
		fi
		return
	fi
	rm -f "$ENV_FILE" "$KEY_FILE"
	mkdir -p "$E2E_DIR" && chmod 700 "$E2E_DIR" || return
	local fp keytool=(-out "$KEY_FILE")
	if [[ -n ${E2E_SERVER_KEY:-} ]]; then
		install -m 600 "$E2E_SERVER_KEY" "$KEY_FILE" || return
		keytool=(-show "$KEY_FILE")
	fi
	fp=$(cd "$ROOT" && go run ./tools/genserverkey "${keytool[@]}" |
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
S3_ACCESS_KEY_ID=GK$(secret 12)
S3_SECRET_ACCESS_KEY=$(secret 32)
GARAGE_RPC_SECRET=$(secret 32)
GARAGE_ADMIN_TOKEN=$(secret 32)
GRAFANA_ADMIN_PASSWORD=$(secret 24)
LOGIN_CODE_HMAC_KEY=$(secret 32)
AUTH_KEY_ENCRYPTION_KEY=$(secret 32)
PUSH_SECRET_ENCRYPTION_KEY=$(secret 32)
NTFY_TOPIC=$(secret 16)
NTFY_URL=http://127.0.0.1:9
HEALTHCHECKS_PING_URL=http://127.0.0.1:9/ping
RESTIC_REPOSITORY=/repo
RESTIC_PASSWORD=$(secret 24)
BACKUP_S3_ACCESS_KEY_ID=unused
BACKUP_S3_SECRET_ACCESS_KEY=unused
MAXMIND_ACCOUNT_ID=unused
MAXMIND_LICENSE_KEY=unused
SERVER_RSA_KEY_FILE=$KEY_FILE
APNS_KEY_ID=${E2E_APNS_KEY_ID:-E2EKEY0001}
APNS_TEAM_ID=${E2E_APNS_TEAM_ID:-E2ETEAM001}
APNS_KEY_FILE=$APNS_KEY_FILE
FCM_CREDENTIALS_FILE=$FCM_CREDENTIALS_FILE
E2E_PUSH_CONFIG=$PUSH_CONFIG
SERVER_RSA_KEY_FINGERPRINT=$fp
EOF
	)
}

# gen_push writes the provider keys and the config of the push service:
# throwaway keys and test/e2e/fakepush, or the real keys of E2E_FCM_CREDENTIALS
# and E2E_APNS_KEY with the real providers.
gen_push() {
	local real_fcm=0 real_apns=0 tmp
	if [[ -n ${E2E_FCM_CREDENTIALS:-} ]]; then
		install -m 600 "$E2E_FCM_CREDENTIALS" "$FCM_CREDENTIALS_FILE" || return
		real_fcm=1
	fi
	if [[ -n ${E2E_APNS_KEY:-} ]]; then
		install -m 600 "$E2E_APNS_KEY" "$APNS_KEY_FILE" || return
		real_apns=1
	fi
	if ((real_fcm == 0 || real_apns == 0)); then
		tmp=$(mktemp -d "$E2E_DIR/keys.XXXXXX") || return
		if ! (cd "$ROOT" && go run ./test/e2e/fakepush -keys "$tmp" -url "$FAKEPUSH_URL"); then
			rm -rf "$tmp"
			return 1
		fi
		if ((real_apns == 0)); then mv "$tmp/apns_key.p8" "$APNS_KEY_FILE" || return; fi
		if ((real_fcm == 0)); then mv "$tmp/fcm_credentials.json" "$FCM_CREDENTIALS_FILE" || return; fi
		rm -rf "$tmp"
	fi
	awk -v url="$FAKEPUSH_URL" -v apns="$real_apns" -v fcm="$real_fcm" '
		{ print }
		/^APNs:$/ && apns == 0 { print "  ProductionURL: " url; print "  SandboxURL: " url "/sandbox" }
		/^FCM:$/ && fcm == 0 { print "  Endpoint: " url; print "  TokenURL: " url "/token" }
	' "$ROOT/teamgramd/etc2/push.yaml" >"$PUSH_CONFIG"
}

cmd_up() {
	gen_secrets || return
	gen_push || return
	log "building the teamgram image"
	compose build idgen || return
	log "starting ${SERVICES[*]}"
	compose up --detach --wait --wait-timeout 300 "${SERVICES[@]}" || return
	wait_consumers || return
	log "stack is up; gateway at $(gateway_addr)"
}

# check_running fails when a teamgram service has exited: with restarts off
# a crash during the tests must fail them.
check_running() {
	local exited
	exited=$(compose ps --all --status exited --format '{{.Service}}' "${TEAMGRAM_SERVICES[@]}") || return
	if [[ -n $exited ]]; then
		log "services exited during the run: $(tr '\n' ' ' <<<"$exited")"
		return 1
	fi
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
	compose port gnetway 10443
}

cmd_test() {
	local addr
	addr=$(gateway_addr) || return
	log "running the e2e tests against $addr"
	(
		cd "$ROOT"
		# shellcheck disable=SC2086 # E2E_GOTEST_FLAGS is a list of flags
		PLUMA_E2E_ADDR=$addr \
			PLUMA_E2E_FAKEPUSH=$(compose port fakepush 8080) \
			PLUMA_E2E_SERVER_KEY=$KEY_FILE \
			PLUMA_E2E_RUN="$ROOT/test/e2e/run.sh" \
			go test -tags e2e -count=1 -timeout 10m -v ${E2E_GOTEST_FLAGS:-} ./test/e2e/... ./test/conformance/...
	) || return
	check_running
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
		compose down --volumes --remove-orphans --timeout 60 || true
	fi
	if [[ ${E2E_KEEP_IMAGE:-} != 1 ]]; then
		docker image rm "$TEAMGRAM_IMAGE" pluma-backup:local pluma-e2e-fakepush:local >/dev/null 2>&1 || true
	fi
	rm -f "$ENV_FILE" "$KEY_FILE" "$APNS_KEY_FILE" "$FCM_CREDENTIALS_FILE" "$PUSH_CONFIG"
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
wait)
	# After a chaos step: start whatever was stopped and wait until the
	# stack serves again (healthchecks, consumer groups own their partitions).
	compose up --detach --wait --wait-timeout 300 "${SERVICES[@]}" && wait_consumers
	;;
test) cmd_test ;;
logs) cmd_logs ;;
down) cmd_down ;;
all) cmd_all ;;
admin)
	shift
	compose exec -T bff ./pluma-admin -f ../etc2/bff.yaml "$@"
	;;
lan)
	# A phone on the same network as this machine: its development build
	# dials ADDR:10443 (the dev_address input of the client's workflow,
	# docs/ops/clients.md), and test/e2e/lanforward relays that to the
	# gateway's loopback port. The stand is open to the network meanwhile.
	gateway=$(compose port gnetway 10443)
	if [ -z "$gateway" ]; then
		echo "run.sh: the stand is not running (test/e2e/run.sh up)" >&2
		exit 1
	fi
	addr=${2:-}
	if [ -z "$addr" ]; then
		if command -v ipconfig >/dev/null; then
			addr=$(ipconfig getifaddr "$(route -n get default | awk '/interface:/ {print $2}')")
		else
			addr=$(ip -4 route get 1.1.1.1 | awk '{for (i = 1; i < NF; i++) if ($i == "src") print $(i + 1)}')
		fi
	fi
	if [ -z "$addr" ]; then
		echo "run.sh: no address of this machine found; pass one: test/e2e/run.sh lan ADDR" >&2
		exit 1
	fi
	echo "run.sh: a development build for $addr reaches the stand while this runs (dev_address=$addr)"
	cd "$ROOT" && exec go run ./test/e2e/lanforward -listen "$addr:10443" -to "$gateway"
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
