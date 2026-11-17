#!/usr/bin/env bash
# Cuts the map data of the maps service out of a daily Protomaps build
# (task 100, ADR 0063, p. 4; docs/ops/maps.md) and switches the service to
# it. Run on the server as root (the owner of MAPS_DATA_DIR), from the
# clone of the repository:
#
#   teamgramd/deploy/maps/extract.sh --dry-run   size of the cut, nothing written
#   teamgramd/deploy/maps/extract.sh             cut, verify, switch, check
#   teamgramd/deploy/maps/extract.sh --use pluma-<date>.pmtiles
#                       verify and switch to a cut made on another machine
#                       and copied into MAPS_DATA_DIR
#
# Memory: pmtiles holds the directory of every tile of the cut, about 10 GB
# for the whole region (113 million tiles, measured 2026-11-16), in the dry
# run as well. The script refuses to start with less than
# MAPS_EXTRACT_MEMORY_GB (default 12) of available memory; on a host that
# cannot spare it, cut on another Linux machine with --no-switch and
# MAPS_DATA_DIR there, copy the file over and switch with --use.
#
# Options:
#   --date YYYYMMDD     the build to cut (default: the newest of
#                       build-metadata.protomaps.dev/builds.json; a build is
#                       kept about a week)
#   --bbox W,S,E,N      a box instead of region.geojson (tests of the stand)
#   --no-switch         cut and verify only; the service is not touched
#   --use FILE          no cut: verify FILE (a name in MAPS_DATA_DIR) and
#                       switch to it
#
# Environment: MAPS_DATA_DIR (default /opt/pluma-maps), the directory the
# service mounts at /data; MAPS_COMPOSE (default "docker compose -f
# docker-compose.yaml -f docker-compose.maps.yaml"), how the service is
# restarted, run in the current directory.
#
# Only tile ranges of the region are requested from build.protomaps.com:
# nothing about the users of the server leaves it. The cut is written next
# to the current one as pluma-<date>.pmtiles; the link pluma.pmtiles, which
# the styles read, moves to it once it is verified, and the old file is
# removed once check.sh passes on the new one. A failed check moves the
# link back.

set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
DATA_DIR=${MAPS_DATA_DIR:-/opt/pluma-maps}
read -ra compose <<<"${MAPS_COMPOSE:-docker compose -f docker-compose.yaml -f docker-compose.maps.yaml}"

# go-pmtiles v1.31.2 (BSD-3-Clause), linux x86_64. The release publishes no
# checksums file: the SHA-256 is of the archive as released on 2026-07-22.
PMTILES_VERSION=1.31.2
PMTILES_URL=https://github.com/protomaps/go-pmtiles/releases/download/v${PMTILES_VERSION}/go-pmtiles_${PMTILES_VERSION}_Linux_x86_64.tar.gz
PMTILES_SHA256=3ed7dbf4ec2e6dfe5e25b6f70d1ffc932729f93c86db353bf514dd71010a312f
BUILDS_URL=https://build-metadata.protomaps.dev/builds.json
BUILD_BASE=https://build.protomaps.com
# The data are z0-15: the most the builds hold; larger zooms are drawn from
# z15 tiles.
MAXZOOM=15
# The styles of @protomaps/basemaps 5 expect tiles of schema 4.
TILES_MAJOR=4
# Free space needed: the size of the cut plus this share.
SPACE_MARGIN_PERCENT=10
MEMORY_GB=${MAPS_EXTRACT_MEMORY_GB:-12}

log() { printf 'extract: %s\n' "$*" >&2; }
die() {
	log "$*"
	exit 1
}

dry_run=false
switch=true
date=
bbox=
use=
while [ $# -gt 0 ]; do
	case "$1" in
	--dry-run) dry_run=true ;;
	--no-switch) switch=false ;;
	--date)
		date=${2:?--date needs YYYYMMDD}
		shift
		;;
	--bbox)
		bbox=${2:?--bbox needs W,S,E,N}
		shift
		;;
	--use)
		use=${2:?--use needs a file name}
		shift
		;;
	-h | --help)
		sed -n '2,/^$/s/^# \{0,1\}//p' "$0"
		exit 0
		;;
	*) die "unknown argument: $1" ;;
	esac
	shift
done

[ "$(uname -s)-$(uname -m)" = Linux-x86_64 ] || die "the pinned pmtiles binary is for Linux x86_64"
for tool in curl sha256sum tar python3 df flock; do
	command -v "$tool" >/dev/null || die "$tool is missing"
