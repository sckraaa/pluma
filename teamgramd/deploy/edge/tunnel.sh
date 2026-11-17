#!/usr/bin/env bash
# The WireGuard tunnels of Pluma at home (docs/ops/home-server.md). Home
# dials out, so the home router needs no forwarded port. Two links:
#
#   hetzner  the edge on the VPS (task 096, ADR 0061), interface "pluma":
#            the VPS 10.77.0.1, home 10.77.0.2. The VPS keeps Pluma's
#            public address and passes its connections home as they are
#            (forward.sh), with the clients' own source addresses.
#   ru       the Russian node amf (task 109, ADR 0072 §8), interface
#            "plumaru": the node 10.78.0.1, home 10.78.0.2. The node opens
#            every connection home itself, from 10.78.0.1, and passes the
#            client's address in PROXY v1: nothing is forwarded, no table.
#
#   tunnel.sh [--link hetzner|ru] key                    the host's public key
#   tunnel.sh [--link hetzner] vps  HOME_KEY PUBLIC_IPV4 on the VPS
#   tunnel.sh --link ru        node HOME_KEY PUBLIC_IPV4 on the Russian node
#   tunnel.sh [--link L]       home PEER_KEY PUBLIC_IPV4 at home, dialling it
#   tunnel.sh [--link L]       rules ROLE up|down        run by the rules unit
#
# Without --link the link is hetzner, as before the second one. PUBLIC_IPV4
# is the address of the VPS or the node. --dry-run DIR changes nothing on
# the host: the files go under DIR as they would go under /, and the
# commands that would change the host are written to DIR/commands.
#
# Home's containers (teamgram_net, 172.20.0.0/16) send everything beyond
# the host and its networks through the hetzner tunnel: the replies to the
# forwarded clients and their own outgoing traffic, which leaves the VPS
# from Pluma's address as before the move, so the home address is not seen
# by anyone. Their replies to the node take the connected route of
# plumaru's subnet, which the rule "lookup main suppress_prefixlength 0"
# keeps ahead of the hetzner table.
# The private keys stay in /etc/wireguard (<interface>.key and the 0600
# <interface>.conf) and are never printed. The rules are a unit of their
# own, bound to the interface's: Ubuntu's AppArmor profile of wg-quick does
# not let it run scripts from PostUp.
set -euo pipefail

usage() {
	sed -n "2,35p" "$SELF" | sed 's/^# \{0,1\}//'
	exit 2
}

SELF=$(readlink -f "$0")
LINK=hetzner
DRY=
while [ $# -gt 0 ]; do
	case $1 in
	--link)
		[ $# -ge 2 ] || usage
		LINK=$2
		shift 2
		;;
	--dry-run)
		[ $# -ge 2 ] || usage
		mkdir -p "$2"
		DRY=$(cd "$2" && pwd)
		shift 2
		;;
	*) break ;;
	esac
done

PORT=51820
BRIDGE_NET=172.20.0.0/16
case $LINK in
hetzner)
	IFACE=pluma
	PEER_ADDR=10.77.0.1
	HOME_ADDR=10.77.0.2
	# The path to the VPS carries 1460-byte packets (measured 2026-11-14);
	# WireGuard over IPv4 takes 60, the rest is margin for other home links.
	MTU=1380
	TABLE=77
	RULES_UNIT=pluma-tunnel-rules.service
	DOCKER_DROPIN=10-pluma-tunnel.conf
	# The units' command names the link only when it is not the default,
	# so the unit of the first tunnel stays as it was written.
	LINK_ARG=
	;;
ru)
	IFACE=plumaru
	PEER_ADDR=10.78.0.1
	HOME_ADDR=10.78.0.2
	# Measured 2026-11-17 (task 109): home → amf moved nothing at 1420, a
	# black hole of path MTU discovery, and 50 Mbit/s at 1280. Both ends.
	MTU=1280
	RULES_UNIT=pluma-tunnel-ru-rules.service
	DOCKER_DROPIN=10-pluma-tunnel-ru.conf
	LINK_ARG="--link ru "
	;;
*) usage ;;
esac

ROOT=$DRY
DIR=$ROOT/etc/wireguard
KEY=$DIR/$IFACE.key
UNITS=$ROOT/etc/systemd/system
# Installed on setup: the rules unit runs it, whatever the checkout of
# /opt/pluma is at.
INSTALLED=/usr/local/sbin/pluma-tunnel

