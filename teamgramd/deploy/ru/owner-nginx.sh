#!/usr/bin/env bash
# The part of Pluma in the owner's nginx on the Russian node (task 109,
# docs/ops/ru-node.md), as root. The owner's nginx shares port 443 by name
# (SNI) in a stream map and serves port 80; Pluma adds only its own files
# and one line:
#
#   /etc/nginx/stream.d/pluma-upstreams.conf  the upstreams of the edge and of delivery 202
#   /etc/nginx/stream.d/pluma-sni.map         the names → those upstreams (not *.conf:
#                                             stream.d/*.conf is included at the level of stream)
#   /etc/nginx/conf.d/pluma-acme-http.conf    port 80: the ACME challenges of both names
#   include /etc/nginx/stream.d/pluma-sni.map;  one line in the owner's map of $ssl_preread_server_name
#
# The files come from /etc/pluma-ru/owner, written by deploy.sh apply.
#
#   owner-nginx.sh check [--dump <nginx -T output> --version <nginx -V output>]
#       reads the owner's configuration (nginx -T) and modules (nginx -V)
#       and says whether install can work, or whether it is installed;
#       changes nothing; with the files given, reads them instead (tests)
#   owner-nginx.sh install
#       check, the files, the line; nginx -t, then reload of the owner's
#       nginx; on any failure the owner's files are put back as they were
#   owner-nginx.sh remove
#       the line and the files out; nginx -t, reload
set -euo pipefail
umask 022

NGINX_ETC=${PLUMA_RU_NGINX_ETC:-/etc/nginx}
PLUMA_ETC=${PLUMA_RU_ETC:-/etc/pluma-ru}
BACKUP_DIR=${PLUMA_RU_OWNER_BACKUP:-/var/lib/pluma-ru/owner-nginx-backup}

UPSTREAMS=$NGINX_ETC/stream.d/pluma-upstreams.conf
SNI_MAP=$NGINX_ETC/stream.d/pluma-sni.map
ACME=$NGINX_ETC/conf.d/pluma-acme-http.conf
INCLUDE_LINE="include $SNI_MAP;"

die() {
	echo "owner-nginx: $*" >&2
	exit 1
}