done
if [ -n "$date" ]; then
	[[ "$date" =~ ^[0-9]{8}$ ]] || die "--date: YYYYMMDD expected, got $date"
fi
if [ -n "$bbox" ]; then
	[[ "$bbox" =~ ^-?[0-9.]+,-?[0-9.]+,-?[0-9.]+,-?[0-9.]+$ ]] || die "--bbox: W,S,E,N expected, got $bbox"
fi
if [ -n "$use" ]; then
	[[ "$use" =~ ^pluma-[0-9A-Za-z-]+\.pmtiles$ ]] || die "--use: a file pluma-<date>.pmtiles in $DATA_DIR expected, got $use"
	if $dry_run || ! $switch || [ -n "$date$bbox" ]; then
		die "--use goes alone"
	fi
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# 1. The pmtiles tool, checked by its pinned digest.
log "downloading go-pmtiles $PMTILES_VERSION"
curl -fsSL --retry 3 -o "$work/pmtiles.tar.gz" "$PMTILES_URL"
echo "$PMTILES_SHA256  $work/pmtiles.tar.gz" | sha256sum -c --quiet - ||
	die "go-pmtiles archive: SHA-256 mismatch"
tar -xzf "$work/pmtiles.tar.gz" -C "$work" pmtiles
pmtiles=$work/pmtiles

# verify FILE checks the structure of an archive and its tile schema.
verify() {
	"$pmtiles" verify "$1" >&2 || die "$1 failed verification; left for inspection"
	got=$("$pmtiles" show --metadata "$1" | python3 -I -c 'import json,sys; print(json.load(sys.stdin).get("version",""))') ||
		die "$1: no metadata"
	[ "${got%%.*}" = "$TILES_MAJOR" ] || die "$1 has tiles $got, $TILES_MAJOR.x expected"
}
# The data the service reads now: the target of the link, empty before the
# first cut. A regular file in its place would be lost by the switch.
if [ -e "$DATA_DIR/pluma.pmtiles" ] && [ ! -L "$DATA_DIR/pluma.pmtiles" ]; then
	die "$DATA_DIR/pluma.pmtiles is not a link: move the file to pluma-<date>.pmtiles and switch to it with --use"
fi
current=$(readlink "$DATA_DIR/pluma.pmtiles" 2>/dev/null || true)

# lock holds the data directory for this run: two runs would write the same
# .part file and move the link under each other.
lock() {
	exec 9>"$DATA_DIR/.extract.lock"
	flock -n 9 || die "another extract.sh works in $DATA_DIR"
	current=$(readlink "$DATA_DIR/pluma.pmtiles" 2>/dev/null || true)
}

if [ -n "$use" ]; then
	name=$use
	[ -f "$DATA_DIR/$name" ] || die "no $DATA_DIR/$name"
	[ -w "$DATA_DIR" ] || die "$DATA_DIR is not writable: run as its owner (root on a server)"
	lock
	[ "$current" != "$name" ] || die "$name is already the current data"
	verify "$DATA_DIR/$name"
	log "verified: $DATA_DIR/$name (tiles $got)"
else
# Steps 2-4, the cut, stay unindented down to the "fi" after step 4: they
# hold here-documents.

# 2. The build: the given date or the newest one of schema 4.
curl -fsSL --retry 3 -o "$work/builds.json" "$BUILDS_URL"
build=$(python3 -I - "$work/builds.json" "$date" "$TILES_MAJOR" <<'EOF'
import json, re, sys
builds = json.load(open(sys.argv[1]))
date, major = sys.argv[2], sys.argv[3]
ok = [b for b in builds
      if re.fullmatch(r"\d{8}\.pmtiles", b.get("key", ""))
      and str(b.get("version", "")).split(".")[0] == major]
if date:
    ok = [b for b in ok if b["key"] == date + ".pmtiles"]
if not ok:
    sys.exit("no build" + (" of " + date if date else "") + " with tiles " + major + ".x")
b = max(ok, key=lambda b: b["key"])
print(b["key"][:8], b["version"], b["size"])
EOF
) || die "build list: $build"
read -r date version planet_size <<<"$build"
url=$BUILD_BASE/$date.pmtiles
log "build $date, tiles $version, planet $((planet_size / 1000000000)) GB"

