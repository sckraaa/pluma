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
#   E2E_CDN_ADDRESS  ADDR:PORT where test/e2e/fakecdn, the CDN of large files,
#                is published and which the redirects name (default:
#                fakecdn:8080 on the network of the services, which the tests
#                reach on a loopback port), e.g. this machine's address on the
#                network of a development client; bff checks the CDN there
#                too, so it needs a Linux Docker host, not Docker in a VM
#                (colima)
#   E2E_CDN_URL_TTL, E2E_CDN_HEALTH_EVERY, E2E_CDN_AVOID_TTL  CDN_URL_TTL,
#                CDN_HEALTH_EVERY and CDN_AVOID_TTL of the stand (default 20s,
#                5s and 30s; the tests of the CDN are given them)
#
# The secrets (passwords, the server RSA key) are generated for one run and
# deleted by `down`; they are never written anywhere git tracks.

set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
E2E_DIR=${E2E_DIR:-$ROOT/.e2e}
E2E_PROJECT=${E2E_PROJECT:-pluma-e2e}
ENV_FILE=$E2E_DIR/env
KEY_FILE=$E2E_DIR/server_pkcs1.key
# The RSA key of the ID tokens of oauth.pluma.chat (task 106), throwaway.
OAUTH_KEY_FILE=$E2E_DIR/oauth_signing_key.pem
LOG_DIR=$E2E_DIR/logs
# Push providers of the stand (task 009): throwaway keys and the config of
# the push service pointing at test/e2e/fakepush, or the real keys.
APNS_KEY_FILE=$E2E_DIR/apns_key.p8
FCM_CREDENTIALS_FILE=$E2E_DIR/fcm_credentials.json
PUSH_CONFIG=$E2E_DIR/push.yaml
FAKEPUSH_URL=http://fakepush:8080
# The config of bff with the limits of voice transcription set for the tests
# (task 090): the engine is test/e2e/fakestt; and the mail provider of the
# recovery email (task 091), test/e2e/fakemail. The maps of geo points
# (task 100) are rendered by test/e2e/fakemaps (MAPS_URL in the env file).
# The search of @pic and the sites of the pictures and web files (task
# 099) are test/e2e/fakeinline: SEARXNG_URL in the env file, and its host
# in WebFetch.AllowHosts of the config, as the server fetches from public
# addresses only. The translation engine of messages (task 101) is
# test/e2e/faketranslate (TRANSLATE_URL in the env file), and the config
# waits for it less than a server does.
BFF_CONFIG=$E2E_DIR/bff.yaml
FAKESTT_MODEL=e2e-model
FAKETRANSLATE_URL=http://faketranslate:8080
FAKETRANSLATE_MODEL=e2e-translate
FAKEMAIL_URL=http://fakemail:8080
FAKEMAPS_URL=http://fakemaps:8080
FAKEINLINE_HOST=fakeinline
FAKEINLINE_URL=http://$FAKEINLINE_HOST:8080
# The Gcore CDN resource of the HTTPS CDN of large files (task 107):
# test/e2e/fakecdn, which pulls the cipher from bff's origin.
FAKECDN_URL=http://fakecdn:8080
# Delivery 202 of the Russian node (task 109): test/e2e/rufiles, the node's
# nginx under the name below, with the run's certificate (RUFILES_TLS) of
# the run's CA (RUFILES_CA, which bff's checks and the tests trust). Clients
# from Russia (the stand's GeoIP database) are routed to it.
RU_FILES_DOMAIN=r1.e2e.pluma.test
RUFILES_TLS=$E2E_DIR/rufiles
RUFILES_CA=$E2E_DIR/rufiles-ca
# The releases of the client apps (docker-compose.releases.yaml, task 115):
# the run's upload token, and the name of the CDN in front of /dl/, which
# bff and the site's latest links name and nothing dials (the tests stand
# in for Gcore at the edge's origin).
RELEASES_SECRETS=$E2E_DIR/releases
RELEASES_CDN_URL=https://cdn.e2e.pluma.test
# The SFU of group calls (docker-compose.sfu.yaml, task 093): the
# certificates of its control API, the SFU's and bff's.
SFU_CERTS=$E2E_DIR/sfu
GROUPCALLS_CERTS=$E2E_DIR/groupcalls
# The Caddy of the web client with the sites of the edges (task 109,
# docker-compose.web.yaml): the plain image the web client's own is built
# on (pluma-web ci/site.Dockerfile), so the site of Web K is empty here.
WEB_IMAGE=caddy:2.11.6-alpine@sha256:d44355d3c2149dc580ce2cac735955d1c08d3d00882c30489c241aa51a5c10d9