# flatten: nginx -T on stdin → one line per directive, "<context>\t<file>\t
# <directive>". The context is the chain of blocks, each "<header>#<n>" (n
# numbers the blocks, so that two servers are told apart), joined by " > ".
# nginx -T prints every file it read once, after a "# configuration file
# <path>:" line, with the include directives as written; the content of an
# included file is walked where its include is, so that its context is
# known. A block opening is a directive of its own, ending in "{".
flatten() {
	awk -v main="$NGINX_ETC/nginx.conf" '
	function glob_re(p,   r, i, c) {
		if (substr(p, 1, 1) != "/") {
			p = prefix "/" p
		}
		r = "^"
		for (i = 1; i <= length(p); i++) {
			c = substr(p, i, 1)
			if (c == "*") r = r "[^/]*"
			else if (c == "?") r = r "[^/]"
			else if (index(".[]()+^$|{}\\", c)) r = r "\\" c
			else r = r c
		}
		return r "$"
	}
	function emit(ctx, file, stmt) {
		gsub(/[ \t\r\n]+/, " ", stmt)
		sub(/^ /, "", stmt)
		sub(/ $/, "", stmt)
		if (stmt != "") print ctx "\t" file "\t" stmt
		return stmt
	}
	function walk(file, ctx, depth,   s, i, c, q, stmt, sp, stack, f, re, word) {
		if (depth > 16) {
			print "owner-nginx: includes nest deeper than 16 at " file > "/dev/stderr"
			exit 2
		}
		s = content[file]
		stmt = ""
		q = ""
		sp = 0
		for (i = 1; i <= length(s); i++) {
			c = substr(s, i, 1)
			if (q != "") {
				stmt = stmt c
				if (c == "\\") { i++; stmt = stmt substr(s, i, 1); continue }
				if (c == q) q = ""
				continue
			}
			if (c == "\"" || c == sq) { q = c; stmt = stmt c; continue }
			if (c == "#") {
				while (i <= length(s) && substr(s, i, 1) != "\n") i++
				stmt = stmt " "
				continue
			}
			if (c == ";") {
				stmt = emit(ctx, file, stmt)
				split(stmt, word, " ")
				if (word[1] == "include" && word[2] != "") {
					re = glob_re(word[2])
					for (f = 1; f <= nfiles; f++) {
						if (names[f] ~ re) walk(names[f], ctx, depth + 1)
					}
				}
				stmt = ""
				continue
			}
			if (c == "{") {
				stmt = emit(ctx, file, stmt " {")
				sub(/ \{$/, "", stmt)
				stack[++sp] = ctx
				blocks++
				ctx = (ctx == "" ? "" : ctx " > ") stmt "#" blocks
				stmt = ""
				continue
			}
			if (c == "}") {
				if (sp > 0) ctx = stack[sp--]
				stmt = ""
				continue
			}
			stmt = stmt c
		}
	}
	BEGIN { sq = sprintf("%c", 39) }
	/^# configuration file .*:$/ {
		cur = $0
		sub(/^# configuration file /, "", cur)
		sub(/:$/, "", cur)
		names[++nfiles] = cur
		content[cur] = ""
		next
	}
	cur != "" { content[cur] = content[cur] $0 "\n" }
	END {
		prefix = main
		sub(/\/[^\/]*$/, "", prefix)
		if (!(main in content)) {
			print "owner-nginx: nginx -T has no " main > "/dev/stderr"
			exit 2
		}
		walk(main, "", 0)
	}'
}

# The flattened configuration and nginx -V, set by load.
FLAT=
VERSION=
load() {
	local dump=${1:-} version=${2:-} raw
	if [ -n "$dump" ]; then
		raw=$(<"$dump")
		VERSION=$(<"$version")
	else
		raw=$(nginx -T 2>/dev/null) || die "nginx -T fails: the owner's configuration is broken now; nothing to do until it is fixed"
		VERSION=$(nginx -V 2>&1)
	fi
	FLAT=$(flatten <<<"$raw") || die "cannot read the output of nginx -T"
}

# Directives of FLAT as "context\tfile\tdirective" lines:
#   rows <context regex> <directive regex>
#   rows_in <context> <directive regex>
# The regexes go through the environment: awk -v would take their
# backslashes as escapes.
rows() {
	C=$1 D=$2 awk -F '\t' '$1 ~ ENVIRON["C"] && $3 ~ ENVIRON["D"]' <<<"$FLAT"
}
rows_in() {
	C=$1 D=$2 awk -F '\t' '$1 == ENVIRON["C"] && $3 ~ ENVIRON["D"]' <<<"$FLAT"
}

# glob_match <include pattern> <path>: whether nginx's include takes path;
# a relative pattern is relative to the directory of nginx.conf.
glob_match() {
	local p=$1
	[ "${p#/}" != "$p" ] || p=$NGINX_ETC/$p
	# shellcheck disable=SC2053 # the pattern is meant to be a glob
	[[ $2 == $p ]]
}

FAILS=0
ok() { echo "ok    $*"; }
fail() {
	echo "FAIL  $*"
	FAILS=$((FAILS + 1))
}

# The names Pluma routes, from the rendered map.
pluma_names() {
	[ -r "$PLUMA_ETC/owner/pluma-sni.map" ] || return 0
	awk '!/^[[:space:]]*(#|$)/ { print $1 }' "$PLUMA_ETC/owner/pluma-sni.map"
}

# Set by check: the owner's SNI map (file, variable) and whether the line is in.
MAP_FILE=
MAP_VAR=
INSTALLED=0

check() {
	local dump='' version='' m n line server listen inc_ok name
	while [ $# -gt 0 ]; do
		case $1 in
		--dump) dump=$2 && shift 2 ;;
		--version) version=$2 && shift 2 ;;
		*) die "check: unknown option $1" ;;
		esac
	done
	{ [ -z "$dump" ] && [ -z "$version" ]; } || { [ -n "$dump" ] && [ -n "$version" ]; } ||
		die "check: --dump and --version go together"
	load "$dump" "$version"
	FAILS=0

	# The modules delivery 202 needs (they are not built by default:
	# https://nginx.org/en/docs/http/ngx_http_secure_link_module.html,
	# https://nginx.org/en/docs/http/ngx_http_slice_module.html).
	for m in http_ssl_module http_v2_module http_realip_module http_secure_link_module http_slice_module; do
		if grep -q -- "--with-$m" <<<"$VERSION"; then ok "nginx has $m"; else fail "nginx is built without $m"; fi
	done

	# Exactly one map of the SNI in stream, its header on one line.
	# shellcheck disable=SC2016 # nginx variables in a regex
	m=$(rows '^stream#[0-9]+$' '^map \$ssl_preread_server_name \$[A-Za-z0-9_]+ \{$')
	n=$(grep -c . <<<"$m" || true)
	if [ "$n" != 1 ]; then
		fail "want one map \$ssl_preread_server_name in stream {}, found $n"
	else
		MAP_FILE=$(cut -f2 <<<"$m")
		MAP_VAR=$(cut -f3 <<<"$m" | awk '{ print $3 }')
		ok "the SNI map of $MAP_VAR is in $MAP_FILE"
		n=$(grep -cE "^[[:space:]]*map[[:space:]]+\\\$ssl_preread_server_name[[:space:]]+\\${MAP_VAR}[[:space:]]*\\{" "$MAP_FILE" 2>/dev/null || true)
		if [ -z "$dump" ] && [ "$n" != 1 ]; then
			fail "$MAP_FILE: the map header is not on one line of its own; add the line \"$INCLUDE_LINE\" into the map by hand"
		fi
		if rows "^stream#[0-9]+ > map \\\$ssl_preread_server_name \\$MAP_VAR#[0-9]+\$" "^include ${SNI_MAP//./\\.}\$" | grep -q .; then
			INSTALLED=1
		fi
	fi

	# The stream server that reads the SNI: it must listen on 443 of every
	# address (home checks delivery 202 at 10.78.0.1:443 through the
	# tunnel), send PROXY to the upstreams and route by the map.
	server=
	if [ -n "$MAP_VAR" ]; then
		server=$(rows '^stream#[0-9]+ > server#[0-9]+$' "^proxy_pass \\$MAP_VAR\$" | cut -f1 | sort -u)
	fi
	if [ "$(grep -c . <<<"$server" || true)" != 1 ]; then
		fail "want one stream server with proxy_pass ${MAP_VAR:-<the map>}"
	else
		ok "the stream server routes by $MAP_VAR"
		if rows_in "$server" '^ssl_preread on$' | grep -q .; then ok "ssl_preread on"; else fail "the stream server has no ssl_preread on"; fi
		if rows_in "$server" '^proxy_protocol on$' | grep -q .; then
			ok "the stream server sends PROXY to its upstreams"
		else
			fail "the stream server has no proxy_protocol on: the edge and delivery 202 take PROXY v1 only"
		fi
		listen=$(rows_in "$server" '^listen ' | cut -f3)
		if grep -qE '^listen (443|\*:443|0\.0\.0\.0:443)( |$)' <<<"$listen"; then
			ok "the stream server listens on 443 of every IPv4 address"
		else
			fail "the stream server does not listen on 443 of every IPv4 address (home reaches it at 10.78.0.1:443): $(tr '\n' ' ' <<<"$listen")"
		fi
	fi

	# stream.d/*.conf in stream {} brings pluma-upstreams.conf; conf.d/*.conf
	# in http {} brings pluma-acme-http.conf.
	inc_ok=0
	while IFS=$'\t' read -r _ _ line; do
		glob_match "${line#include }" "$UPSTREAMS" && inc_ok=1
	done < <(rows '^stream#[0-9]+$' '^include ')
	if [ $inc_ok = 1 ]; then ok "stream {} includes $UPSTREAMS"; else fail "no include in stream {} takes $UPSTREAMS"; fi
	inc_ok=0
	while IFS=$'\t' read -r _ _ line; do
		glob_match "${line#include }" "$SNI_MAP" && inc_ok=1
	done < <(rows '^stream#[0-9]+$' '^include ')
	if [ $inc_ok = 1 ]; then fail "an include in stream {} takes $SNI_MAP too: it would be read outside the map"; fi
	inc_ok=0
	while IFS=$'\t' read -r _ _ line; do
		glob_match "${line#include }" "$ACME" && inc_ok=1
	done < <(rows '^http#[0-9]+$' '^include ')
	if [ $inc_ok = 1 ]; then ok "http {} includes $ACME"; else fail "no include in http {} takes $ACME"; fi

	# The names of Pluma are nobody else's: neither in the owner's map nor
	# as a server_name of the owner's http servers.
	while read -r name; do
		[ -n "$name" ] || continue
		if rows '^stream#[0-9]+ > map ' "^${name//./\\.} " | awk -F '\t' -v f="$SNI_MAP" '$2 != f' | grep -q .; then
			fail "the owner's map already routes $name"
		fi
		if rows '^http#[0-9]+ > server#[0-9]+$' '^server_name ' | awk -F '\t' -v f="$ACME" -v n="$name" '
			$2 != f { k = split($3, w, " "); for (i = 2; i <= k; i++) if (w[i] == n) found = 1 }
			END { exit !found }'; then
			fail "an http server of the owner already has server_name $name"
		fi
	done < <(pluma_names)

	if [ $INSTALLED = 1 ]; then
		echo "state installed"
	else
		echo "state not installed"
	fi
	if [ $FAILS -gt 0 ]; then
		echo "owner-nginx: $FAILS check(s) failed" >&2
		return 1
	fi
}

