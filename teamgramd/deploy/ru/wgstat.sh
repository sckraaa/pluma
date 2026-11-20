#!/usr/bin/env bash
# Writes the textfile metrics of the Russian node (task 109,
# docs/ops/ru-node.md) for pluma-ru-node-exporter; pluma-ru-wgstat.timer
# runs it every minute, as root (wg show needs CAP_NET_ADMIN):
#
#   pluma_wg_up{interface}                        1 while the interface of the tunnel exists
#   pluma_wg_latest_handshake_seconds{interface}  Unix time of the last handshake with home, 0 for none
#   pluma_ru_cache_bytes                          disk the cache of delivery 202 takes
#   pluma_ru_cache_max_bytes                      its max_size in files.conf
#   pluma_ru_unit_active{unit}                    1 while pluma-ru-nginx runs (systemctl is-active)
#   pluma_ru_sfu_upstream_up                      1 when the SFU behind the relay of group calls
#                                                 takes a TCP connection (only with the relay, task 116)
#   pluma_ru_stats_timestamp_seconds              when this file was written
#
# The file is replaced at once (a temporary file and mv in its directory),
# so node-exporter never reads half of it; it reads only *.prom.
#
#   wgstat.sh <output.prom>
set -euo pipefail
out=${1:?usage: wgstat.sh <output.prom>}
iface=${PLUMA_RU_WG_IFACE:-plumaru}
cache=${PLUMA_RU_CACHE_DIR:-/var/cache/pluma-ru/files}
conf=${PLUMA_RU_FILES_CONF:-/etc/pluma-ru/files.conf}
stream=${PLUMA_RU_STREAM_CONF:-/etc/pluma-ru/stream.conf}

# size_bytes <nginx size>: 15g → bytes (nginx sizes: k, m, g, or none).
size_bytes() {
	local n=${1%[kKmMgG]}
	case $1 in
	*[kK]) echo $((n * 1024)) ;;
	*[mM]) echo $((n * 1024 * 1024)) ;;
	*[gG]) echo $((n * 1024 * 1024 * 1024)) ;;
	*) echo "$n" ;;
	esac
}

up=0
handshake=0
if hs=$(wg show "$iface" latest-handshakes 2>/dev/null); then
	up=1
	# One line per peer, "<public key>\t<Unix time>"; home is the only peer.
	while read -r _ t; do
		if [[ $t =~ ^[0-9]+$ ]] && ((t > handshake)); then
			handshake=$t
		fi
	done <<<"$hs"
fi

# The SFU's ICE-TCP port of the relay, <IPv4>:<port> of the upstream
# pluma_sfu_tcp in stream.conf; empty without the relay. UDP cannot be
# checked so: the SFU answers no STUN without the credentials of a call.
sfu=
if [ -r "$stream" ]; then
	sfu=$(sed -n '/^[[:space:]]*upstream pluma_sfu_tcp {/,/}/s/^[[:space:]]*server \([0-9.]*:[0-9]*\);.*/\1/p' "$stream" | head -n 1)
fi
sfu_up=0
# shellcheck disable=SC2016 # $1 and $2 belong to the inner bash: the address comes as arguments
if [ -n "$sfu" ] && timeout 5 bash -c 'exec 3<>"/dev/tcp/$1/$2"' _ "${sfu%:*}" "${sfu##*:}" 2>/dev/null; then
	sfu_up=1
fi
nginx_active=0
have_systemctl=0
if command -v systemctl >/dev/null; then
	have_systemctl=1
	if systemctl is-active --quiet pluma-ru-nginx.service; then
		nginx_active=1
	fi
fi

tmp=$(mktemp --suffix=.tmp "$out.XXXXXX")
trap 'rm -f "$tmp"' EXIT
{
	echo '# HELP pluma_wg_up Whether the WireGuard interface of the tunnel to home exists.'
	echo '# TYPE pluma_wg_up gauge'
	echo "pluma_wg_up{interface=\"$iface\"} $up"
	echo '# HELP pluma_wg_latest_handshake_seconds Unix time of the last handshake with home, 0 for none.'
	echo '# TYPE pluma_wg_latest_handshake_seconds gauge'
	echo "pluma_wg_latest_handshake_seconds{interface=\"$iface\"} $handshake"
	if [ -d "$cache" ]; then
		echo '# HELP pluma_ru_cache_bytes Disk the cache of delivery 202 takes.'
		echo '# TYPE pluma_ru_cache_bytes gauge'
		echo "pluma_ru_cache_bytes $(du -s -B1 "$cache" | cut -f1)"
	fi
	if max=$(grep -o 'max_size=[0-9]*[kKmMgG]\?' "$conf" 2>/dev/null | head -n 1) && [ -n "$max" ]; then
		echo '# HELP pluma_ru_cache_max_bytes The max_size of the cache of delivery 202.'
		echo '# TYPE pluma_ru_cache_max_bytes gauge'
		echo "pluma_ru_cache_max_bytes $(size_bytes "${max#max_size=}")"
	fi
	if [ $have_systemctl = 1 ]; then
		echo '# HELP pluma_ru_unit_active Whether a unit of the node is active.'
		echo '# TYPE pluma_ru_unit_active gauge'
		echo "pluma_ru_unit_active{unit=\"pluma-ru-nginx\"} $nginx_active"
	fi
	if [ -n "$sfu" ]; then
		echo '# HELP pluma_ru_sfu_upstream_up Whether the SFU behind the relay of group calls takes a TCP connection.'
		echo '# TYPE pluma_ru_sfu_upstream_up gauge'
		echo "pluma_ru_sfu_upstream_up $sfu_up"
	fi
	echo '# HELP pluma_ru_stats_timestamp_seconds When these metrics were written.'
	echo '# TYPE pluma_ru_stats_timestamp_seconds gauge'
	echo "pluma_ru_stats_timestamp_seconds $(date +%s)"
} >"$tmp"
chmod 0644 "$tmp"
mv -f "$tmp" "$out"
trap - EXIT