# Teamgram services (docker-compose.yaml) and the infrastructure the tests
# need; everything else in docker-compose-env.yaml (Elasticsearch, Kibana,
# filebeat, go-stash, metrics) is not started.
TEAMGRAM_SERVICES=(idgen status authsession dfs media biz msg sync push bff session gnetway links releases)
# Services outside the teamgram image that must not exit during the run either.
OWN_SERVICES=(sfu web rufiles)
SERVICES=(mysql redis kafka kafka-init etcd garage jaeger migrate storage-init fakepush fakecodes fakestt faketranslate fakemail fakemaps fakeinline fakecdn "${OWN_SERVICES[@]}" "${TEAMGRAM_SERVICES[@]}")

# One image for all teamgram services, removed by `down` unless E2E_KEEP_IMAGE=1
# (the Go build cache stays in BuildKit, so the next build is quick). The
# images are tagged with the project: stands of several branches run at once
# on one machine, and a shared tag let one stand's build replace the image of
# another's containers (recreated by `wait` after a test restarts a service),
# and one's `down` remove the image of another.
export TEAMGRAM_IMAGE=${TEAMGRAM_IMAGE:-pluma-e2e-teamgram:$E2E_PROJECT}
export PLUMA_BACKUP_IMAGE=${PLUMA_BACKUP_IMAGE:-pluma-backup:$E2E_PROJECT}
export E2E_FAKEPUSH_IMAGE=${E2E_FAKEPUSH_IMAGE:-pluma-e2e-fakepush:$E2E_PROJECT}
export E2E_FAKECODES_IMAGE=${E2E_FAKECODES_IMAGE:-pluma-e2e-fakecodes:$E2E_PROJECT}
export E2E_FAKESTT_IMAGE=${E2E_FAKESTT_IMAGE:-pluma-e2e-fakestt:$E2E_PROJECT}
export E2E_FAKETRANSLATE_IMAGE=${E2E_FAKETRANSLATE_IMAGE:-pluma-e2e-faketranslate:$E2E_PROJECT}
export E2E_FAKEMAIL_IMAGE=${E2E_FAKEMAIL_IMAGE:-pluma-e2e-fakemail:$E2E_PROJECT}
export E2E_FAKEMAPS_IMAGE=${E2E_FAKEMAPS_IMAGE:-pluma-e2e-fakemaps:$E2E_PROJECT}
export E2E_FAKEINLINE_IMAGE=${E2E_FAKEINLINE_IMAGE:-pluma-e2e-fakeinline:$E2E_PROJECT}
export E2E_FAKECDN_IMAGE=${E2E_FAKECDN_IMAGE:-pluma-e2e-fakecdn:$E2E_PROJECT}
export E2E_RUFILES_IMAGE=${E2E_RUFILES_IMAGE:-pluma-e2e-rufiles:$E2E_PROJECT}
export PLUMA_SFU_IMAGE=${PLUMA_SFU_IMAGE:-pluma-e2e-sfu:$E2E_PROJECT}
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
		-f "$ROOT/docker-compose.sfu.yaml" \
		-f "$ROOT/docker-compose.web.yaml" \
		-f "$ROOT/docker-compose.releases.yaml" \
		-f "$ROOT/test/e2e/docker-compose.e2e.yaml" \
		"$@"
}