need_root() { [ -n "$DRY" ] || [ "$(id -u)" = 0 ] || { echo "run as root" >&2; exit 1; }; }

# A command that changes the host: run, or only written down on a dry run,
# one line each, an argument with blanks or quotes in single quotes.
run() {
	if [ -n "$DRY" ]; then
		local arg line=
		for arg in "$@"; do
			case $arg in
			'' | *[[:space:]\'\"\\\$]*) arg="'${arg//\'/\'\\\'\'}'" ;;
			esac
			line+="${line:+ }$arg"
		done
		printf '%s\n' "$line" >>"$DRY/commands"
	else
		"$@"
	fi
}

say() {
	if [ -n "$DRY" ]; then
		echo "dry run, nothing changed; would be: $*"
	else
		echo "$*"
	fi
}

pubkey() {
	need_root
	umask 077
	mkdir -p "$DIR"
	[ -s "$KEY" ] || wg genkey >"$KEY"
	wg pubkey <"$KEY"
}

valid_key() { [[ $1 =~ ^[A-Za-z0-9+/]{42}[AEIMQUYcgkosw048]=$ ]] || { echo "not a WireGuard key: $1" >&2; exit 2; }; }
valid_ip() { [[ $1 =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] || { echo "not an IPv4 address: $1" >&2; exit 2; }; }
own_ip() { ip -4 addr show | grep -q "inet $1/" || { echo "$1 is not an address of this host" >&2; exit 1; }; }

# The interface the host reaches the internet by.
uplink() { ip -4 route get 1.1.1.1 | sed -n 's/.* dev \([^ ]*\).*/\1/p'; }

install_self() { [ "$SELF" = "$INSTALLED" ] || run install -m 0755 "$SELF" "$INSTALLED"; }

write_conf() { # role peer_key public_ip
	local role=$1 peer=$2 public=$3
	pubkey >/dev/null
	install_self
	umask 077
	printf 'ROLE=%s\nPUBLIC_IPV4=%s\nUPLINK=%s\n' "$role" "$public" "$(uplink)" >"$DIR/$IFACE.env"
	{
		echo "# Written by teamgramd/deploy/edge/tunnel.sh; edit there, not here."
		echo "[Interface]"
		if [ "$role" = vps ]; then
			echo "Address = $PEER_ADDR/24"
			echo "ListenPort = $PORT"
		else
			echo "Address = $HOME_ADDR/24"
			# Routes are the rules' job: the peer is every address.
			echo "Table = off"
		fi
		echo "MTU = $MTU"
		echo "PrivateKey = $(cat "$KEY")"
		echo
		echo "[Peer]"
		echo "PublicKey = $peer"
		if [ "$role" = vps ]; then
			echo "AllowedIPs = $HOME_ADDR/32"
		else
			echo "Endpoint = $public:$PORT"
			# Clients' packets arrive with their own source addresses.
			echo "AllowedIPs = 0.0.0.0/0"
			# Home is behind NAT: keep its mapping open for the VPS.
			echo "PersistentKeepalive = 25"
		fi
	} >"$DIR/$IFACE.conf"
}

# The Russian link: no forwarding and no table, so a file of its own.
write_conf_ru() { # role peer_key public_ip
	local role=$1 peer=$2 public=$3
	pubkey >/dev/null
	install_self
	umask 077
	printf 'ROLE=%s\nPUBLIC_IPV4=%s\n' "$role" "$public" >"$DIR/$IFACE.env"
	{
		echo "# Written by teamgramd/deploy/edge/tunnel.sh; edit there, not here."
		echo "[Interface]"
		if [ "$role" = node ]; then
			echo "Address = $PEER_ADDR/24"
			echo "ListenPort = $PORT"
		else
			echo "Address = $HOME_ADDR/24"
			# The subnet's connected route is all home needs: the node's
			# connections come from its tunnel address, and the replies take
			# that route by the hetzner link's rule "lookup main
			# suppress_prefixlength 0", not its table.
			echo "Table = off"
		fi
		echo "MTU = $MTU"
		echo "PrivateKey = $(cat "$KEY")"
		echo
		echo "[Peer]"
		echo "PublicKey = $peer"
		if [ "$role" = node ]; then
			# Home's own address, and the one Docker gives the outgoing
			# traffic of home's containers (host_ipv4 of teamgram_net,
			# docker-compose.public-ip.yaml): bff and Prometheus reach the
			# node from there. Routes to these two only: the node gets no
			# other address of home or of the VPS.
			echo "AllowedIPs = $HOME_ADDR/32, 10.77.0.2/32"
		else
			echo "Endpoint = $public:$PORT"
			echo "AllowedIPs = $PEER_ADDR/32"
			# Home is behind NAT: keep its mapping open for the node.
			echo "PersistentKeepalive = 25"
		fi
	} >"$DIR/$IFACE.conf"
}

# Rules are added once: -C checks before -I or -A. A dry run writes the
# change down as if the rule were not there.
ipt() { # table chain position-op rule...
	local t=$1 c=$2 op=$3
	shift 3
	if [ -n "$DRY" ]; then
		run iptables -t "$t" "$op" "$c" "$@"
	elif [ "$op" = -D ]; then
		while iptables -t "$t" -C "$c" "$@" 2>/dev/null; do iptables -t "$t" -D "$c" "$@"; done
	else
		iptables -t "$t" -C "$c" "$@" 2>/dev/null || iptables -t "$t" "$op" "$c" "$@"
	fi
}

ip_rule_add() { # pref selector...
	local pref=$1
	shift
	if [ -z "$DRY" ] && ip rule show pref "$pref" | grep -q .; then
		return
	fi
	run ip rule add pref "$pref" "$@"
}

rules_vps() { # up|down
	local op=-I public uplink
	[ "$1" = down ] && op=-D
	public=$(sed -n 's/^PUBLIC_IPV4=//p' "$DIR/$IFACE.env")
	uplink=$(sed -n 's/^UPLINK=//p' "$DIR/$IFACE.env")
	# Home's own traffic out to the internet, from Pluma's address; before
	# the other SNAT rules of the host, so none of them takes it.
	ipt nat POSTROUTING "$op" -s "$HOME_ADDR" -o "$uplink" -j SNAT --to-source "$public"
	# Forwarding between the tunnel and the internet: home's traffic out and
	# its replies, and the connections forward.sh sends home.
	ipt filter FORWARD "$op" -i "$IFACE" -o "$uplink" -j ACCEPT
	ipt filter FORWARD "$op" -i "$uplink" -o "$IFACE" -m conntrack --ctstate ESTABLISHED,RELATED,DNAT -j ACCEPT
	# TCP segments that fit the tunnel, both ways.
	ipt mangle FORWARD "$op" -o "$IFACE" -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu
	ipt mangle FORWARD "$op" -i "$IFACE" -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu
}

rules_home() { # up|down
	if [ "$1" = up ]; then
		run ip route replace default dev "$IFACE" table "$TABLE"
		# The host's own routes first, without the default one: the LAN,
		# the bridges and the tunnels' subnets stay where they are.
		ip_rule_add 100 lookup main suppress_prefixlength 0
		# Everything else of the containers goes through the tunnel.
		ip_rule_add 110 from "$BRIDGE_NET" lookup "$TABLE"
		ipt mangle FORWARD -I -o "$IFACE" -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu
		# Replies leave by the tunnel although their source route is the LAN.
		run sysctl -qw "net.ipv4.conf.$IFACE.rp_filter=2"
	else
		run ip rule del pref 110 2>/dev/null || true
		run ip rule del pref 100 2>/dev/null || true
		run ip route flush table "$TABLE" 2>/dev/null || true
		ipt mangle FORWARD -D -o "$IFACE" -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu
	fi
}

# The node forwards nothing: its own connections home (nginx) and its
# replies to home's (Prometheus, bff) leave it by OUTPUT, SYN and SYN-ACK.
rules_node() { # up|down
	local op=-I
	[ "$1" = down ] && op=-D
	ipt mangle OUTPUT "$op" -o "$IFACE" -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu
}

# At home the node's connections reach the containers behind Docker's
# ports on the tunnel's address, and the containers reach the node: both
# are forwarded, both ways.
rules_home_ru() { # up|down
	local op=-I
	[ "$1" = down ] && op=-D
	ipt mangle FORWARD "$op" -o "$IFACE" -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu
	ipt mangle FORWARD "$op" -i "$IFACE" -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu
}

# The rules follow the interface: started after it, stopped with it.
rules_unit() { # role
	local description="Routing and firewall rules of Pluma's tunnel"
	[ "$LINK" = hetzner ] || description="Firewall rules of Pluma's tunnel to the Russian node"
	mkdir -p "$UNITS"
	cat >"$UNITS/$RULES_UNIT" <<-EOF
		# Written by teamgramd/deploy/edge/tunnel.sh.
		[Unit]
		Description=$description
		BindsTo=wg-quick@$IFACE.service
		After=wg-quick@$IFACE.service

		[Service]
		Type=oneshot
		RemainAfterExit=yes
		ExecStart=$INSTALLED ${LINK_ARG}rules $1 up
		ExecStop=$INSTALLED ${LINK_ARG}rules $1 down

		[Install]
		WantedBy=wg-quick@$IFACE.service
	EOF
	run systemctl daemon-reload
	run systemctl enable -q "wg-quick@$IFACE" "$RULES_UNIT"
	run systemctl restart "wg-quick@$IFACE"
	run systemctl restart "$RULES_UNIT"
}

# Docker publishes Pluma's ports on the tunnel's address: it must exist
# before dockerd starts. The interface comes up without an answer from the
# peer, so a peer that is down does not hold home's start.
docker_dropin() {
	mkdir -p "$UNITS/docker.service.d"
	cat >"$UNITS/docker.service.d/$DOCKER_DROPIN" <<-EOF
		# Written by teamgramd/deploy/edge/tunnel.sh.
		[Unit]
		Wants=wg-quick@$IFACE.service
		After=wg-quick@$IFACE.service
	EOF
}

setup_vps() { # home_key public_ip
	need_root
	valid_key "$1"
	valid_ip "$2"
	own_ip "$2"
	write_conf vps "$1" "$2"
	# Only Pluma's address takes the tunnel's port; the host's other
	# addresses stay as they are.
	run ufw allow proto udp to "$2" port "$PORT" comment 'Pluma tunnel' >/dev/null
	rules_unit vps
	say "tunnel up: $PEER_ADDR, port $PORT on $2"
}

setup_home() { # vps_key public_ip
	need_root
	valid_key "$1"
	valid_ip "$2"
	write_conf home "$1" "$2"
	docker_dropin
	rules_unit home
	say "tunnel up: $HOME_ADDR, dialling $2:$PORT"
}

# The node's ufw is off (amf, 2026-11-17) and stays as the node's operator
# keeps it: the tunnel's port is not opened there.
setup_node() { # home_key public_ip
	need_root
	valid_key "$1"
	valid_ip "$2"
	own_ip "$2"
	write_conf_ru node "$1" "$2"
	rules_unit node
	say "tunnel up: $PEER_ADDR, port $PORT on $2"
}

setup_home_ru() { # node_key public_ip
	need_root
	valid_key "$1"
	valid_ip "$2"
	write_conf_ru home "$1" "$2"
	# The node sees none of the host's own services (SSH and the rest):
	# the first of ufw's rules, ahead of "allow 22/tcp", in place before
	# the interface comes up. Docker's ports on the tunnel's address pass
	# by ufw through Docker's DNAT, which is what they are published there
	# for. prepend skips a rule that is there already, and on an empty set
	# adds it (ufw: frontend.py and backend_iptables.py, set_rule).
	run ufw prepend deny in on "$IFACE" comment 'Pluma tunnel to the Russian node' >/dev/null
	docker_dropin
	rules_unit home
	say "tunnel up: $HOME_ADDR, dialling $2:$PORT"
}

case $LINK/${1:-} in
*/key) pubkey ;;
hetzner/vps) [ $# = 3 ] || usage; setup_vps "$2" "$3" ;;
hetzner/home) [ $# = 3 ] || usage; setup_home "$2" "$3" ;;
ru/node) [ $# = 3 ] || usage; setup_node "$2" "$3" ;;
ru/home) [ $# = 3 ] || usage; setup_home_ru "$2" "$3" ;;
*/rules)
	[ $# = 3 ] || usage
	case $LINK/$2/$3 in
	hetzner/vps/up | hetzner/vps/down) rules_vps "$3" ;;
	hetzner/home/up | hetzner/home/down) rules_home "$3" ;;
	ru/node/up | ru/node/down) rules_node "$3" ;;
	ru/home/up | ru/home/down) rules_home_ru "$3" ;;
	*) usage ;;
	esac
	;;
*) usage ;;
esac
