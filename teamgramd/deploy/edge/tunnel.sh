#!/usr/bin/env bash
# The WireGuard tunnel between Pluma at home and its edge on the VPS (task
# 096, ADR 0061, docs/ops/home-server.md). Home dials out to the VPS, so the
# home router needs no forwarded port; the VPS keeps Pluma's public address
# and passes its connections home as they are (forward.sh), with the
# clients' own source addresses.
#
#   tunnel.sh key                          the host's public key (made once)
#   tunnel.sh vps  HOME_KEY PUBLIC_IPV4    on the VPS: interface, rules, unit
#   tunnel.sh home VPS_KEY  PUBLIC_IPV4    at home: the same, dialling the VPS
#   tunnel.sh rules vps|home up|down       run by pluma-tunnel-rules.service
#
# Addresses: the VPS 10.77.0.1, home 10.77.0.2 (interface "pluma"). Home's
# containers (teamgram_net, 172.20.0.0/16) send everything beyond the host
# and its networks through the tunnel: the replies to the forwarded clients
# and their own outgoing traffic, which leaves the VPS from Pluma's address
# as before the move, so the home address is not seen by anyone.
# The private key stays in /etc/wireguard (pluma.key and the 0600
# pluma.conf) and is never printed. The rules are a unit of their own,
# bound to the interface's: Ubuntu's AppArmor profile of wg-quick does not
# let it run scripts from PostUp.
set -euo pipefail

IFACE=pluma
VPS_ADDR=10.77.0.1
HOME_ADDR=10.77.0.2
PORT=51820
# The path to the VPS carries 1460-byte packets (measured 2026-11-14);
# WireGuard over IPv4 takes 60, the rest is margin for other home links.
MTU=1380
BRIDGE_NET=172.20.0.0/16
TABLE=77
DIR=/etc/wireguard
KEY=$DIR/$IFACE.key
# Installed on setup: the rules unit runs it, whatever the checkout of
# /opt/pluma is at.
INSTALLED=/usr/local/sbin/pluma-tunnel
SELF=$(readlink -f "$0")

usage() {
	sed -n "2,23p" "$SELF" | sed 's/^# \{0,1\}//'
	exit 2
}

need_root() { [ "$(id -u)" = 0 ] || { echo "run as root" >&2; exit 1; }; }

pubkey() {
	need_root
	umask 077
	mkdir -p "$DIR"
	[ -s "$KEY" ] || wg genkey >"$KEY"
	wg pubkey <"$KEY"
}

valid_key() { [[ $1 =~ ^[A-Za-z0-9+/]{42}[AEIMQUYcgkosw048]=$ ]] || { echo "not a WireGuard key: $1" >&2; exit 2; }; }
valid_ip() { [[ $1 =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] || { echo "not an IPv4 address: $1" >&2; exit 2; }; }

# The interface the host reaches the internet by.
uplink() { ip -4 route get 1.1.1.1 | sed -n 's/.* dev \([^ ]*\).*/\1/p'; }

write_conf() { # role peer_key public_ip
	local role=$1 peer=$2 public=$3
	pubkey >/dev/null
	[ "$SELF" = "$INSTALLED" ] || install -m 0755 "$SELF" "$INSTALLED"
	umask 077
	printf 'ROLE=%s\nPUBLIC_IPV4=%s\nUPLINK=%s\n' "$role" "$public" "$(uplink)" >"$DIR/$IFACE.env"
	{
		echo "# Written by teamgramd/deploy/edge/tunnel.sh; edit there, not here."
		echo "[Interface]"
		if [ "$role" = vps ]; then
			echo "Address = $VPS_ADDR/24"
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

# Rules are added once: -C checks before -I or -A.
ipt() { # table chain position-op rule...
	local t=$1 c=$2 op=$3
	shift 3
	if [ "$op" = -D ]; then
		while iptables -t "$t" -C "$c" "$@" 2>/dev/null; do iptables -t "$t" -D "$c" "$@"; done
	else
		iptables -t "$t" -C "$c" "$@" 2>/dev/null || iptables -t "$t" "$op" "$c" "$@"
	fi
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
		ip route replace default dev "$IFACE" table "$TABLE"
		# The host's own routes first, without the default one: the LAN,
		# the bridges and the tunnel's subnet stay where they are.
		ip rule show pref 100 | grep -q . || ip rule add pref 100 lookup main suppress_prefixlength 0
		# Everything else of the containers goes through the tunnel.
		ip rule show pref 110 | grep -q . || ip rule add pref 110 from "$BRIDGE_NET" lookup "$TABLE"
		ipt mangle FORWARD -I -o "$IFACE" -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu
		# Replies leave by the tunnel although their source route is the LAN.
		sysctl -qw "net.ipv4.conf.$IFACE.rp_filter=2"
	else
		ip rule del pref 110 2>/dev/null || true
		ip rule del pref 100 2>/dev/null || true
		ip route flush table "$TABLE" 2>/dev/null || true
		ipt mangle FORWARD -D -o "$IFACE" -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu
	fi
}

# The rules follow the interface: started after it, stopped with it.
rules_unit() { # role
	cat >/etc/systemd/system/pluma-tunnel-rules.service <<-EOF
		# Written by teamgramd/deploy/edge/tunnel.sh.
		[Unit]
		Description=Routing and firewall rules of Pluma's tunnel
		BindsTo=wg-quick@$IFACE.service
		After=wg-quick@$IFACE.service

		[Service]
		Type=oneshot
		RemainAfterExit=yes
		ExecStart=$INSTALLED rules $1 up
		ExecStop=$INSTALLED rules $1 down

		[Install]
		WantedBy=wg-quick@$IFACE.service
	EOF
	systemctl daemon-reload
	systemctl enable -q "wg-quick@$IFACE" pluma-tunnel-rules.service
	systemctl restart "wg-quick@$IFACE"
	systemctl restart pluma-tunnel-rules.service
}

setup_vps() { # home_key public_ip
	need_root
	valid_key "$1"
	valid_ip "$2"
	ip -4 addr show | grep -q "inet $2/" || { echo "$2 is not an address of this host" >&2; exit 1; }
	write_conf vps "$1" "$2"
	# Only Pluma's address takes the tunnel's port; the host's other
	# addresses stay as they are.
	ufw allow proto udp to "$2" port "$PORT" comment 'Pluma tunnel' >/dev/null
	rules_unit vps
	echo "tunnel up: $VPS_ADDR, port $PORT on $2"
}

setup_home() { # vps_key public_ip
	need_root
	valid_key "$1"
	valid_ip "$2"
	write_conf home "$1" "$2"
	# Docker publishes Pluma's ports on the tunnel's address: it must exist
	# before dockerd starts.
	mkdir -p /etc/systemd/system/docker.service.d
	cat >/etc/systemd/system/docker.service.d/10-pluma-tunnel.conf <<-EOF
		# Written by teamgramd/deploy/edge/tunnel.sh.
		[Unit]
		Wants=wg-quick@$IFACE.service
		After=wg-quick@$IFACE.service
	EOF
	rules_unit home
	echo "tunnel up: $HOME_ADDR, dialling $2:$PORT"
}

case ${1:-} in
key) pubkey ;;
vps) [ $# = 3 ] || usage; setup_vps "$2" "$3" ;;
home) [ $# = 3 ] || usage; setup_home "$2" "$3" ;;
rules)
	[ $# = 3 ] || usage
	case $2/$3 in
	vps/up | vps/down) rules_vps "$3" ;;
	home/up | home/down) rules_home "$3" ;;
	*) usage ;;
	esac
	;;
*) usage ;;
esac
