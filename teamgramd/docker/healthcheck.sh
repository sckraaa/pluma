#!/bin/bash
# Container health (Dockerfile HEALTHCHECK): healthy once every service
# process started by runall-docker.sh is running and the gateway accepts
# MTProto connections. A service that exits makes the container unhealthy,
# which `docker ps` and `docker compose up --wait` show; nothing restarts it
# yet (no process supervisor: task 004, docs/audit/03-architecture-and-ops.md).

for s in idgen status authsession dfs media biz msg sync bff session gnetway; do
	if ! pgrep -x "$s" >/dev/null; then
		echo "$s is not running"
		exit 1
	fi
done

if ! exec 3<>/dev/tcp/127.0.0.1/10443; then
	echo "gnetway does not accept connections on 10443"
	exit 1
fi
exec 3>&-
