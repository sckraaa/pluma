#!/usr/bin/env bash
# The switch of the move (task 096, docs/ops/home-server.md): Pluma's public
# ports on the VPS forwarded home through the tunnel of tunnel.sh, or not.
#
#   forward.sh on | off | status
#
# Only the ports of the core, as a list: the gateway (10443, 5222) and the
# web (80, 443 and HTTP/3 on 443/udp). The ports that stay on the VPS (the
# SFU, TURN, the tunnel itself, SSH) and any port added later are not
# caught. The destination is rewritten, the source is not: home sees each
# client's own address, which its limits, sessions and GeoIP need.
# "on" is kept across reboots by the unit pluma-forward.service; "off"
# removes the unit's effect and disables it. Run as root on the VPS.
set -euo pipefail

HOME_ADDR=10.77.0.2
CHAIN=PLUMA-FORWARD
ENV=/etc/wireguard/pluma.env
UNIT=/etc/systemd/system/pluma-forward.service
INSTALLED=/usr/local/sbin/pluma-forward
SELF=$(readlink -f "$0")

[ "$(id -u)" = 0 ] || { echo "run as root" >&2; exit 1; }
[ -s "$ENV" ] || { echo "no tunnel: run tunnel.sh vps first" >&2; exit 1; }
PUBLIC=$(sed -n 's/^PUBLIC_IPV4=//p' "$ENV")

hook() { # -C|-I|-D
	iptables -t nat "$1" PREROUTING -d "$PUBLIC" -j "$CHAIN" 2>/dev/null
}

on() {
	iptables -t nat -N "$CHAIN" 2>/dev/null || iptables -t nat -F "$CHAIN"
	iptables -t nat -A "$CHAIN" -p tcp -m multiport --dports 80,443,5222,10443 -j DNAT --to-destination "$HOME_ADDR"
	iptables -t nat -A "$CHAIN" -p udp --dport 443 -j DNAT --to-destination "$HOME_ADDR"
	# First in PREROUTING, before Docker's own chain: a container that still
	# publishes one of these ports on the VPS does not take its traffic.
	# Connections already open keep their way; new ones go home.
	hook -C || hook -I
	[ "$SELF" = "$INSTALLED" ] || install -m 0755 "$SELF" "$INSTALLED"
	if [ ! -s "$UNIT" ]; then
		cat >"$UNIT" <<-EOF
			# Written by teamgramd/deploy/edge/forward.sh.
			[Unit]
			Description=Pluma's public ports forwarded home
			After=wg-quick@pluma.service docker.service
			Wants=wg-quick@pluma.service

			[Service]
			Type=oneshot
			RemainAfterExit=yes
			ExecStart=$INSTALLED on
			ExecStop=$INSTALLED off-now

			[Install]
			WantedBy=multi-user.target
		EOF
		systemctl daemon-reload
		systemctl enable -q pluma-forward.service
	fi
	echo "forwarding $PUBLIC tcp 80,443,5222,10443 udp 443 -> $HOME_ADDR"
}

off_now() {
	while hook -C; do hook -D; done
	iptables -t nat -F "$CHAIN" 2>/dev/null || true
	iptables -t nat -X "$CHAIN" 2>/dev/null || true
}

case ${1:-} in
on) on ;;
off)
	off_now
	if [ -s "$UNIT" ]; then
		systemctl stop pluma-forward.service
		systemctl disable -q pluma-forward.service
		rm -f "$UNIT"
		systemctl daemon-reload
	fi
	echo "not forwarding: the ports of $PUBLIC are the VPS's own"
	;;
off-now) off_now ;;
status)
	if hook -C; then
		iptables -t nat -S "$CHAIN"
	else
		echo "off"
	fi
	;;
*)
	sed -n '2,14p' "$SELF" | sed 's/^# \{0,1\}//'
	exit 2
	;;
esac
