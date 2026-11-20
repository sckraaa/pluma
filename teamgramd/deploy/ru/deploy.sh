#!/usr/bin/env bash
# Deploys the Russian node of Pluma (task 109, ADR 0072 §8,
# docs/ops/ru-node.md) on amf, as root, without Docker: services of the
# Ubuntu packages, files from the repository at a commit.
#
#   ssh amf 'NEW=<sha> bash -s' < teamgramd/deploy/ru/deploy.sh
#
# fetches the repository at NEW into /opt/pluma-ru/src and runs the
# deploy.sh of that commit with "apply". The commands of the checkout
# (/opt/pluma-ru/src/teamgramd/deploy/ru/deploy.sh <command>):
#
#   apply          packages on the first run, the files of /etc/pluma-ru
#                  from the templates and /etc/pluma-ru/env, the units;
#                  restarts only what changed
#   cert-register  the ACME account of the node; prints its URL for the CAA
#                  record of the name of delivery 202
#   cert-issue     the certificate of delivery 202 (HTTP-01 through the
#                  owner's port 80), then starts pluma-ru-nginx
#   render <env file> <directory> [--cache-status-header]
#                  writes the files only, for the tests; the header is
#                  X-Cache-Status of the test build
#
# The owner's nginx is never touched here: that is owner-nginx.sh. Nothing
# of /etc/pluma-ru/env, secrets.conf or turn.conf is printed.
set -euo pipefail
umask 022
shopt -u patsub_replacement 2>/dev/null || true

REPO=${PLUMA_RU_REPO:-https://github.com/sckraaa/pluma.git}
SRC=/opt/pluma-ru/src
ETC=/etc/pluma-ru
ENV_FILE=$ETC/env
LE_DIR=$ETC/letsencrypt
CERTBOT_DIRS=(--config-dir "$LE_DIR" --work-dir /var/lib/pluma-ru/certbot --logs-dir /var/log/pluma-ru/certbot)
# The packages of the node. wireguard-tools carries the tunnel (tunnel.sh
# --link ru), nginx and certbot are usually the owner's already.
PACKAGES=(nginx coturn prometheus-node-exporter certbot wireguard-tools)
# Units of the packages that must not run: their ports are ours, and the
# package's coturn would start with the default configuration.
PACKAGE_UNITS=(coturn.service prometheus-node-exporter.service)
UNITS=(pluma-ru-nginx.service pluma-ru-turn.service pluma-ru-node-exporter.service
	pluma-ru-wgstat.service pluma-ru-wgstat.timer pluma-ru-certbot.service pluma-ru-certbot.timer)

die() {
	echo "deploy: $*" >&2
	exit 1
}
say() { echo "deploy: $*"; }

# ---------------------------------------------------------------------------
# /etc/pluma-ru/env: NAME=value lines, no quotes, no expansion. Only the
# names below; the values are checked before anything is written, and an
# error names the variable, never its value.

KNOWN_VARS=(PLUMA_RU_PUBLIC_IPV4 PLUMA_RU_FILES_DOMAIN PLUMA_RU_EDGE_DOMAIN PLUMA_CDN_ORIGIN_DOMAIN
	PLUMA_WEB_DOMAIN CDN_RU_TOKEN_KEY CDN_RU_ORIGIN_AUTH CALLS_TURN_SECRETS PLUMA_RU_ORIGIN_ADDR
	PLUMA_RU_ACME_EMAIL PLUMA_GCORE_EDGE_DOMAIN PLUMA_RU_MIRROR_DOMAINS PLUMA_RU_SFU_ADDR)
declare -A ENV=()

load_env() {
	local file=$1 line n=0 name
	[ -r "$file" ] || die "$file is missing (docs/ops/ru-node.md, «Переменные»)"
	while IFS= read -r line || [ -n "$line" ]; do
		n=$((n + 1))
		case $line in '' | '#'*) continue ;; esac
		[[ $line =~ ^([A-Z][A-Z0-9_]*)=(.*)$ ]] || die "$file:$n is not NAME=value"
		name=${BASH_REMATCH[1]}
		case " ${KNOWN_VARS[*]} " in
		*" $name "*) ;;
		*) die "$file:$n: unknown variable $name" ;;
		esac
		[ -z "${ENV[$name]+set}" ] || die "$file:$n: $name is set twice"
		ENV[$name]=${BASH_REMATCH[2]}
	done <"$file"
}

