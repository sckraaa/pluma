#!/usr/bin/env bash
# Prepares a server for calls (docs/ops/calls.md), once, as root, in the
# directory of .env: makes the TURN secret if there is none, writes the call
# variables and opens the firewall. The secret is never printed.
#
#   teamgramd/deploy/turn/setup.sh <public IPv4 of the node>
set -euo pipefail
[ $# = 1 ] || { echo "usage: $0 <public IPv4>" >&2; exit 2; }
ip=$1
[[ $ip =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "setup: $ip is not an IPv4 address" >&2; exit 2; }
[ -f .env ] || { echo "setup: no .env here" >&2; exit 1; }

# set_var NAME VALUE: replaces the line of NAME in .env, or adds one.
set_var() {
	local tmp
	tmp=$(mktemp .env.XXXXXX)
	grep -v "^$1=" .env >"$tmp" || true
	printf '%s=%s\n' "$1" "$2" >>"$tmp"
	chmod --reference=.env "$tmp"
	chown --reference=.env "$tmp"
	mv -f "$tmp" .env
}

if ! grep -Eq '^CALLS_TURN_SECRET=.+' .env; then
	set_var CALLS_TURN_SECRET "$(openssl rand -hex 32)"
	echo "setup: CALLS_TURN_SECRET made"
fi
set_var TURN_PUBLIC_IPV4 "$ip"
set_var CALLS_ICE_SERVERS "$ip:3478:stun+turn"

ufw allow 3478/udp comment 'Pluma TURN'
ufw allow 3478/tcp comment 'Pluma TURN'
ufw allow 49160:49659/udp comment 'Pluma TURN relays'
echo "setup: done; if the provider has a firewall of its own, open the same ports there"
