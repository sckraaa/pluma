#!/usr/bin/env bash
# Checks the running maps services (docs/ops/maps.md): after a deployment, a
# restart or new data. Run from the clone of the repository, as the user who
# runs compose:
#
#   teamgramd/deploy/maps/check.sh
#
# The maps service (task 100, ADR 0063): waits for /health, then, for every
# style of the image, draws inside the container (the service has no
# published port and its network has no way out) what bff asks for:
#   - Red Square, z15, 277x195@2x (a chat bubble of Android or Web K):
#     a PNG of exactly 554x390 with more than one colour;
#   - a point of the Atlantic outside the region (30N 30W), z15, 277x195:
#     a PNG of exactly 277x195 of one colour, the background of the style.
#
# The public map tiles (task 112, ADR 0070), when maps-tiles runs: the
# compose files must then include docker-compose.web.yaml (MAPS_COMPOSE).
# From inside maps-tiles (its only network is the one it shares with Caddy):
#   - maps-tiles itself, as Caddy asks it: every style JSON of
#     config-public.json, with links under https://<domain>/<key>/ only; the
#     TileJSON names OpenStreetMap; no static image;
#   - Caddy, by the site's name with its certificate: a style JSON, one
#     tile, one glyph range and a sprite with the key, with CORS for every
#     origin; the preflight; 404 without the key, with another key, for a
#     static image and for the list of styles.
# The name and the key are read from the web container; nothing is printed
# of the key.
#
# Exits 0 when everything is right.
#
# Environment: MAPS_COMPOSE as in extract.sh; MAPS_CHECK_TIMEOUT, seconds to
# wait for the services to be ready (default 180).

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

# The public map tiles: only where maps-tiles runs.
if [[ -z $("${compose[@]}" ps --status running -q maps-tiles 2>/dev/null) ]]; then
	echo "maps-tiles is not running: the public map tiles are not checked"
	exit 0
fi
domain=$("${compose[@]}" exec -T web printenv PLUMA_MAPS_DOMAIN || true)
key=$("${compose[@]}" exec -T web printenv MAPS_PUBLIC_KEY || true)
if [[ -z $domain || -z $key ]]; then
	echo "FAIL maps-tiles runs, but no PLUMA_MAPS_DOMAIN or MAPS_PUBLIC_KEY is read from the web container (MAPS_COMPOSE must include docker-compose.web.yaml; docs/ops/maps.md)" >&2
	exit 1
fi
"${compose[@]}" exec -T -w /usr/src/app -e CHECK_TIMEOUT="$timeout" -e MAPS_DOMAIN="$domain" -e MAPS_KEY="$key" maps-tiles node - <<'EOF'
const fs = require('fs');
const http = require('http');
const https = require('https');

const domain = process.env.MAPS_DOMAIN;
const key = process.env.MAPS_KEY;
const prefix = `https://${domain}/${key}/`;
const asCaddy = { Host: domain, 'X-Forwarded-Proto': 'https', 'X-Forwarded-Path': '/' + key };
const config = JSON.parse(fs.readFileSync('/app/config-public.json', 'utf8'));
const styles = Object.keys(config.styles || {});

// The errors never name the key: a path is printed with <key> in its place.
const shown = (s) => s.split(key).join('<key>');

// A request by a module of node (http or https) with the options opts; the
// body is read whole. fetch is not used: it does not send a Host of one's
// own.
function request(mod, opts) {
  return new Promise((resolve, reject) => {
    const req = mod.request({ timeout: 15000, ...opts }, (res) => {
      const chunks = [];
      res.on('data', (c) => chunks.push(c));
      res.on('end', () => resolve({ status: res.statusCode, headers: res.headers, body: Buffer.concat(chunks) }));
    });
    req.on('timeout', () => req.destroy(new Error('timeout')));
    req.on('error', reject);
    req.end();
  });
}

// maps-tiles itself, asked as Caddy asks it.
const tiles = (path) => request(http, { host: '127.0.0.1', port: 8080, path, headers: asCaddy });

// Caddy on the network shared with it, by the site's name.
const caddy = (method, path) =>
  request(https, { host: 'web', port: 443, servername: domain, method, path, headers: { Host: domain, Origin: 'null' } });

