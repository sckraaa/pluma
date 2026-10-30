#!/usr/bin/env bash
# Puts the DB-IP Lite City database of a month into geoip/ at the root of the
# repository, the directory docker-compose.yaml mounts into authsession
# (docs/ops/deployment.md, "GeoIP (DB-IP Lite)"). DB-IP releases it monthly.
# authsession reads the database at start: restart it afterwards.
#
#   teamgramd/deploy/geoip/update-dbip.sh [YYYY-MM [SHA1]]
#
# YYYY-MM is the release, the current month (UTC) by default. SHA1 is the
# SHA1SUM that the download page lists for the MMDB, the sum of the unpacked
# file; given, the file must have it.
#
# The new file is checked (gzip, size, the MMDB metadata of DB-IP City Lite)
# and then renamed over the old one. authsession maps the open file into
# memory: writing over it in place would break the running service.
set -euo pipefail

release=${1:-$(date -u +%Y-%m)}
want_sha1=${2:-}
if [[ ! $release =~ ^[0-9]{4}-(0[1-9]|1[0-2])$ ]]; then
	echo "release $release: want YYYY-MM" >&2
	exit 2
fi
if [[ -n $want_sha1 && ! $want_sha1 =~ ^[0-9a-f]{40}$ ]]; then
	echo "SHA1 $want_sha1: want 40 hex digits" >&2
	exit 2
fi

dir=$(cd "$(dirname "$0")/../../.." && pwd)/geoip
url=https://download.db-ip.com/free/dbip-city-lite-$release.mmdb.gz
# The release of October 2026 is 127 MB unpacked: a much smaller file is not
# a whole database.
min_size=$((64 * 1024 * 1024))

mkdir -p "$dir"
# In the same directory, so that the final rename does not cross file systems.
tmp=$(mktemp -d "$dir/.update.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

echo "downloading $url"
curl --fail --silent --show-error --location --proto '=https' --max-time 1800 \
	--output "$tmp/db.mmdb.gz" "$url"
gzip -t "$tmp/db.mmdb.gz"
gzip -dc "$tmp/db.mmdb.gz" >"$tmp/db.mmdb"

size=$(($(wc -c <"$tmp/db.mmdb")))
if ((size < min_size)); then
	echo "$url: $size bytes unpacked, want at least $min_size" >&2
	exit 1
fi
# The metadata ends an MMDB file: its marker, and the database type that
# authsession's reader opens as a City database.
tail -c 131072 "$tmp/db.mmdb" >"$tmp/metadata"
if ! LC_ALL=C grep -aq 'MaxMind\.com' "$tmp/metadata" || ! LC_ALL=C grep -aq 'DBIP-City-Lite' "$tmp/metadata"; then
	echo "$url: not a DB-IP City Lite MMDB file" >&2
	exit 1
fi
sha1=$(sha1sum "$tmp/db.mmdb" | cut -d' ' -f1)
if [[ -n $want_sha1 && $sha1 != "$want_sha1" ]]; then
	echo "$url: SHA1 $sha1, want $want_sha1" >&2
	exit 1
fi

chmod 0644 "$tmp/db.mmdb"
mv -f "$tmp/db.mmdb" "$dir/dbip-city-lite.mmdb"
echo "$dir/dbip-city-lite.mmdb: release $release, $size bytes, SHA1 $sha1"
echo "restart authsession to read it (docs/ops/deployment.md, \"GeoIP (DB-IP Lite)\")"
