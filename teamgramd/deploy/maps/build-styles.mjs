// Builds the map styles of the maps image (task 100, ADR 0063, p. 5; task
// 112, ADR 0070), one per language of the labels, and the two tileserver-gl
// configs that serve them:
//
//   node build-styles.mjs <langs> <config template> <output dir>
//
// <langs> is a comma-separated list such as "ru,en" (MAPS_STYLES of bff: it
// asks for a style by the language, and hands the list to the clients).
// Each light style <lang> is the "light" flavor of @protomaps/basemaps with
// labels in that language (https://docs.protomaps.com/basemaps/localization),
// each dark style <lang>-dark the "dark" flavor with its sprite. They read
// only local files: the tiles through the "protomaps" data source of the
// config (pluma.pmtiles in /data), the glyphs and the sprites from the image.
//
// Writes into <output dir>:
// - styles/<lang>.json and styles/<lang>-dark.json;
// - config.json, of the maps service: the template with its "styles"
//   replaced by one rendered light style per language. Only static images
//   are asked for; the style JSON, glyphs and sprites are not served. No
//   source attribution is drawn: bff draws "© OpenStreetMap" itself, at the
//   bottom left, where the clients draw no message time.
// - config-public.json, of the maps-tiles service: the same paths and data,
//   the light and the dark style of each language served as JSON with their
//   glyphs and sprites (serve_data), and the vector tiles, for the clients'
//   own renderers; no renderer, no static image, no front page. The
//   TileJSON of the data names OpenStreetMap, which the attribution control
//   of the clients' maps shows (ODbL, ADR 0063, p. 6).
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

// The style of one flavor of @protomaps/basemaps with labels in lang.
function style(name, flavor, sprite, lang) {
  return {
    version: 8,
    name,
    sources: {
      protomaps: {
        type: 'vector',
        // The data source of the config by its id: the file in paths.pmtiles.
        url: 'pmtiles://{protomaps}',
      },
    },
    // Relative to paths.fonts and paths.sprites of the config.
    glyphs: '{fontstack}/{range}.pbf',
    sprite,
    layers: layers('protomaps', namedFlavor(flavor), { lang }),
  };
}

mkdirSync(join(outDir, 'styles'), { recursive: true });

const publicConfig = {
  options: {
    paths: config.options.paths,
    frontPage: false,
    serveAllFonts: false,
    serveAllStyles: false,
    serveStaticMaps: false,
  },
  styles: {},
  data: {
    protomaps: {
      ...config.data.protomaps,
      tilejson: {
        attribution: '<a href="https://www.openstreetmap.org/copyright">© OpenStreetMap</a>',
      },
    },
  },
};

config.styles = {};
for (const lang of langs) {
  const dark = `${lang}-dark`;
  writeFileSync(join(outDir, 'styles', `${lang}.json`), JSON.stringify(style(`pluma-${lang}`, 'light', 'light', lang)));
  writeFileSync(join(outDir, 'styles', `${dark}.json`), JSON.stringify(style(`pluma-${dark}`, 'dark', 'dark', lang)));
  config.styles[lang] = { style: `${lang}.json`, serve_rendered: true, serve_data: false };
  publicConfig.styles[lang] = { style: `${lang}.json`, serve_rendered: false, serve_data: true };
  publicConfig.styles[dark] = { style: `${dark}.json`, serve_rendered: false, serve_data: true };
}
writeFileSync(join(outDir, 'config.json'), JSON.stringify(config, null, 2) + '\n');
writeFileSync(join(outDir, 'config-public.json'), JSON.stringify(publicConfig, null, 2) + '\n');
console.log(`styles: ${langs.join(', ')}, each light and dark`);