area=(--region="$here/region.geojson")
[ -z "$bbox" ] || area=(--bbox="$bbox")

avail_kb=$(awk '/^MemAvailable:/{print $2}' /proc/meminfo)
if [ -z "$bbox" ] && [ "$avail_kb" -lt $((MEMORY_GB * 1024 * 1024)) ]; then
	die "available memory $((avail_kb / 1024 / 1024)) GB, pmtiles needs about 10 GB for the region (MAPS_EXTRACT_MEMORY_GB=$MEMORY_GB); see --use"
fi

# 3. The size of the cut, without downloading the tiles.
log "dry run (reads the directories of the build: about ten minutes for the region)"
"$pmtiles" extract "$url" "$work/dry.pmtiles" "${area[@]}" --maxzoom="$MAXZOOM" \
	--download-threads=4 --dry-run 2>&1 | tee "$work/dry.log" >&2
size=$(python3 -I - "$work/dry.log" <<'EOF'
import re, sys
# "Extract transferred 98 GB (overfetch 0.05) for an archive size of 94 GB"
# (humanize.Bytes: SI units)
text = open(sys.argv[1]).read()
m = re.search(r"transferred ([\d.]+) ([kMGTP]?B) \(overfetch", text)
if not m:
    sys.exit("no size in the dry run output")
units = {"B": 1, "kB": 10**3, "MB": 10**6, "GB": 10**9, "TB": 10**12, "PB": 10**15}
print(int(float(m.group(1)) * units[m.group(2)]))
EOF
) || die "dry run: $size"
need=$((size + size * SPACE_MARGIN_PERCENT / 100))
log "download $((size / 1000000)) MB; free space needed $((need / 1000000)) MB"

if $dry_run; then
	if [ -d "$DATA_DIR" ]; then
		free=$(df -B1 --output=avail "$DATA_DIR" | tail -n1 | tr -d ' ')
		log "free in $DATA_DIR: $((free / 1000000)) MB"
	fi
	exit 0
fi

# 4. The cut, next to the current data. On a server the directory belongs
# to root (docs/ops/maps.md); a new one is created 0755 for the caller.
[ -d "$DATA_DIR" ] || install -d -m 0755 "$DATA_DIR"
[ -w "$DATA_DIR" ] || die "$DATA_DIR is not writable: run as its owner (root on a server)"
lock
free=$(df -B1 --output=avail "$DATA_DIR" | tail -n1 | tr -d ' ')
[ "$free" -ge "$need" ] || die "free in $DATA_DIR: $((free / 1000000)) MB, needed $((need / 1000000)) MB"

name=pluma-$date.pmtiles
[ -z "$bbox" ] || name=pluma-$date-bbox-$(printf '%s' "$bbox" | sha256sum | cut -c1-8).pmtiles
part=$DATA_DIR/$name.part
target=$DATA_DIR/$name
[ "$current" != "$name" ] || die "$name is already the current data"
rm -f "$part"
log "extracting into $part (hours for the whole region)"
"$pmtiles" extract "$url" "$part" "${area[@]}" --maxzoom="$MAXZOOM" --download-threads=4 >&2
verify "$part"
chmod 0644 "$part"
mv -f "$part" "$target"
log "verified: $target ($(du -h "$target" | cut -f1), tiles $got)"

$switch || exit 0
fi # the cut

# 5. Switch the link, restart the service, check it; roll back on failure.
point() {
	ln -sfn "$1" "$DATA_DIR/pluma.pmtiles.new"
	mv -T "$DATA_DIR/pluma.pmtiles.new" "$DATA_DIR/pluma.pmtiles"
}
point "$name"
if [ -n "$current" ]; then
	"${compose[@]}" restart maps >&2
else
	# The first data: the service may not exist yet.
	"${compose[@]}" up -d maps >&2
fi
if "$here/check.sh"; then
	if [ -n "$current" ] && [ "$current" != "$name" ] && [ -f "$DATA_DIR/$current" ]; then
		rm -f "$DATA_DIR/$current"
		log "removed the previous data $current"
	fi
	log "the maps service draws from $name"
	exit 0
fi
if [ -n "$current" ]; then
	log "check failed: back to $current; $name left for inspection"
	point "$current"
	"${compose[@]}" restart maps >&2
	"$here/check.sh" || log "the previous data fail the check too"
else
	log "check failed; $name is linked and left for inspection"
fi
exit 1