log() { printf '[e2e %s] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }

secret() { openssl rand -hex "$1"; }

# The steps below run where `set -e` does not apply (cmd_all calls them in
# `||` lists), so every step that can fail returns explicitly.

# gen_secrets writes the env file and the server RSA key for this run.
gen_secrets() {
	if [[ -f $ENV_FILE && -f $KEY_FILE && -f $OAUTH_KEY_FILE ]]; then
		if [[ -n ${E2E_SERVER_KEY:-} ]] && ! cmp -s "$E2E_SERVER_KEY" "$KEY_FILE"; then
			log "E2E_SERVER_KEY is not the key of the existing stand; run down first"
			return 1
		fi
		return
	fi
	rm -f "$ENV_FILE" "$KEY_FILE" "$OAUTH_KEY_FILE"
	mkdir -p "$E2E_DIR" && chmod 700 "$E2E_DIR" || return
	(umask 077 && openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "$OAUTH_KEY_FILE" 2>/dev/null) || {
		log "openssl made no OAuth signing key"
		return 1
	}
	local sfu_port
	sfu_port=$(free_port) || return
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
		cat >"$ENV_FILE" <<EOF || exit
MYSQL_ROOT_PASSWORD=$(secret 24)
MYSQL_PASSWORD=$(secret 24)
REDIS_PASSWORD=$(secret 24)
S3_ACCESS_KEY_ID=GK$(secret 12)
S3_SECRET_ACCESS_KEY=$(secret 32)
GARAGE_RPC_SECRET=$(secret 32)
GARAGE_ADMIN_TOKEN=$(secret 32)
GRAFANA_ADMIN_PASSWORD=$(secret 24)
CALLS_ICE_SERVERS=198.51.100.7:3478:stun+turn
CALLS_TURN_SECRET=$(secret 32)
LOGIN_CODE_HMAC_KEY=$(secret 32)
LOGIN_CODES_SENDER_URL=http://fakecodes:8080
LOGIN_CODES_SENDER_TOKEN=$(secret 32)
STT_URL=http://fakestt:8080
STT_TOKEN=$(secret 32)
STT_MODEL=$FAKESTT_MODEL
TRANSLATE_URL=$FAKETRANSLATE_URL
TRANSLATE_TOKEN=$(secret 32)
TRANSLATE_MODEL=$FAKETRANSLATE_MODEL
MAIL_API_KEY=re_e2e_$(secret 16)
MAIL_FROM=Pluma <noreply@e2e.pluma.test>
MAPS_URL=$FAKEMAPS_URL
SEARXNG_URL=$FAKEINLINE_URL
WEBFILE_HMAC_KEY=$(secret 32)
BOT_SECRET_ENCRYPTION_KEY=$(secret 32)
WEBAPP_SIGNING_KEY=$(secret 32)
STATS_GRAPH_TOKEN_KEY=$(openssl rand -base64 32)
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
OAUTH_SIGNING_KEY_FILE=$OAUTH_KEY_FILE
APNS_KEY_ID=${E2E_APNS_KEY_ID:-E2EKEY0001}
APNS_TEAM_ID=${E2E_APNS_TEAM_ID:-E2ETEAM001}
APNS_KEY_FILE=$APNS_KEY_FILE
FCM_CREDENTIALS_FILE=$FCM_CREDENTIALS_FILE
E2E_PUSH_CONFIG=$PUSH_CONFIG
E2E_BFF_CONFIG=$BFF_CONFIG
SERVER_RSA_KEY_FINGERPRINT=$fp
SFU_PUBLIC_IPV4=127.0.0.1
SFU_UDP_PORT=$sfu_port
SFU_TCP_PORT=$sfu_port
PLUMA_SFU_CERTS=$SFU_CERTS
PLUMA_GROUPCALLS_CERTS=$GROUPCALLS_CERTS
EOF
		gen_cdn_env >>"$ENV_FILE"
		gen_edge_env >>"$ENV_FILE"
	) || {
		rm -f "$ENV_FILE"
		return 1
	}
}