reload_owner() {
	if [ -d /run/systemd/system ]; then
		systemctl reload nginx.service
	else
		nginx -s reload
	fi
}

# restore <backup of the map file>: the owner's files as before install.
restore() {
	echo "owner-nginx: putting the owner's files back" >&2
	cat "$1" >"$MAP_FILE"
	rm -f "$UPSTREAMS" "$SNI_MAP" "$ACME"
	for f in "$UPSTREAMS" "$SNI_MAP" "$ACME"; do
		if [ -f "$BACKUP_DIR/last/$(basename "$f")" ]; then
			cp -p "$BACKUP_DIR/last/$(basename "$f")" "$f"
		fi
	done
	nginx -t -q || echo "owner-nginx: nginx -t fails even with the owner's files back; look at once" >&2
}

install_files() {
	[ "$(id -u)" = 0 ] || die "run as root"
	local f backup tmp n
	for f in pluma-upstreams.conf pluma-sni.map pluma-acme-http.conf; do
		[ -s "$PLUMA_ETC/owner/$f" ] || die "$PLUMA_ETC/owner/$f is missing: deploy.sh apply first"
	done
	check || die "check failed: nothing changed"

	install -d -m 0700 "$BACKUP_DIR" "$BACKUP_DIR/last"
	backup=$BACKUP_DIR/$(basename "$MAP_FILE").$(date -u +%Y%m%dT%H%M%SZ)
	cp -p "$MAP_FILE" "$backup"
	rm -f "$BACKUP_DIR/last/"*
	for f in "$UPSTREAMS" "$SNI_MAP" "$ACME"; do
		[ ! -f "$f" ] || cp -p "$f" "$BACKUP_DIR/last/"
	done

	install -m 0644 "$PLUMA_ETC/owner/pluma-upstreams.conf" "$UPSTREAMS"
	install -m 0644 "$PLUMA_ETC/owner/pluma-sni.map" "$SNI_MAP"
	install -m 0644 "$PLUMA_ETC/owner/pluma-acme-http.conf" "$ACME"
	if [ $INSTALLED = 0 ]; then
		# The line goes right after the map header; the file keeps its inode,
		# owner and mode (cat >, not mv).
		tmp=$(mktemp "$BACKUP_DIR/map.XXXXXX")
		n=$(grep -nE "^[[:space:]]*map[[:space:]]+\\\$ssl_preread_server_name[[:space:]]+\\${MAP_VAR}[[:space:]]*\\{" "$MAP_FILE" | cut -d: -f1)
		awk -v n="$n" -v l="    $INCLUDE_LINE" '{ print } NR == n { print l }' "$MAP_FILE" >"$tmp"
		cat "$tmp" >"$MAP_FILE"
		rm -f "$tmp"
	fi
	if ! nginx -t -q; then
		restore "$backup"
		die "nginx -t failed with the files of Pluma: the owner's configuration is as it was"
	fi
	if ! reload_owner; then
		restore "$backup"
		reload_owner || true
		die "the reload of the owner's nginx failed: the owner's configuration is as it was"
	fi
	sleep 1
	nginx -T 2>/dev/null | grep -qF "# configuration file $SNI_MAP:" || die "nginx -T does not read $SNI_MAP after the reload"
	echo "owner-nginx: installed; the backup of $MAP_FILE is $backup"
}

