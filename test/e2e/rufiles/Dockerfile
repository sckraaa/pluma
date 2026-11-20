# rufiles: delivery 202 of the Russian node for the end-to-end tests (task
# 109, test/e2e/cdn_ru_test.go). The node's nginx of Ubuntu 26.04 (the
# release of amf, the image of teamgramd/deploy/ru/test) with the files
# deploy.sh renders from the repository's templates, behind a stand-in of
# the owner's nginx stream on 443 that adds PROXY v1 as the owner's does.
FROM ubuntu:26.04@sha256:d66b366dac61b575bee2aef07e9c26e8070096fbe84a3d4d3eb0da069b5d76db
RUN export DEBIAN_FRONTEND=noninteractive && apt-get update -qq && \
	apt-get install -y -qq --no-install-recommends nginx libnginx-mod-stream openssl ca-certificates >/dev/null && \
	apt-get clean && rm -rf /var/lib/apt/lists/* /etc/nginx/sites-enabled/default && \
	useradd --system --user-group --home-dir /nonexistent --no-create-home --shell /usr/sbin/nologin pluma-ru && \
	install -d -m 0755 /etc/pluma-ru /var/lib/pluma-ru && \
	install -d -m 0700 -o pluma-ru -g pluma-ru /var/cache/pluma-ru /var/cache/pluma-ru/files /var/lib/pluma-ru/nginx
COPY teamgramd/deploy/ru/deploy.sh teamgramd/deploy/ru/nginx.conf teamgramd/deploy/ru/files.conf.tmpl teamgramd/deploy/ru/stream.conf.tmpl \
	teamgramd/deploy/ru/stream-upstreams.conf teamgramd/deploy/ru/sni.map.tmpl teamgramd/deploy/ru/acme-http.conf.tmpl \
	/src/teamgramd/deploy/ru/
COPY teamgramd/deploy/turn/turnserver.conf /src/teamgramd/deploy/turn/turnserver.conf
COPY test/e2e/rufiles/entrypoint.sh /entrypoint.sh
# 443: the owner's stream stand-in, for the clients of the tests and the
# checks of bff (CDN_RU_HEALTH_ADDR); 20600/udp and 20601: the relay of
# group calls (task 116).
EXPOSE 443 20600/udp 20601
HEALTHCHECK --interval=2s --timeout=2s --retries=60 CMD test -s /run/pluma-ru-nginx.pid && test -s /run/owner-stream.pid
ENTRYPOINT ["bash", "/entrypoint.sh"]
