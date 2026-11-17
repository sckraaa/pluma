#!/usr/bin/env bash
# Checks tunnel.sh by its dry run (task 109): for each link and role, the
# files it writes and the commands it would run on setup and on the rules
# unit's start and stop are compared with testdata/<link>-<role>/. The
# hetzner cases run without --link, as the units and the operators call it,
# and with --link hetzner, which must be the same. A dry run must not run
# one command that changes the host: the stubs on PATH would log it.
#
#   tunnel_test.sh            compare
#   tunnel_test.sh -u         rewrite testdata from the current script
#
# Needs bash, diff and the usual tools; no root, no WireGuard, no network.
set -Eeuo pipefail
# A command that fails outside the checks ends the test, never silently.
trap 'echo "FAIL: tunnel_test.sh:$LINENO: $BASH_COMMAND exited $?" >&2' ERR

HERE=$(cd "$(dirname "$0")" && pwd -P)
SCRIPT=$HERE/tunnel.sh
TESTDATA=$HERE/testdata
UPDATE=
[ "${1:-}" = -u ] && UPDATE=1

# Throwaway values of the right form, made up for the test: the peer's
# public key, the key pair the stub wg "generates", and documentation
# addresses (RFC 5737) for the hosts.
PEER_KEY=UGVlcktleU9mVGhlVGVzdE9ubHlOb3RBUmVhbEtleTA=
STUB_PRIVATE=U3R1YlByaXZhdGVLZXlPZlRoZVRlc3RPbmx5MDAwMDA=
STUB_PUBLIC=U3R1YlB1YmxpY0tleU9mVGhlVGVzdE9ubHkwMDAwMDA=
OWN_IP=198.51.100.7
PEER_IP=203.0.113.9

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
stubs=$work/bin
mkdir -p "$stubs"
LOG=$work/host-changes
: >"$LOG"
export LOG STUB_PRIVATE STUB_PUBLIC OWN_IP

# Read-only queries answer; anything that would change the host is logged.
cat >"$stubs/wg" <<'EOF'
#!/usr/bin/env bash
case $1 in
genkey) echo "$STUB_PRIVATE" ;;
pubkey) cat >/dev/null; echo "$STUB_PUBLIC" ;;
*) echo "wg $*" >>"$LOG" ;;
esac
EOF
cat >"$stubs/ip" <<'EOF'
#!/usr/bin/env bash
case "$*" in
"-4 route get 1.1.1.1") echo "1.1.1.1 via 192.0.2.1 dev eth0 src $OWN_IP uid 0" ;;
"-4 addr show") echo "    inet $OWN_IP/32 scope global eth0" ;;
"rule show"*) ;;
*) echo "ip $*" >>"$LOG" ;;
esac
EOF
for cmd in iptables ufw systemctl sysctl install; do
	cat >"$stubs/$cmd" <<-EOF
		#!/usr/bin/env bash
		echo "$cmd \$*" >>"\$LOG"
	EOF
done
chmod +x "$stubs"/*
PATH=$stubs:$PATH

failed=0
fail() {
	echo "FAIL: $*" >&2
	failed=1
}

# One dry run of setup and both rules commands, normalised to compare.
dry() { # out link-args... -- role
	local out=$1 role
	shift
	local args=()
	while [ "$1" != -- ]; do
		args+=("$1")
		shift
	done
	role=$2
	# The VPS and the node are set up with their own address, home with
	# its peer's.
	local ip=$OWN_IP
	[ "$role" != home ] || ip=$PEER_IP
	mkdir -p "$out"
	"$SCRIPT" ${args[@]+"${args[@]}"} --dry-run "$out" "$role" "$PEER_KEY" "$ip" >"$out/stdout"
	"$SCRIPT" ${args[@]+"${args[@]}"} --dry-run "$out" rules "$role" up >>"$out/stdout"
	"$SCRIPT" ${args[@]+"${args[@]}"} --dry-run "$out" rules "$role" down >>"$out/stdout"
	# The stub's private key is not part of the expected tree; that the
	# config carries it is checked here instead.
	local key
	key=$(find "$out/etc/wireguard" -name '*.key')
	[ "$(cat "$key")" = "$STUB_PRIVATE" ] || fail "$out: the key file holds something else"
	grep -qx "PrivateKey = $STUB_PRIVATE" "$out"/etc/wireguard/*.conf || fail "$out: the config lacks the private key"
	rm "$key"
	# The checkout's path is the only part that depends on where it runs.
	sed "s|$SCRIPT|@SCRIPT@|g" "$out/commands" >"$out/commands.tmp"
	mv "$out/commands.tmp" "$out/commands"
}

check() { # name link-args... -- role
	local name=$1
	shift
	local out=$work/$name
	dry "$out" "$@"
	if [ -n "$UPDATE" ]; then
		rm -rf "${TESTDATA:?}/$name"
		mkdir -p "$TESTDATA"
		cp -R "$out" "$TESTDATA/$name"
		echo "updated testdata/$name"
	elif ! diff -ru "$TESTDATA/$name" "$out"; then
		fail "$name differs from testdata/$name"
	fi
}

check hetzner-vps -- vps
check hetzner-home -- home
check ru-node --link ru -- node
check ru-home --link ru -- home

# --link hetzner is the default spelled out.
for role in vps home; do
	dry "$work/explicit-$role" --link hetzner -- "$role"
	diff -ru "$work/hetzner-$role" "$work/explicit-$role" >/dev/null ||
		fail "--link hetzner $role differs from no --link"
done

# key: the link's own key file, the public key alone on stdout.
for link in hetzner:pluma ru:plumaru; do
	out=$work/key-${link%%:*}
	got=$("$SCRIPT" --link "${link%%:*}" --dry-run "$out" key)
	[ "$got" = "$STUB_PUBLIC" ] || fail "key of ${link%%:*} printed $got"
	[ -s "$out/etc/wireguard/${link#*:}.key" ] || fail "key of ${link%%:*}: no ${link#*:}.key"
done

# Roles of the other link, unknown links and bad arguments are refused
# before anything is written.
refuse() { # args...
	local out=$work/refused
	rm -rf "$out"
	local code=0
	"$SCRIPT" --dry-run "$out" "$@" >/dev/null 2>&1 || code=$?
	[ "$code" = 2 ] || fail "tunnel.sh $* exited $code, want 2"
	[ ! -e "$out/commands" ] || fail "tunnel.sh $* wrote commands"
}
refuse node "$PEER_KEY" "$PEER_IP"
refuse --link ru vps "$PEER_KEY" "$PEER_IP"
refuse --link ru rules vps up
refuse --link hetzner rules node up
refuse --link mars key
refuse --link ru home not-a-key "$PEER_IP"
refuse --link ru home "$PEER_KEY" 203.0.113
refuse --link ru home "$PEER_KEY"

# The node and the VPS must be set up on their own address.
code=0
"$SCRIPT" --link ru --dry-run "$work/elsewhere" node "$PEER_KEY" 192.0.2.77 >/dev/null 2>&1 || code=$?
[ "$code" = 1 ] || fail "node on an address not of the host exited $code, want 1"

if [ -s "$LOG" ]; then
	fail "a dry run ran commands that change the host:"
	cat "$LOG" >&2
fi

[ "$failed" = 0 ] || exit 1
echo "ok: tunnel.sh"