DOMAIN_RE='^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$'

need() {
	[ -n "${ENV[$1]:-}" ] || die "$1 is not set in the env file"
}

# ipv4 <name>: the value of the variable is a dotted IPv4 address.
ipv4() {
	local o
	[[ ${ENV[$1]} =~ ^([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})$ ]] || die "$1 is not an IPv4 address"
	for o in "${BASH_REMATCH[@]:1}"; do
		((10#$o <= 255)) || die "$1 is not an IPv4 address"
	done
}

# check_env: every value against its form; the values go into nginx and
# coturn configuration lines, so the forms leave out quotes, $, ;, braces,
# spaces and line breaks.
check_env() {
	local v
	for v in PLUMA_RU_PUBLIC_IPV4 PLUMA_RU_FILES_DOMAIN PLUMA_RU_EDGE_DOMAIN PLUMA_CDN_ORIGIN_DOMAIN \
		PLUMA_WEB_DOMAIN CDN_RU_TOKEN_KEY CDN_RU_ORIGIN_AUTH CALLS_TURN_SECRETS; do
		need "$v"
	done
	ipv4 PLUMA_RU_PUBLIC_IPV4
	# The SFU of group calls behind the relay (task 116): its public
	# address, which the relay passes the media to. Empty: no relay.
	if [ -n "${ENV[PLUMA_RU_SFU_ADDR]:-}" ]; then
		ipv4 PLUMA_RU_SFU_ADDR
		[ "${ENV[PLUMA_RU_SFU_ADDR]}" != "${ENV[PLUMA_RU_PUBLIC_IPV4]}" ] ||
			die "PLUMA_RU_SFU_ADDR is this node's own address: the relay would pass the media to itself"
	fi
	for v in PLUMA_RU_FILES_DOMAIN PLUMA_RU_EDGE_DOMAIN PLUMA_CDN_ORIGIN_DOMAIN PLUMA_WEB_DOMAIN; do
		[[ ${ENV[$v]} =~ $DOMAIN_RE ]] || die "$v is not a lower-case domain name"
	done
	[ "${ENV[PLUMA_RU_FILES_DOMAIN]}" != "${ENV[PLUMA_RU_EDGE_DOMAIN]}" ] ||
		die "PLUMA_RU_FILES_DOMAIN and PLUMA_RU_EDGE_DOMAIN are the same name"
	# The Secure Token key of delivery 202, CDN_RU_TOKEN_KEY of the home's
	# .env: what pkg/cdn takes (6 to 32 characters), from a set that is
	# safe in an nginx string.
	[[ ${ENV[CDN_RU_TOKEN_KEY]} =~ ^[A-Za-z0-9._~-]{6,32}$ ]] ||
		die "CDN_RU_TOKEN_KEY must be 6 to 32 of A-Z a-z 0-9 . _ ~ - (openssl rand -hex 16)"
	# The origin's access value of delivery 202 (64 hex, as CDN_ORIGIN_AUTH).
	[[ ${ENV[CDN_RU_ORIGIN_AUTH]} =~ ^[0-9a-f]{64}$ ]] ||
		die "CDN_RU_ORIGIN_AUTH must be 64 lower-case hex digits (openssl rand -hex 32)"
	turn_secret
	if [ -n "${ENV[PLUMA_RU_ORIGIN_ADDR]:-}" ]; then
		[[ ${ENV[PLUMA_RU_ORIGIN_ADDR]} =~ ^[0-9.]+:[0-9]{1,5}$ ]] || die "PLUMA_RU_ORIGIN_ADDR is not <IPv4>:<port>"
	fi
	mirror_names
	if [ -n "${ENV[PLUMA_GCORE_EDGE_DOMAIN]:-}" ]; then
		[[ ${ENV[PLUMA_GCORE_EDGE_DOMAIN]} =~ $DOMAIN_RE ]] || die "PLUMA_GCORE_EDGE_DOMAIN is not a lower-case domain name"
	fi
	if [ -n "${ENV[PLUMA_RU_ACME_EMAIL]:-}" ]; then
		[[ ${ENV[PLUMA_RU_ACME_EMAIL]} =~ ^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+$ ]] || die "PLUMA_RU_ACME_EMAIL is not an e-mail address"
	fi
}

# mirror_names sets MIRROR_NAMES to the names of PLUMA_RU_MIRROR_DOMAINS
# (task 113): the other names whose TLS goes home as the edge's does, the
# mirrors of the site and of the login on sites (pluma.sckra.dev,
# oauth.sckra.dev), separated by commas. Each is a lower-case domain name,
# at most MAX_MIRRORS of them, none twice, and none is the name of the edge
# or of delivery 202: the SNI map routes a name once. Empty: none.
MAX_MIRRORS=8
MIRROR_NAMES=()
mirror_names() {
	local name list=${ENV[PLUMA_RU_MIRROR_DOMAINS]:-}
	local -a names
	MIRROR_NAMES=()
	[ -n "$list" ] || return 0
	IFS=, read -r -a names <<<"$list"
	[[ $list != *, ]] || die "PLUMA_RU_MIRROR_DOMAINS ends with a comma"
	for name in "${names[@]}"; do
		[[ $name =~ $DOMAIN_RE ]] || die "PLUMA_RU_MIRROR_DOMAINS: an item is not a lower-case domain name"
		case " ${MIRROR_NAMES[*]} ${ENV[PLUMA_RU_EDGE_DOMAIN]} ${ENV[PLUMA_RU_FILES_DOMAIN]} " in
		*" $name "*) die "PLUMA_RU_MIRROR_DOMAINS: $name is named twice, or is the edge's or delivery 202's name" ;;
		esac
		MIRROR_NAMES+=("$name")
	done
	[ ${#MIRROR_NAMES[@]} -le $MAX_MIRRORS ] || die "PLUMA_RU_MIRROR_DOMAINS has more than $MAX_MIRRORS names"
}

# turn_secret sets TURN_SECRET to the secret of the label "ru" of
# CALLS_TURN_SECRETS (label=secret entries separated by commas, as in the
# home's .env). Only the node's own secret belongs on the node: another
# label is an error, so that a whole line copied from home does not bring
# the secrets of other TURN servers here.
TURN_SECRET=
turn_secret() {
	local entry label value
	local -a entries
	IFS=, read -r -a entries <<<"${ENV[CALLS_TURN_SECRETS]}"
	for entry in "${entries[@]}"; do
		label=${entry%%=*}
		value=${entry#*=}
		[ "$label" = ru ] || die "CALLS_TURN_SECRETS: only the secret labelled ru belongs on this node"
		[ -z "$TURN_SECRET" ] || die "CALLS_TURN_SECRETS: the label ru is set twice"
		[[ $value =~ ^[A-Za-z0-9]{32,128}$ ]] || die "CALLS_TURN_SECRETS: the secret of ru must be 32 to 128 letters and digits (openssl rand -hex 32)"
		TURN_SECRET=$value
	done
	[ -n "$TURN_SECRET" ] || die "CALLS_TURN_SECRETS has no secret labelled ru"
}

# ---------------------------------------------------------------------------
# Rendering.

HERE=$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)

# fill <template> <output> [cache status line]: the @@NAME@@ of the
# template replaced by the checked values.
fill() {
	local s gcore=
	s=$(<"$1")
	if [ -n "${ENV[PLUMA_GCORE_EDGE_DOMAIN]:-}" ]; then
		gcore="	\"https://${ENV[PLUMA_GCORE_EDGE_DOMAIN]}\" \$http_origin;"
	fi
	s=${s//@@GCORE_EDGE_CORS@@/$gcore}
	# The names whose TLS goes home: the edge and the mirrors, each a line
	# of the SNI map to the edge's upstream, and all of them the server
	# names of port 80 that passes their challenges home.
	local name sni names
	sni="${ENV[PLUMA_RU_EDGE_DOMAIN]} pluma_ru_edge;"
	names=${ENV[PLUMA_RU_EDGE_DOMAIN]}
	for name in "${MIRROR_NAMES[@]}"; do
		sni+=$'\n'"$name pluma_ru_edge;"
		names+=" $name"
	done
	s=${s//@@HOME_TLS_SNI@@/$sni}
	s=${s//@@HOME_TLS_NAMES@@/$names}
	s=${s//@@FILES_DOMAIN@@/${ENV[PLUMA_RU_FILES_DOMAIN]}}
	s=${s//@@EDGE_DOMAIN@@/${ENV[PLUMA_RU_EDGE_DOMAIN]}}
	s=${s//@@ORIGIN_DOMAIN@@/${ENV[PLUMA_CDN_ORIGIN_DOMAIN]}}
	s=${s//@@WEB_DOMAIN@@/${ENV[PLUMA_WEB_DOMAIN]}}
	s=${s//@@ORIGIN_ADDR@@/${ENV[PLUMA_RU_ORIGIN_ADDR]:-10.78.0.2:443}}
	s=${s//@@RELAY_ADDR@@/${ENV[PLUMA_RU_PUBLIC_IPV4]}}
	s=${s//@@SFU_ADDR@@/${ENV[PLUMA_RU_SFU_ADDR]:-}}
	s=${s//@@CACHE_STATUS_HEADER@@/${3:-}}
	if [[ $s =~ @@[A-Z_]+@@ ]]; then
		die "$1: ${BASH_REMATCH[0]} has no value"
	fi
	printf '%s\n' "$s" >"$2"
}

# render <env file> <directory> [--cache-status-header]: the files of
# /etc/pluma-ru into the directory (which exists and is 0700): nginx.conf,
# files.conf, stream.conf, secrets.conf (0600), turn.conf (0640), and
# owner/ for owner-nginx.sh.
render() {
	local out=$2 status=
	# shellcheck disable=SC2016 # an nginx variable, for nginx
	case ${3:-} in
	'') ;;
	--cache-status-header) status='add_header X-Cache-Status $upstream_cache_status always;' ;;
	*) die "render: unknown option $3" ;;
	esac
	load_env "$1"
	check_env

	install -m 0644 "$HERE/nginx.conf" "$out/nginx.conf"
	fill "$HERE/files.conf.tmpl" "$out/files.conf" "$status"
	chmod 0644 "$out/files.conf"
	# The relay of group calls only with the SFU's address: a node without
	# it listens on no port of the relay.
	if [ -n "${ENV[PLUMA_RU_SFU_ADDR]:-}" ]; then
		fill "$HERE/stream.conf.tmpl" "$out/stream.conf"
	else
		echo "# No relay of group calls: PLUMA_RU_SFU_ADDR is not set (docs/ops/ru-node.md)." >"$out/stream.conf"
	fi
	chmod 0644 "$out/stream.conf"

	(
		umask 077
		{
			echo "# Written by deploy.sh from $ENV_FILE. Secrets: 0600, never printed."
			echo "map \"\" \$pluma_token_key { default \"${ENV[CDN_RU_TOKEN_KEY]}\"; }"
			echo "map \"\" \$pluma_origin_auth { default \"${ENV[CDN_RU_ORIGIN_AUTH]}\"; }"
		} >"$out/secrets.conf"
		chmod 0600 "$out/secrets.conf"

		# The settings shared with the TURN of the Hetzner node, then the
		# node's own address and secret: coturn reads one file.
		local ip=${ENV[PLUMA_RU_PUBLIC_IPV4]}
		{
			echo "# Written by deploy.sh: teamgramd/deploy/turn/turnserver.conf and the"
			echo "# address and secret of this node from $ENV_FILE. 0640 root:turnserver."
			echo
			cat "$HERE/../turn/turnserver.conf"
			echo
			echo "listening-ip=$ip"
			echo "relay-ip=$ip"
			echo "external-ip=$ip"
			echo "static-auth-secret=$TURN_SECRET"
		} >"$out/turn.conf"
		chmod 0640 "$out/turn.conf"
	)

	mkdir -p "$out/owner"
	install -m 0644 "$HERE/stream-upstreams.conf" "$out/owner/pluma-upstreams.conf"
	fill "$HERE/sni.map.tmpl" "$out/owner/pluma-sni.map"
	fill "$HERE/acme-http.conf.tmpl" "$out/owner/pluma-acme-http.conf"
	chmod 0644 "$out/owner/pluma-sni.map" "$out/owner/pluma-acme-http.conf"
}

# ---------------------------------------------------------------------------
# apply.

require_root() {
	[ "$(id -u)" = 0 ] || die "run as root"
}

check_env_file() {
	[ -f "$ENV_FILE" ] || die "$ENV_FILE is missing: write it first (docs/ops/ru-node.md, «Переменные»)"
	[ "$(stat -c '%u %a' "$ENV_FILE")" = "0 600" ] || die "$ENV_FILE must be root's, mode 0600"
}

installed() {
	[ "$(dpkg-query -W -f='${db:Status-Status}' "$1" 2>/dev/null)" = installed ]
}

install_packages() {
	local p u missing=()
	for p in "${PACKAGES[@]}"; do
		installed "$p" || missing+=("$p")
	done
	for u in "${PACKAGE_UNITS[@]}"; do
		if systemctl is-active --quiet "$u"; then
			die "$u of the package runs on this host and takes the ports of Pluma; stop it or ask its owner"
		fi
		# Masked before the package is installed, so that its postinst does
		# not start it: deb-systemd-helper unmask removes only a mask it made
		# itself (Ubuntu 26.04, /usr/bin/deb-systemd-helper, unmask_service),
		# and the start of a masked unit fails (the postinst ignores that).
		[ "$(systemctl is-enabled "$u" 2>/dev/null || true)" = masked ] || systemctl mask --quiet "$u"
	done
	if [ ${#missing[@]} -gt 0 ]; then
		say "installing ${missing[*]}"
		# Only the missing ones: apt-get install of an installed package
		# would upgrade it, and the owner's nginx with it.
		DEBIAN_FRONTEND=noninteractive apt-get update -qq
		DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends "${missing[@]}" >/dev/null
	fi
}

make_layout() {
	if ! getent passwd pluma-ru >/dev/null; then
		useradd --system --user-group --home-dir /nonexistent --no-create-home --shell /usr/sbin/nologin pluma-ru
	fi
	install -d -m 0755 "$ETC" /opt/pluma-ru/bin /var/lib/pluma-ru /var/lib/pluma-ru/acme \
		/var/lib/pluma-ru/textfile /var/log/pluma-ru
	install -d -m 0700 /var/lib/pluma-ru/certbot /var/log/pluma-ru/certbot /var/lib/pluma-ru/owner-nginx-backup
	install -d -m 0700 -o pluma-ru -g pluma-ru /var/cache/pluma-ru /var/cache/pluma-ru/files /var/cache/pluma-ru/releases \
		/var/lib/pluma-ru/nginx
}

CHANGED=()
# The directory apply renders into, removed on exit.
STAGING=
# put <new file> <destination> <mode> <owner:group> <tag>: moves the file
# into place when it differs; the tag goes to CHANGED.
put() {
	if [ -f "$2" ] && cmp -s "$1" "$2" &&
		[ "$(stat -c '%a %U:%G' "$2")" = "$(printf '%o' "0$3") $4" ]; then
		rm -f "$1"
		return
	fi
	chmod "$3" "$1"
	chown "$4" "$1"
	mv -f "$1" "$2"
	CHANGED+=("$5")
}

changed() {
	case " ${CHANGED[*]} " in *" $1 "*) return 0 ;; esac
	return 1
}

cert_path() {
	echo "$LE_DIR/live/${ENV[PLUMA_RU_FILES_DOMAIN]}/fullchain.pem"
}

# check_nginx <directory>: nginx -t on a rendered set in its own directory,
# before it replaces the running one (nginx.conf includes relative to
# itself). Before the first certificate of delivery 202 the same files are
# checked with a throwaway self-signed one in place of it; that set is never
# served.
check_nginx() {
	# Called in a condition, where set -e is off: every step is checked.
	local dir=$1 chk rc=0
	if [ -f "$(cert_path)" ]; then
		nginx -t -q -c "$dir/nginx.conf" -g 'daemon off;'
		return
	fi
	chk=$dir/check
	mkdir -m 0700 "$chk" || return
	openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -subj /CN=nginx-check -days 1 \
		-keyout "$chk/key.pem" -out "$chk/cert.pem" 2>/dev/null || return
	cp "$dir/nginx.conf" "$dir/secrets.conf" "$dir/stream.conf" "$chk/" || return
	sed -e "s#^\([[:space:]]*ssl_certificate\) .*#\1 $chk/cert.pem;#" \
		-e "s#^\([[:space:]]*ssl_certificate_key\) .*#\1 $chk/key.pem;#" "$dir/files.conf" >"$chk/files.conf" || return
	nginx -t -q -c "$chk/nginx.conf" -g 'daemon off;' || rc=$?
	rm -rf "$chk"
	return $rc
}

apply() {
	require_root
	check_env_file
	[ -f "$HERE/nginx.conf" ] || die "run apply from the checkout (/opt/pluma-ru/src/teamgramd/deploy/ru/deploy.sh)"
	install_packages
	make_layout

	local staging have_cert=0 u f
	staging=$(mktemp -d "$ETC/.staging.XXXXXX")
	STAGING=$staging
	trap 'rm -rf "$STAGING"' EXIT
	render "$ENV_FILE" "$staging"
	[ -f "$(cert_path)" ] && have_cert=1
	check_nginx "$staging" || die "nginx -t failed on the new files; nothing changed"

	put "$staging/nginx.conf" "$ETC/nginx.conf" 644 root:root nginx
	put "$staging/files.conf" "$ETC/files.conf" 644 root:root nginx
	put "$staging/stream.conf" "$ETC/stream.conf" 644 root:root nginx
	put "$staging/secrets.conf" "$ETC/secrets.conf" 600 root:root nginx
	put "$staging/turn.conf" "$ETC/turn.conf" 640 root:turnserver turn
	install -d -m 0755 "$ETC/owner"
	for f in pluma-upstreams.conf pluma-sni.map pluma-acme-http.conf; do
		put "$staging/owner/$f" "$ETC/owner/$f" 644 root:root owner
	done
	cp "$HERE/wgstat.sh" "$staging/wgstat.sh"
	put "$staging/wgstat.sh" /opt/pluma-ru/bin/wgstat.sh 755 root:root wgstat
	for u in "${UNITS[@]}"; do
		cp "$HERE/$u" "$staging/$u"
		put "$staging/$u" "/etc/systemd/system/$u" 644 root:root "unit:$u"
	done
	[[ " ${CHANGED[*]} " == *" unit:"* ]] && systemctl daemon-reload

	# TURN: a restart drops the relays of calls in progress, so only when
	# its configuration or unit changed.
	systemctl enable --quiet pluma-ru-turn.service
	if changed turn || changed unit:pluma-ru-turn.service; then
		say "restarting pluma-ru-turn (relayed calls in progress reconnect)"
		systemctl restart pluma-ru-turn.service
	else
		systemctl start pluma-ru-turn.service
	fi

	systemctl enable --quiet pluma-ru-node-exporter.service
	if changed unit:pluma-ru-node-exporter.service; then
		systemctl restart pluma-ru-node-exporter.service
	else
		systemctl start pluma-ru-node-exporter.service
	fi
	ip -4 -o addr show dev plumaru 2>/dev/null | grep -q ' 10\.78\.0\.1/' ||
		say "WARNING: no 10.78.0.1 on plumaru: node-exporter waits for the tunnel (tunnel.sh --link ru node)"

	systemctl enable --quiet --now pluma-ru-wgstat.timer pluma-ru-certbot.timer
	# A timer takes a new schedule only when it is started again.
	for u in pluma-ru-wgstat.timer pluma-ru-certbot.timer; do
		if changed "unit:$u"; then
			systemctl restart "$u"
		fi
	done
	systemctl start pluma-ru-wgstat.service

	if [ $have_cert = 1 ]; then
		systemctl enable --quiet pluma-ru-nginx.service
		if ! systemctl is-active --quiet pluma-ru-nginx.service || changed unit:pluma-ru-nginx.service; then
			systemctl restart pluma-ru-nginx.service
		elif changed nginx; then
			systemctl reload pluma-ru-nginx.service
		fi
	else
		say "no certificate for ${ENV[PLUMA_RU_FILES_DOMAIN]} yet: pluma-ru-nginx waits for deploy.sh cert-register and cert-issue"
	fi

	if changed owner; then
		say "the files for the owner's nginx changed: $HERE/owner-nginx.sh check, then install"
	fi
	say "at $(git -C "$HERE" log --oneline -1 2>/dev/null || echo 'an unknown commit')"
	for u in pluma-ru-nginx pluma-ru-turn pluma-ru-node-exporter pluma-ru-wgstat.timer pluma-ru-certbot.timer; do
		say "  $u: $(systemctl is-active "$u" || true)"
	done
}

# ---------------------------------------------------------------------------
# Certificates of delivery 202: certbot of the package with a configuration
# directory of its own, so that the owner's account and renewals stay apart
# and the CAA record of the name can allow this account only.

cert_register() {
	require_root
	check_env_file
	load_env "$ENV_FILE"
	check_env
	make_layout
	local email=(--register-unsafely-without-email)
	[ -z "${ENV[PLUMA_RU_ACME_EMAIL]:-}" ] || email=(-m "${ENV[PLUMA_RU_ACME_EMAIL]}" --no-eff-email)
	if ! certbot show_account "${CERTBOT_DIRS[@]}" >/dev/null 2>&1; then
		certbot register -n --agree-tos "${email[@]}" "${CERTBOT_DIRS[@]}"
	fi
	say "the CAA record of ${ENV[PLUMA_RU_FILES_DOMAIN]} (docs/ops/ru-node.md, «Сертификат»):"
	certbot show_account "${CERTBOT_DIRS[@]}" 2>/dev/null | sed -n 's/^.*Account URL: *//p' |
		sed "s#^#  ${ENV[PLUMA_RU_FILES_DOMAIN]}. CAA 0 issue \"letsencrypt.org; accounturi=#; s#\$#; validationmethods=http-01\"#"
}

cert_issue() {
	require_root
	check_env_file
	load_env "$ENV_FILE"
	check_env
	[ -f /etc/nginx/conf.d/pluma-acme-http.conf ] ||
		die "the owner's nginx does not serve the challenges yet: owner-nginx.sh install first"
	certbot certonly -n --webroot -w /var/lib/pluma-ru/acme -d "${ENV[PLUMA_RU_FILES_DOMAIN]}" \
		--key-type ecdsa --elliptic-curve secp256r1 \
		--deploy-hook 'systemctl try-reload-or-restart pluma-ru-nginx.service' \
		"${CERTBOT_DIRS[@]}"
	nginx -t -q -c "$ETC/nginx.conf" -g 'daemon off;' || die "nginx -t failed: run deploy.sh apply again"
	systemctl enable --quiet pluma-ru-nginx.service
	systemctl restart pluma-ru-nginx.service
	say "pluma-ru-nginx: $(systemctl is-active pluma-ru-nginx.service || true)"
}

# ---------------------------------------------------------------------------
# From stdin: the checkout at NEW, then its own deploy.sh.

bootstrap() {
	require_root
	[ -n "${NEW:-}" ] || die "NEW=<commit> is required: ssh amf 'NEW=<sha> bash -s' < teamgramd/deploy/ru/deploy.sh"
	[[ $NEW =~ ^[0-9a-f]{7,40}$ ]] || die "NEW must be a commit hash"
	if ! command -v git >/dev/null; then
		DEBIAN_FRONTEND=noninteractive apt-get update -qq
		DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends git ca-certificates >/dev/null
	fi
	install -d -m 0755 /opt/pluma-ru
	[ -d "$SRC/.git" ] || git clone -q "$REPO" "$SRC"
	git -C "$SRC" fetch -q origin
	git -C "$SRC" checkout -q --detach "$NEW"
	exec bash "$SRC/teamgramd/deploy/ru/deploy.sh" apply
}

case ${1:-bootstrap} in
bootstrap) bootstrap ;;
apply) apply ;;
cert-register) cert_register ;;
cert-issue) cert_issue ;;
render)
	[ $# -ge 3 ] || die "usage: deploy.sh render <env file> <directory> [--cache-status-header]"
	render "$2" "$3" "${4:-}"
	;;
*) die "unknown command $1 (bootstrap, apply, cert-register, cert-issue, render)" ;;
esac
