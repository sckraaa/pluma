#!/usr/bin/env bash
# Rebuilds region.geojson next to this script: the area of the map data of
# the maps service (task 100, ADR 0063, p. 4; docs/ops/maps.md). The result
# is committed; run this only to change the region, then commit both.
#
#   make-region.sh
#
# Needs Docker and the network: a throwaway container of a pinned Python
# image installs numpy and shapely by hash and runs make_region.py on the
# Natural Earth country borders, a pinned file checked by SHA-256. The list
# of countries is in make_region.py.

set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)

PYTHON_IMAGE=python:3.13.18-slim-trixie@sha256:e48f59cdba69a3cbd9ed2cf0f348d90d45a8d350d98134cf1070bc106ee9ed90
# Natural Earth Admin 0 countries 1:10m, release v5.1.2 (public domain).
NE_URL=https://raw.githubusercontent.com/nvkelso/natural-earth-vector/v5.1.2/geojson/ne_10m_admin_0_countries.geojson
NE_SHA256=239eec57ac17f100a11e2536cffc56752c318b50ae765b0918ff7aab4ce8f255
# pmtiles extract reads the whole file; keep it small.
MAX_BYTES=512000

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cp "$here/make_region.py" "$here/requirements-region.txt" "$work/"

# The container runs as the caller, so the output belongs to them; pip
# installs into the work directory and nothing else is written.
docker run --rm \
	--user "$(id -u):$(id -g)" \
	-e HOME=/work -e PIP_DISABLE_PIP_VERSION_CHECK=1 -e PIP_NO_CACHE_DIR=1 \
	-e NE_URL="$NE_URL" -e NE_SHA256="$NE_SHA256" \
	-v "$work:/work" -w /work \
	"$PYTHON_IMAGE" \
	sh -euc '
		pip install --quiet --require-hashes --only-binary :all: --no-deps \
			--target /work/site -r requirements-region.txt
		python -I -c "
import hashlib, os, urllib.request
data = urllib.request.urlopen(os.environ[\"NE_URL\"], timeout=120).read()
got = hashlib.sha256(data).hexdigest()
if got != os.environ[\"NE_SHA256\"]:
    raise SystemExit(\"countries file: sha256 \" + got + \", expected \" + os.environ[\"NE_SHA256\"])
open(\"countries.geojson\", \"wb\").write(data)
"
		PYTHONPATH=/work/site python make_region.py countries.geojson region.geojson
	'

size=$(wc -c <"$work/region.geojson")
if [ "$size" -gt "$MAX_BYTES" ]; then
	echo "make-region: region.geojson is $size bytes, more than $MAX_BYTES" >&2
	exit 1
fi
mv "$work/region.geojson" "$here/region.geojson"
echo "make-region: wrote $here/region.geojson ($size bytes)"