# gen_cdn_env prints the HTTPS CDN of the stand for the env file (task 107,
# ADR 0071): delivery 201 is test/e2e/fakecdn over http://, which only the
# stand allows (CDN_DEV_ALLOW_HTTP), with the run's Secure Token key and
# origin header, and intervals short enough for the tests to wait for.
# Delivery 202 is test/e2e/rufiles over https:// with keys of its own,
# checked at rufiles:443 as a server checks it through the tunnel, for the
# clients from Russia (task 109); its origin is bff's through the stand's
# Caddy under PLUMA_CDN_ORIGIN_DOMAIN.
gen_cdn_env() {
	local base=$FAKECDN_URL
	if [[ -n ${E2E_CDN_ADDRESS:-} ]]; then
		if [[ ! $E2E_CDN_ADDRESS =~ ^[^:/]+:[0-9]+$ ]]; then
			log "E2E_CDN_ADDRESS is ADDR:PORT, not $E2E_CDN_ADDRESS"
			return 1
		fi
		base=http://$E2E_CDN_ADDRESS
		echo "E2E_CDN_PUBLISH=$E2E_CDN_ADDRESS:8080"
	fi
	cat <<EOF
CDN_GCORE_BASE_URL=$base
CDN_GCORE_TOKEN_KEY=$(secret 16)
CDN_ORIGIN_AUTH=$(secret 32)
CDN_DEV_ALLOW_HTTP=1
CDN_URL_TTL=${E2E_CDN_URL_TTL:-20s}
CDN_HEALTH_EVERY=${E2E_CDN_HEALTH_EVERY:-5s}
CDN_AVOID_TTL=${E2E_CDN_AVOID_TTL:-30s}
CDN_RU_BASE_URL=https://$RU_FILES_DOMAIN
CDN_RU_TOKEN_KEY=$(secret 16)
CDN_RU_ORIGIN_AUTH=$(secret 32)
CDN_RU_HEALTH_ADDR=rufiles:443
CDN_ROUTES=RU:202,201;*:201
PLUMA_CDN_ORIGIN_DOMAIN=origin.localhost
E2E_RU_FILES_DOMAIN=$RU_FILES_DOMAIN
E2E_RUFILES_TLS=$RUFILES_TLS
E2E_RUFILES_CA=$RUFILES_CA
EOF
}

# gen_rufiles issues the certificate of delivery 202 for the run: a CA of
# its own (RUFILES_CA/ca.pem, which bff and the tests trust) and the
# certificate of RU_FILES_DOMAIN (RUFILES_TLS). They are throwaway and lie
# in E2E_DIR, which only the owner opens; nginx in rufiles reads them as
# root, bff as its own user.
gen_rufiles() {
	if [[ -f $RUFILES_CA/ca.pem && -f $RUFILES_TLS/cert.pem && -f $RUFILES_TLS/key.pem ]]; then
		return
	fi
	rm -rf "$RUFILES_CA" "$RUFILES_TLS"
	mkdir -p "$RUFILES_CA" "$RUFILES_TLS" || return
	local tmp status=0
	tmp=$(mktemp -d "$E2E_DIR/rufiles.XXXXXX") || return
	(
		set -e
		cd "$tmp"
		openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 2 \
			-subj "/CN=Pluma e2e rufiles CA" -keyout ca.key -out ca.pem 2>/dev/null
		openssl req -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \
			-subj "/CN=$RU_FILES_DOMAIN" -keyout key.pem -out leaf.csr 2>/dev/null
		printf 'subjectAltName=DNS:%s\nextendedKeyUsage=serverAuth\n' "$RU_FILES_DOMAIN" >leaf.ext
		openssl x509 -req -in leaf.csr -CA ca.pem -CAkey ca.key -CAcreateserial -days 2 \
			-extfile leaf.ext -out leaf.pem 2>/dev/null
		cat leaf.pem ca.pem >"$RUFILES_TLS/cert.pem"
		install -m 0600 key.pem "$RUFILES_TLS/key.pem"
		install -m 0644 ca.pem "$RUFILES_CA/ca.pem"
	) || status=$?
	rm -rf "$tmp"
	if [[ $status != 0 ]]; then
		log "openssl made no certificate of delivery 202"
		return 1
	fi
	chmod 0755 "$RUFILES_CA" "$RUFILES_TLS" && chmod 0644 "$RUFILES_TLS/cert.pem"
}

