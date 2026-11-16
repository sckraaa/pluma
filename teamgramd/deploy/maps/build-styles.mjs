// Builds the map styles of the maps service (task 100, ADR 0063, p. 5), one
// per language of the labels, and the tileserver-gl config that serves them:
//
//   node build-styles.mjs <langs> <config template> <output dir>
//
// <langs> is a comma-separated list such as "ru,en" (MAPS_STYLES of bff: it
// asks for a style by the language). Each style is the "light" flavor of
// @protomaps/basemaps with labels in that language
// (https://docs.protomaps.com/basemaps/localization); it reads only local
// files: the tiles through the "protomaps" data source of the config
// (pluma.pmtiles in /data), the glyphs and the sprite from the image. No
// source attribution is drawn: bff draws "© OpenStreetMap" itself, at the
// bottom left, where the clients draw no message time.
//
// Writes <output dir>/styles/<lang>.json and <output dir>/config.json: the
// template with its "styles" replaced by one rendered style per language
// (only static images are asked for; the style JSON, glyphs and sprites are
// not served).

import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { language_script_pairs, layers, namedFlavor } from '@protomaps/basemaps';

const [langsArg, templatePath, outDir] = process.argv.slice(2);
if (!langsArg || !templatePath || !outDir) {
  console.error('usage: node build-styles.mjs <langs> <config template> <output dir>');
  process.exit(2);
}

const known = new Set(language_script_pairs.map((p) => p.lang));
const langs = langsArg.split(',').map((s) => s.trim()).filter((s) => s !== '');
if (langs.length === 0) {
  console.error('no languages given');
  process.exit(2);
}
for (const lang of langs) {
  // The same rule as bff's style names (pkg/staticmap Config.Validate).
  if (!/^[a-z-]{2,10}$/.test(lang)) {
    console.error(`bad language "${lang}": [a-z-]{2,10} expected`);
    process.exit(2);
  }
  if (!known.has(lang)) {
    console.error(`@protomaps/basemaps has no labels in "${lang}"; known: ${[...known].sort().join(',')}`);
    process.exit(2);
  }
}
if (new Set(langs).size !== langs.length) {
  console.error(`a language is listed twice: ${langsArg}`);
  process.exit(2);
}

const config = JSON.parse(readFileSync(templatePath, 'utf8'));
const dataIds = Object.keys(config.data || {});
if (dataIds.length !== 1 || dataIds[0] !== 'protomaps') {
  console.error('the config template must have exactly one data source, "protomaps"');
  process.exit(2);
}

mkdirSync(join(outDir, 'styles'), { recursive: true });
config.styles = {};
for (const lang of langs) {
  const style = {
    version: 8,
    name: `pluma-${lang}`,
    sources: {
      protomaps: {
        type: 'vector',
        // The data source of the config by its id: the file in paths.pmtiles.
        url: 'pmtiles://{protomaps}',
      },
    },
    // Relative to paths.fonts and paths.sprites of the config.
    glyphs: '{fontstack}/{range}.pbf',
    sprite: 'light',
    layers: layers('protomaps', namedFlavor('light'), { lang }),
  };
  writeFileSync(join(outDir, 'styles', `${lang}.json`), JSON.stringify(style));
  config.styles[lang] = { style: `${lang}.json`, serve_rendered: true, serve_data: false };
}
writeFileSync(join(outDir, 'config.json'), JSON.stringify(config, null, 2) + '\n');
console.log(`styles: ${langs.join(', ')}`);