remove_files() {
	[ "$(id -u)" = 0 ] || die "run as root"
	local backup tmp f
	check >/dev/null || true
	[ -n "$MAP_FILE" ] || die "the owner's SNI map is not found: remove the line \"$INCLUDE_LINE\" by hand"
	install -d -m 0700 "$BACKUP_DIR" "$BACKUP_DIR/last"
	backup=$BACKUP_DIR/$(basename "$MAP_FILE").$(date -u +%Y%m%dT%H%M%SZ)
	cp -p "$MAP_FILE" "$backup"
	rm -f "$BACKUP_DIR/last/"*
	for f in "$UPSTREAMS" "$SNI_MAP" "$ACME"; do
		[ ! -f "$f" ] || cp -p "$f" "$BACKUP_DIR/last/"
	done
	tmp=$(mktemp "$BACKUP_DIR/map.XXXXXX")
	grep -vxF "    $INCLUDE_LINE" "$MAP_FILE" >"$tmp" || true
	cat "$tmp" >"$MAP_FILE"
	rm -f "$tmp" "$UPSTREAMS" "$SNI_MAP" "$ACME"
	if ! nginx -t -q; then
		restore "$backup"
		die "nginx -t failed without the files of Pluma: they are back"
	fi
	reload_owner || die "the reload of the owner's nginx failed: the files of Pluma are out, nginx -t passes; look at systemctl status nginx"
	echo "owner-nginx: removed; the backup of $MAP_FILE is $backup"
}

case ${1:-} in
check)
	shift
	check "$@"
	;;
install) install_files ;;
remove) remove_files ;;
*) die "usage: owner-nginx.sh check [--dump <file> --version <file>] | install | remove" ;;
esac