# gen_edge_env prints the edges of the stand for the env file (task 109,
# ADR 0072): Caddy's sites of the Gcore origin and of the Russian node's
# edge under *.localhost names, the run's X-Pluma-Edge-Auth, and the
# pluma_edge of bff by country (Russia by the stand's GeoIP database). The
# addresses of the edges in pluma_edge are only handed out, never dialled.
# Task 113: the mirrors of the site and of the login on sites through the
# node, the releases' site with its mirror for Russia, and two settings of
# the path probes off their defaults (path_probe_test.go). Task 115: the
# caches of the releases' /dl/, the CDN for everyone and delivery 202's
# node for Russia, and the directory of the upload token (gen_releases).
gen_edge_env() {
	cat <<EOF
PLUMA_WEB_IMAGE=$WEB_IMAGE
PLUMA_WEB_DOMAIN=web.localhost
PLUMA_SITE_DOMAINS=site.localhost
PLUMA_EDGE_ORIGIN_DOMAIN=edge.localhost
PLUMA_EDGE_ORIGIN_AUTH=$(secret 32)
PLUMA_RU_EDGE_DOMAIN=ru.localhost
EDGE_WS_URLS=gcore=wss://edge.e2e.pluma.test/apiws,ru=wss://ru.e2e.pluma.test/apiws
EDGE_ROUTES=RU:edge,direct:ru,gcore;*:direct,edge:gcore,ru
EDGE_STICKY_S=900
PLUMA_RU_SITE_DOMAIN=rusite.localhost
PLUMA_RU_OAUTH_DOMAIN=ruoauth.localhost
PLUMA_RELEASES_URL=https://site.localhost
PLUMA_RELEASES_ROUTES=RU=https://rusite.localhost
PLUMA_RELEASES_FILES_URL=$RELEASES_CDN_URL
PLUMA_RELEASES_FILES_ROUTES=RU=https://$RU_FILES_DOMAIN
PLUMA_RELEASES_SECRETS=$RELEASES_SECRETS
PATH_PROBE_STALL_MS=1700
PATH_PROBE_SWITCH_RATIO=0.65
EOF
}

# env_value prints the value of name in the env file of the run.
env_value() {
	sed -n "s/^$1=//p" "$ENV_FILE" | tail -n 1
}

# free_port prints a port free for both UDP and TCP on this machine. The
# SFU's media ports are fixed for the run: its answers to joins name the
# port the clients reach it at, which Docker's ephemeral ports would not
# give before the SFU starts.
port_taken() {
	if command -v ss >/dev/null; then
		[[ -n $(ss -Hlnu "sport = :$1") || -n $(ss -Hlnt "sport = :$1") ]]
	else
		lsof -nP -iUDP:"$1" -iTCP:"$1" >/dev/null 2>&1
	fi
}

free_port() {
	local i p
	for ((i = 0; i < 50; i++)); do
		p=$((21000 + RANDOM % 9000))
		if ! port_taken "$p"; then
			echo "$p"
			return
		fi
	done
	log "no free port for the SFU"
	return 1
}

