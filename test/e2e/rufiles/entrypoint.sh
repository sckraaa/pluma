#!/usr/bin/env bash
# The node of delivery 202 on the e2e stand (Dockerfile). deploy.sh render
# writes /etc/pluma-ru from the templates as on amf, with X-Cache-Status of
# its test build; the origin is the stand's Caddy (the site of
# PLUMA_CDN_ORIGIN_DOMAIN, cdn-origin.caddy) in front of bff, as at home.
#
# It is also the relay of group calls (task 116): the stream.conf of
# deploy.sh passes 20600/udp and 20601 of this container to the SFU's.
#
# Environment: PLUMA_RU_FILES_DOMAIN, PLUMA_CDN_ORIGIN_DOMAIN,
# CDN_RU_TOKEN_KEY, CDN_RU_ORIGIN_AUTH. Mounts: /test/tls (the certificate
# of PLUMA_RU_FILES_DOMAIN from run.sh) and /caddy-data (Caddy's data, read
# only: the root of its CA, which signs the origin's certificate). Nothing
# secret is printed.
set -euo pipefail

root=/caddy-data/caddy/pki/authorities/local/root.crt
for _ in $(seq 120); do
	[ -s "$root" ] && break
	sleep 1
done
[ -s "$root" ] || { echo "rufiles: no root certificate of Caddy's CA" >&2; exit 1; }
# The origin's certificate is checked as on the node, against the system's
# roots: Caddy's CA stands for Let's Encrypt.
cat "$root" >>/etc/ssl/certs/ca-certificates.crt

web=$(getent ahostsv4 web | awk 'NR == 1 { print $1 }')
self=$(getent ahostsv4 "$(hostname)" | awk 'NR == 1 { print $1 }')
# The SFU behind the relay of group calls (task 116), by its address as
# PLUMA_RU_SFU_ADDR is on amf.
sfu=$(getent ahostsv4 sfu | awk 'NR == 1 { print $1 }')
[ -n "$web" ] && [ -n "$self" ] && [ -n "$sfu" ] || { echo "rufiles: no address of web, sfu or this container" >&2; exit 1; }

(
	umask 077
	cat >/etc/pluma-ru/env <<ENV
PLUMA_RU_PUBLIC_IPV4=$self
PLUMA_RU_FILES_DOMAIN=$PLUMA_RU_FILES_DOMAIN
PLUMA_RU_EDGE_DOMAIN=ru.$PLUMA_RU_FILES_DOMAIN
PLUMA_CDN_ORIGIN_DOMAIN=$PLUMA_CDN_ORIGIN_DOMAIN
PLUMA_WEB_DOMAIN=web.localhost
CDN_RU_TOKEN_KEY=$CDN_RU_TOKEN_KEY
CDN_RU_ORIGIN_AUTH=$CDN_RU_ORIGIN_AUTH
CALLS_TURN_SECRETS=ru=$(openssl rand -hex 32)
PLUMA_RU_ORIGIN_ADDR=$web:443
PLUMA_RU_SFU_ADDR=$sfu
ENV
)
bash /src/teamgramd/deploy/ru/deploy.sh render /etc/pluma-ru/env /etc/pluma-ru --cache-status-header

live=/etc/pluma-ru/letsencrypt/live/$PLUMA_RU_FILES_DOMAIN
install -d -m 0755 "$live"
install -m 0644 /test/tls/cert.pem "$live/fullchain.pem"
install -m 0600 /test/tls/key.pem "$live/privkey.pem"

# The owner's nginx stream on 443, reduced to what Pluma installs in it
# (owner-nginx.sh): its upstreams and SNI map, PROXY v1 to every upstream.
# The edge's upstream is not reached on the stand.
cat >/etc/nginx/owner-stream.conf <<'CONF'
pid /run/owner-stream.pid;
error_log stderr warn;
include /etc/nginx/modules-enabled/*.conf;
events {}
stream {
	include /etc/pluma-ru/owner/pluma-upstreams.conf;
	map $ssl_preread_server_name $owner_upstream {
		include /etc/pluma-ru/owner/pluma-sni.map;
		default 127.0.0.1:9;
	}
	server {
		listen 443;
		ssl_preread on;
		proxy_pass $owner_upstream;
		proxy_protocol on;
	}
}
CONF

nginx -t -q -c /etc/pluma-ru/nginx.conf
nginx -c /etc/pluma-ru/nginx.conf
exec nginx -c /etc/nginx/owner-stream.conf -g 'daemon off;'