async function ready(deadline) {
  for (;;) {
    try {
      if ((await tiles('/health')).status === 200) return;
    } catch (_) {
      // not listening yet
    }
    if (Date.now() > deadline) throw new Error('not ready: /health is not 200');
    await new Promise((r) => setTimeout(r, 2000));
  }
}

function under(what, url) {
  if (typeof url !== 'string' || !url.startsWith(prefix)) throw new Error(`${what}: ${shown(String(url))} is not under https://${domain}/<key>/`);
}

async function main() {
  if (styles.length === 0) throw new Error('no styles in /app/config-public.json');
  await ready(Date.now() + Number(process.env.CHECK_TIMEOUT) * 1000);

  let glyphs = '';
  for (const style of styles) {
    const res = await tiles(`/styles/${style}/style.json`);
    if (res.status !== 200) throw new Error(`${style}: style.json HTTP ${res.status}`);
    const json = JSON.parse(res.body.toString('utf8'));
    under(`${style} glyphs`, json.glyphs);
    under(`${style} sprite`, json.sprite);
    for (const [id, source] of Object.entries(json.sources || {})) under(`${style} source ${id}`, source.url);
    glyphs = json.glyphs;
    console.log(`ok ${style}: style.json, links under the public prefix`);
  }
  const tjRes = await tiles('/data/protomaps.json');
  if (tjRes.status !== 200) throw new Error(`the TileJSON: HTTP ${tjRes.status}`);
  const tj = JSON.parse(tjRes.body.toString('utf8'));
  if (!/OpenStreetMap/.test(tj.attribution || '')) throw new Error('the TileJSON does not name OpenStreetMap');
  under('the tiles of the TileJSON', (tj.tiles || [])[0]);
  console.log(`ok TileJSON: attribution, tiles z${tj.minzoom}-${tj.maxzoom}`);
  const stat = await tiles(`/styles/${styles[0]}/static/37.617635,55.755814,15/277x195.png`);
  if (stat.status !== 404) throw new Error(`a static image answered ${stat.status}, 404 expected`);
  console.log('ok maps-tiles: no static image');

  const base = `/${key}`;
  const font = glyphs.slice(prefix.length - 1).replace('{fontstack}', encodeURIComponent('Noto Sans Regular')).replace('{range}', '0-255');
  const good = [
    [`${base}/styles/${styles[0]}/style.json`, 'a style JSON'],
    [`${base}/data/protomaps/0/0/0.pbf`, 'the tile 0/0/0'],
    [`${base}${font}`, 'the glyphs 0-255 of Noto Sans Regular'],
    [`${base}/styles/${styles[0]}/sprite@2x.png`, 'the sprite @2x'],
  ];
  for (const [path, what] of good) {
    const res = await caddy('GET', path);
    if (res.status !== 200) throw new Error(`Caddy, ${what}: HTTP ${res.status}`);
    if (res.headers['access-control-allow-origin'] !== '*') throw new Error(`Caddy, ${what}: no CORS for every origin`);
    if (res.headers.server) throw new Error(`Caddy, ${what}: a Server header`);
    console.log(`ok Caddy ${what}: ${res.body.length} bytes, ${res.headers['cache-control']}`);
  }
  const pre = await caddy('OPTIONS', `${base}/styles/${styles[0]}/style.json`);
  if (pre.status !== 204 || pre.headers['access-control-allow-origin'] !== '*') throw new Error(`Caddy, the preflight: HTTP ${pre.status}`);
  console.log('ok Caddy preflight');
  const other = key.slice(0, -1) + (key.endsWith('A') ? 'B' : 'A');
  const refused = [
    [`/styles/${styles[0]}/style.json`, 'without the key'],
    [`/${other}/styles/${styles[0]}/style.json`, 'with another key'],
    [`${base}/styles/${styles[0]}/static/37.617635,55.755814,15/277x195.png`, 'a static image'],
    [`${base}/styles.json`, 'the list of styles'],
  ];
  for (const [path, what] of refused) {
    const res = await caddy('GET', path);
    if (res.status !== 404) throw new Error(`Caddy, ${what}: HTTP ${res.status}, 404 expected`);
    console.log(`ok Caddy refuses ${what}`);
  }
}

main().catch((err) => {
  console.error('FAIL ' + shown(String(err.message)));
  process.exit(1);
});
EOF