# gen_sfu issues the certificates of the SFU's control API for the run. The
# SFU's image runs as nonroot (uid 65532), which must read its files; they
# are throwaway and lie in E2E_DIR, which only the owner opens.
gen_sfu() {
	if [[ -f $SFU_CERTS/server.pem && -f $GROUPCALLS_CERTS/client.pem ]]; then
		return
	fi
	rm -rf "$SFU_CERTS" "$GROUPCALLS_CERTS"
	"$ROOT/teamgramd/deploy/sfu/control-certs.sh" "$SFU_CERTS" "$GROUPCALLS_CERTS" >/dev/null || return
	chmod 0755 "$SFU_CERTS" && chmod 0444 "$SFU_CERTS"/*.pem
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

# gen_bff writes the config of bff for the stand: a record that fails is
# given up after the second attempt, so that a test of a failing engine
# waits one pause (5 s) and not all three, and is tried again 15 s after it
# failed, not 10 minutes. A translation is waited for 5 seconds, not 25
# or 45, so that a test of a slow engine sees FLOOD_WAIT_X soon. The emails
# go to fakemail, and the server's day of emails holds every run of the
# tests. The fetches of web files and of the pictures of @pic may reach
# fakeinline (WebFetch.AllowHosts), which a server's config never allows.
gen_bff() {
	if grep -q '^WebFetch:' "$ROOT/teamgramd/etc2/bff.yaml"; then
		log "teamgramd/etc2/bff.yaml has WebFetch: a server's config must not open internal hosts"
		return 1
	fi
	awk -v mail="$FAKEMAIL_URL" -v host="$FAKEINLINE_HOST" '
		{ print }
		/^Transcribe:$/ { print "  MaxAttempts: 2"; print "  FailedTTL: 15s" }
		/^Translate:$/ { print "  SyncWait: 5s"; print "  SyncWaitSingle: 5s" }
		/^Mail:$/ { print "  Endpoint: " mail; print "  DailyLimit: 10000" }
		END { print ""; print "WebFetch:"; print "  AllowHosts:"; print "    - " host }
	' "$ROOT/teamgramd/etc2/bff.yaml" >"$BFF_CONFIG" || return
	grep -q '^  MaxAttempts: 2$' "$BFF_CONFIG" && grep -q '^  FailedTTL: 15s$' "$BFF_CONFIG" &&
		grep -q '^  SyncWait: 5s$' "$BFF_CONFIG" && grep -q '^  SyncWaitSingle: 5s$' "$BFF_CONFIG" &&
		grep -q "^  Endpoint: $FAKEMAIL_URL\$" "$BFF_CONFIG" && grep -q "^    - $FAKEINLINE_HOST\$" "$BFF_CONFIG"
}

# gen_releases writes the upload token of the releases service for the run,
# in E2E_DIR, which only the owner opens; the service runs as root.
gen_releases() {
	[[ -s $RELEASES_SECRETS/upload-token ]] && return
	mkdir -p "$RELEASES_SECRETS" && chmod 700 "$RELEASES_SECRETS" || return
	(umask 077 && secret 32 >"$RELEASES_SECRETS/upload-token") || return
}

cmd_up() {
	gen_secrets || return
	gen_push || return
	gen_bff || return
	gen_sfu || return
	gen_rufiles || return
	gen_releases || return
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
	exited=$(compose ps --all --status exited --format '{{.Service}}' "${OWN_SERVICES[@]}" "${TEAMGRAM_SERVICES[@]}") || return
	if [[ -n $exited ]]; then
		log "services exited during the run: $(tr '\n' ' ' <<<"$exited")"
		return 1
	fi
	check_memory
}

# check_memory fails when the kernel killed a container of the stack at its
# memory limit (docs/ops/memory.md): the limits must hold the load of the
# tests, infrastructure included.
check_memory() {
	local ids killed
	ids=$(compose ps --all --quiet) || return
	[[ -n $ids ]] || return 0
	# shellcheck disable=SC2086 # one id per word
	killed=$(docker inspect --format '{{if .State.OOMKilled}}{{.Name}}{{end}}' $ids | sed '/^$/d; s#^/##') || return
	if [[ -n $killed ]]; then
		log "killed at their memory limit: $(tr '\n' ' ' <<<"$killed")"
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
			PLUMA_E2E_FAKECODES=$(compose port fakecodes 8080) \
			PLUMA_E2E_FAKESTT=$(compose port fakestt 8080) \
			PLUMA_E2E_FAKETRANSLATE=$(compose port faketranslate 8080) \
			PLUMA_E2E_FAKEMAIL=$(compose port fakemail 8080) \
			PLUMA_E2E_FAKEMAPS=$(compose port fakemaps 8080) \
			PLUMA_E2E_FAKEINLINE=$(compose port fakeinline 8080) \
			PLUMA_E2E_LINKS=$(compose port links 9096) \
			PLUMA_E2E_CDN_URL_TTL=$(env_value CDN_URL_TTL) \
			PLUMA_E2E_CDN_HEALTH_EVERY=$(env_value CDN_HEALTH_EVERY) \
			PLUMA_E2E_CDN_AVOID_TTL=$(env_value CDN_AVOID_TTL) \
			PLUMA_E2E_RU_FILES=$(compose port rufiles 443) \
			PLUMA_E2E_RU_FILES_DOMAIN=$RU_FILES_DOMAIN \
			PLUMA_E2E_RU_FILES_CA=$RUFILES_CA/ca.pem \
			PLUMA_E2E_RELEASES_TOKEN_FILE=$RELEASES_SECRETS/upload-token \
			PLUMA_E2E_RELEASES_CDN_URL=$RELEASES_CDN_URL \
			PLUMA_E2E_EDGE_ORIGIN=$(compose port web 443) \
			PLUMA_E2E_RU_EDGE=$(compose port web 8443) \
			PLUMA_E2E_EDGE_AUTH=$(env_value PLUMA_EDGE_ORIGIN_AUTH) \
			PLUMA_E2E_EDGE_WS_URLS=$(env_value EDGE_WS_URLS) \
			PLUMA_E2E_EDGE_STICKY_S=$(env_value EDGE_STICKY_S) \
			PLUMA_E2E_SERVER_KEY=$KEY_FILE \
			PLUMA_E2E_RUN="$ROOT/test/e2e/run.sh" \
			go test -tags e2e -count=1 -timeout 60m -v ${E2E_GOTEST_FLAGS:-} ./test/e2e/... ./test/conformance/...
	) || return
	check_running
}

cmd_logs() {
	mkdir -p "$LOG_DIR"
	compose ps --all >"$LOG_DIR/ps.txt" 2>&1 || true
	check_memory >>"$LOG_DIR/ps.txt" 2>&1 || true
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
		docker image rm "$TEAMGRAM_IMAGE" "$PLUMA_BACKUP_IMAGE" "$E2E_FAKEPUSH_IMAGE" "$E2E_FAKECODES_IMAGE" "$E2E_FAKESTT_IMAGE" "$E2E_FAKETRANSLATE_IMAGE" "$E2E_FAKEMAIL_IMAGE" "$E2E_FAKEMAPS_IMAGE" "$E2E_FAKEINLINE_IMAGE" "$E2E_FAKECDN_IMAGE" "$E2E_RUFILES_IMAGE" "$PLUMA_SFU_IMAGE" >/dev/null 2>&1 || true
	fi
	rm -f "$ENV_FILE" "$KEY_FILE" "$OAUTH_KEY_FILE" "$APNS_KEY_FILE" "$FCM_CREDENTIALS_FILE" "$PUSH_CONFIG" "$BFF_CONFIG"
	rm -rf "$SFU_CERTS" "$GROUPCALLS_CERTS" "$RUFILES_TLS" "$RUFILES_CA" "$RELEASES_SECRETS"
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
