#!/usr/bin/env bash
# Issues the certificates of the control API of a CDN node (task 102,
# docs/ops/cdn.md): mutual TLS between bff at home and the node on the edge.
#
#   teamgramd/deploy/cdn/control-certs.sh CDN_DIR BFF_DIR [NAME...]
#
# CDN_DIR gets the node's certificate and the CA of bff's certificate
# (server.pem, server-key.pem, client-ca.pem); BFF_DIR gets bff's
# certificate and the CA of the node's (client.pem, client-key.pem,
# server-ca.pem). NAMEs are the names and addresses bff reaches the node
# by, besides "cdn" (the compose service): the tunnel address 10.77.0.1
# on the edge. Two CAs, so that neither side's certificate passes for the
# other's, and none shared with the SFU's; their keys are not kept: a new
# certificate is a new run, and both sides take the new files.
set -euo pipefail

if [ $# -lt 2 ]; then
	sed -n '2,14p' "$0" | sed 's/^# \{0,1\}//'
	exit 2
fi
cdn_dir=$1
bff_dir=$2
shift 2

san="DNS:cdn"
for n in "$@"; do
	if [[ $n =~ ^[0-9.]+$ || $n == *:* ]]; then
		san="$san,IP:$n"
	else
		san="$san,DNS:$n"
	fi
done

umask 077
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

key() { openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$1" 2>/dev/null; }

ca() {
	key "$work/$1-key.pem"
	openssl req -x509 -new -key "$work/$1-key.pem" -out "$work/$1.pem" -days 3650 -subj "/CN=$2" \
		-addext "basicConstraints=critical,CA:TRUE" -addext "keyUsage=critical,keyCertSign,cRLSign"
}

leaf() { # name CN ca extendedKeyUsage subjectAltName
	key "$work/$1-key.pem"
	openssl req -new -key "$work/$1-key.pem" -out "$work/$1.csr" -subj "/CN=$2"
	{
		echo "basicConstraints=critical,CA:FALSE"
		echo "keyUsage=critical,digitalSignature"
		echo "extendedKeyUsage=$4"
		[ -n "$5" ] && echo "subjectAltName=$5"
	} >"$work/$1.ext"
	openssl x509 -req -in "$work/$1.csr" -CA "$work/$3.pem" -CAkey "$work/$3-key.pem" \
		-set_serial "0x$(openssl rand -hex 16)" -days 825 -extfile "$work/$1.ext" -out "$work/$1.pem" 2>/dev/null
}

ca server-ca "Pluma CDN control: nodes"
ca client-ca "Pluma CDN control: home"
leaf server cdn server-ca serverAuth "$san"
leaf client bff client-ca clientAuth ""

mkdir -p "$cdn_dir" "$bff_dir"
install -m 0400 "$work/server.pem" "$work/server-key.pem" "$work/client-ca.pem" "$cdn_dir/"
install -m 0400 "$work/client.pem" "$work/client-key.pem" "$work/server-ca.pem" "$bff_dir/"
if [ "$(id -u)" = 0 ]; then
	# The node's image runs as nonroot (uid 65532); the folder too, or the
	# node could not reach its files.
	chown 65532:65532 "$cdn_dir" "$cdn_dir"/server.pem "$cdn_dir"/server-key.pem "$cdn_dir"/client-ca.pem
fi
echo "CDN: $cdn_dir (names: $san); bff: $bff_dir"
