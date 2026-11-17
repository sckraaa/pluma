#!/usr/bin/env bash
# Checks the running maps service (task 100, ADR 0063; docs/ops/maps.md):
# after a deployment, a restart or new data. Run from the clone of the
# repository, as the user who runs compose:
#
#   teamgramd/deploy/maps/check.sh
#
# Waits for /health, then, for every style of the image, draws inside the
# container (the service has no published port and its network has no way
# out) what bff asks for:
#   - Red Square, z15, 277x195@2x (a chat bubble of Android or Web K):
#     a PNG of exactly 554x390 with more than one colour;
#   - a point of the Atlantic outside the region (30N 30W), z15, 277x195:
#     a PNG of exactly 277x195 of one colour, the background of the style.
# Exits 0 when every image is right.
#
# Environment: MAPS_COMPOSE as in extract.sh; MAPS_CHECK_TIMEOUT, seconds to
# wait for the service to be ready (default 180).

set -euo pipefail

read -ra compose <<<"${MAPS_COMPOSE:-docker compose -f docker-compose.yaml -f docker-compose.maps.yaml}"
timeout=${MAPS_CHECK_TIMEOUT:-180}

# The script runs in the container's own Node.js, with the image's sharp
# (a dependency of tileserver-gl) to read the pixels; it prints one line per
# image and exits 1 on the first wrong one.
"${compose[@]}" exec -T -w /usr/src/app -e CHECK_TIMEOUT="$timeout" maps node - <<'EOF'
const fs = require('fs');
const sharp = require('sharp');

const base = 'http://127.0.0.1:8080';
const config = JSON.parse(fs.readFileSync('/app/config.json', 'utf8'));
const styles = Object.keys(config.styles || {});
const cases = [
  { name: 'Red Square', path: '37.617635,55.755814,15/277x195@2x.png', w: 554, h: 390, uniform: false },
  { name: 'Atlantic', path: '-30.000000,30.000000,15/277x195.png', w: 277, h: 195, uniform: true },
];

async function ready(deadline) {
  for (;;) {
    try {
      const res = await fetch(base + '/health');
      if (res.status === 200) return;
    } catch (_) {
      // not listening yet
    }
    if (Date.now() > deadline) throw new Error('not ready: /health is not 200');
    await new Promise((r) => setTimeout(r, 2000));
  }
}

async function main() {
  if (styles.length === 0) throw new Error('no styles in /app/config.json');
  await ready(Date.now() + Number(process.env.CHECK_TIMEOUT) * 1000);
  for (const style of styles) {
    for (const c of cases) {
      const started = Date.now();
      const res = await fetch(`${base}/styles/${style}/static/${c.path}`, { redirect: 'error' });
      const body = Buffer.from(await res.arrayBuffer());
      const ms = Date.now() - started;
      if (res.status !== 200) throw new Error(`${style} ${c.name}: HTTP ${res.status}`);
      const meta = await sharp(body).metadata();
      if (meta.format !== 'png') throw new Error(`${style} ${c.name}: ${meta.format}, png expected`);
      if (meta.width !== c.w || meta.height !== c.h) {
        throw new Error(`${style} ${c.name}: ${meta.width}x${meta.height}, ${c.w}x${c.h} expected`);
      }
      const { channels } = await sharp(body).stats();
      const uniform = channels.every((ch) => ch.min === ch.max);
      if (uniform !== c.uniform) {
        throw new Error(`${style} ${c.name}: ${uniform ? 'one colour' : 'more than one colour'}, ${c.uniform ? 'one colour' : 'a map'} expected`);
      }
      console.log(`ok ${style} ${c.name}: ${meta.width}x${meta.height}, ${body.length} bytes, ${ms} ms`);
    }
  }
}

main().catch((err) => {
  console.error('FAIL ' + err.message);
  process.exit(1);
});
EOF
