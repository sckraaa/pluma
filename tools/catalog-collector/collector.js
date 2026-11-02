// Copyright 2026 The Pluma Authors
//  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// The collector of the catalog from Telegram (task 024, ADR 0028;
// docs/ops/catalog.md). It runs in a tab of Telegram Web K
// (web.telegram.org/k) that is already logged in, through the page's own
// API client (rootScope.managers.apiManager.invokeApi), and only reads:
// the answers of the catalog's methods, every sticker set they name, and
// the files of every document, raw (upload.getFile, not tweb's download,
// which converts files and caches them). What it collects goes to put(path,
// body), with manifest.json last; pkg/catalog/importer reads it.
//
// It stops on any sign the session is at risk: an error 401 or 406, a key
// or session error, or a FLOOD_WAIT over two minutes. Metadata is asked
// one call at a time with a pause; files at most four at once.
//
//   await collectCatalog({put: async (path, body) => {…}, log: console.log})

/* eslint-disable no-await-in-loop */
async function collectCatalog({put, log = () => {}, maxParallel = 4, pauseMs = [300, 800]}) {
  const LAYER = 228;
  const api = rootScope.managers.apiManager;

  // Stop for good on what could hurt the account.
  const fatal = (e) => e && (e.code === 401 || e.code === 406 ||
    /^(AUTH_KEY|SESSION_|USER_DEACTIVATED)/.test(e.type || ''));
  let stopped = null;
  const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
  const pause = () => sleep(pauseMs[0] + Math.random() * (pauseMs[1] - pauseMs[0]));

  async function call(method, params, options = {}) {
    if (stopped) throw stopped;
    try {
      return await api.invokeApi(method, params, {noErrorBox: true, floodMaxTimeout: 120, ...options});
    } catch (e) {
      if (fatal(e) || /^FLOOD_WAIT/.test(e && e.type || '')) {
        stopped = new Error(`collector: stopped on ${method}: ${e.type || e.code}`);
        throw stopped;
      }
      throw e;
    }
  }

  // JSON with bytes as {"$b64": …} (pkg/catalog/tl).
  const b64 = (u) => {
    let s = '';
    for (let i = 0; i < u.length; i += 0x8000) s += String.fromCharCode.apply(null, u.subarray(i, i + 0x8000));
    return btoa(s);
  };
  const json = (v) => JSON.stringify(v, (k, x) => x instanceof Uint8Array ? {$b64: b64(x)} : x);
  const id = (v) => String(v);

  const manifest = {layer: LAYER, client: `tweb ${typeof App !== 'undefined' && App.version || ''}`.trim(), collected_at: Math.floor(Date.now() / 1000),
    answers: [], sets: [], extra_documents: [], wallpapers: [], files: []};
  const objects = []; // everything collected, for documents and sets
  let n = 0;

  async function answer(method, params, arg = '') {
    const r = await call(method, params);
    const file = `answers/${String(++n).padStart(4, '0')}-${method}${arg ? '-' + arg : ''}.json`;
    await put(file, json(r));
    manifest.answers.push({method, arg, file});
    objects.push(r);
    await pause();
    return r;
  }

  // 1. The answers of the catalog's methods (app/bff/catalog).
  for (const m of ['messages.getAvailableReactions', 'messages.getDefaultTagReactions', 'messages.getAvailableEffects',
    'messages.getFeaturedStickers', 'messages.getFeaturedEmojiStickers', 'messages.getEmojiGroups',
    'messages.getEmojiStatusGroups', 'messages.getEmojiProfilePhotoGroups', 'messages.getEmojiStickerGroups',
    'help.getPeerColors', 'help.getPeerProfileColors', 'help.getTimezonesList', 'account.getChatThemes',
    'account.getWallPapers', 'account.getDefaultEmojiStatuses', 'account.getChannelDefaultEmojiStatuses',
    'account.getDefaultProfilePhotoEmojis', 'account.getDefaultGroupPhotoEmojis',
    'account.getDefaultBackgroundEmojis', 'account.getChannelRestrictedStatusEmojis']) {
    await answer(m, {hash: 0});
  }
  await answer('messages.getTopReactions', {limit: 100, hash: 0});
  for (const format of ['android', 'ios', 'macos', 'tdesktop']) {
    await answer('account.getThemes', {format, hash: 0}, format);
  }
  // The trending sets past the first page, as one answer.
  {
    const all = {_: 'messages.featuredStickers', pFlags: {}, hash: 0, count: 0, sets: [], unread: []};
    for (let offset = 0; offset < 10000;) {
      const r = await call('messages.getOldFeaturedStickers', {offset, limit: 100, hash: 0});
      all.sets.push(...r.sets);
      all.count = r.count;
      await pause();
      if (!r.sets.length || all.sets.length >= r.count) break;
      offset += r.sets.length;
    }
    const file = `answers/${String(++n).padStart(4, '0')}-messages.getOldFeaturedStickers.json`;
    await put(file, json(all));
    manifest.answers.push({method: 'messages.getOldFeaturedStickers', arg: '', file});
    objects.push(all);
  }
  // Emoji keywords of every language Telegram has them for.
  {
    const langs = await call('messages.getEmojiKeywordsLanguages', {lang_codes: [
      'en', 'ru', 'uk', 'be', 'kk', 'uz', 'de', 'fr', 'es', 'it', 'pt', 'nl', 'pl', 'cs', 'sk', 'sl', 'hr', 'sr', 'bg',
      'ro', 'hu', 'fi', 'sv', 'nb', 'da', 'et', 'lv', 'lt', 'el', 'tr', 'az', 'hy', 'ka', 'he', 'ar', 'fa', 'ur', 'hi',
      'bn', 'mr', 'gu', 'ta', 'te', 'kn', 'ml', 'si', 'th', 'vi', 'id', 'ms', 'fil', 'ja', 'ko', 'zh', 'zh-hans',
      'zh-hant', 'ca', 'eu', 'gl', 'ga', 'cy', 'is', 'mk', 'sq', 'af', 'sw', 'am', 'ne', 'km', 'lo', 'my', 'mn']});
    await pause();
    for (const l of langs) await answer('messages.getEmojiKeywords', {lang_code: l.lang_code}, l.lang_code);
  }
  log(`collector: ${manifest.answers.length} answers`);

  // 2. Documents named only by id: custom emoji of lists, statuses, groups,
  // tag reactions.
  // The documents clients name by id in their code, which keep their id
  // (pkg/catalog/importer KeptIDs).
  const named = new Set(['5269590556232664327', '5390854796011906616', '5471952986970267163', '5386654653003864312',
    '5373141891321699086', '5197252827247841976']);
  const walk = (v, f) => {
    if (Array.isArray(v)) { v.forEach((x) => walk(x, f)); return; }
    if (v && typeof v === 'object' && !(v instanceof Uint8Array)) { f(v); Object.values(v).forEach((x) => walk(x, f)); }
  };
  walk(objects, (o) => {
    if (o._ === 'emojiList') o.document_id.forEach((x) => named.add(id(x)));
    if (o._ === 'emojiStatus' || o._ === 'reactionCustomEmoji') named.add(id(o.document_id));
    if (/^emojiGroup/.test(o._)) { if (o.icon_emoji_id) named.add(id(o.icon_emoji_id)); (o.document_id || []).forEach((x) => named.add(id(x))); }
  });
  const known = new Set();
  walk(objects, (o) => { if (o._ === 'document') known.add(id(o.id)); });
  const missing = [...named].filter((x) => x !== '0' && !known.has(x));
  for (let i = 0; i < missing.length; i += 100) {
    const docs = await call('messages.getCustomEmojiDocuments', {document_id: missing.slice(i, i + 100)});
    for (const d of docs) {
      const file = `documents/${id(d.id)}.json`;
      await put(file, json(d));
      manifest.extra_documents.push(file);
      objects.push(d);
    }
    await pause();
  }

  // 3. Sets: those asked for by kind or name, and every set named, until
  // no set names one not collected yet.
  const sets = new Map(); // Telegram's id → collected
  async function set(input, kind = '') {
    let r;
    try {
      r = await call('messages.getStickerSet', {stickerset: input, hash: 0});
    } catch (e) {
      if (stopped) throw e;
      log(`collector: no set ${JSON.stringify(input)}: ${e.type || e}`);
      return;
    }
    await pause();
    const key = id(r.set.id);
    if (sets.has(key)) {
      if (kind) manifest.sets.find((s) => s.id === key).kind = kind;
      return;
    }
    const file = `sets/${key}.json`;
    await put(file, json(r));
    sets.set(key, r);
    manifest.sets.push({file, kind, id: key});
    objects.push(r);
  }
  for (const kind of ['inputStickerSetAnimatedEmoji', 'inputStickerSetAnimatedEmojiAnimations', 'inputStickerSetPremiumGifts',
    'inputStickerSetEmojiGenericAnimations', 'inputStickerSetEmojiDefaultStatuses', 'inputStickerSetEmojiDefaultTopicIcons',
    'inputStickerSetEmojiChannelDefaultStatuses', 'inputStickerSetTonGifts']) {
    await set({_: kind}, kind);
  }
  for (const emoticon of ['🎲', '🎯', '🏀', '⚽', '🎳', '🎰']) {
    await set({_: 'inputStickerSetDice', emoticon}, `inputStickerSetDice:${emoticon}`);
  }
  for (const short_name of ['EmojiAnimations']) await set({_: 'inputStickerSetShortName', short_name});
  for (;;) {
    const want = new Map();
    walk(objects, (o) => {
      if (o._ === 'inputStickerSetID' && !sets.has(id(o.id))) want.set(id(o.id), o);
      if (o._ === 'stickerSet' && !sets.has(id(o.id))) want.set(id(o.id), {_: 'inputStickerSetID', id: o.id, access_hash: o.access_hash});
    });
    if (!want.size) break;
    for (const input of want.values()) await set(input);
    // A set that cannot be had is not asked for again.
    for (const k of want.keys()) if (!sets.has(k)) sets.set(k, null);
  }
  manifest.sets = manifest.sets.map(({file, kind}) => ({file, kind}));
  log(`collector: ${[...sets.values()].filter(Boolean).length} sets`);

  // 4. Wallpapers named in code by clients (docs/tasks/024-research).
  for (const slug of ['fqv01SQemVIBAAAApND8LDRUhRU']) {
    try {
      const w = await call('account.getWallPaper', {wallpaper: {_: 'inputWallPaperSlug', slug}});
      const file = `wallpapers/${slug}.json`;
      await put(file, json(w));
      manifest.wallpapers.push(file);
      objects.push(w);
    } catch (e) {
      if (stopped) throw e;
      log(`collector: no wallpaper ${slug}: ${e.type || e}`);
    }
    await pause();
  }

  // 5. The files of every document: its own and its downloadable
  // thumbnails, raw, in parts of 1 MiB (core.telegram.org/api/files).
  const docs = new Map();
  walk(objects, (o) => { if (o._ === 'document' && !docs.has(id(o.id))) docs.set(id(o.id), o); });
  const jobs = [];
  for (const d of docs.values()) {
    const entry = {id: id(d.id), file: `files/${id(d.id)}`, thumbs: {}};
    jobs.push({d, type: '', path: entry.file});
    for (const t of [...(d.thumbs || []), ...(d.video_thumbs || [])]) {
      if (t._ === 'photoSize' || t._ === 'photoSizeProgressive' || t._ === 'videoSize') {
        entry.thumbs[t.type] = `files/${id(d.id)}.${t.type}`;
        jobs.push({d, type: t.type, path: entry.thumbs[t.type]});
      }
    }
    manifest.files.push(entry);
  }
  log(`collector: ${docs.size} documents, ${jobs.length} files`);
  let done = 0;
  // A file reference expires (FILE_REFERENCE_EXPIRED); a document's fresh
  // one comes with its set again.
  async function refresh(d) {
    const ref = (d.attributes || []).map((a) => a.stickerset).find((s) => s && s._ === 'inputStickerSetID');
    if (!ref) return false;
    const r = await call('messages.getStickerSet', {stickerset: ref, hash: 0});
    const fresh = r.documents.find((x) => id(x.id) === id(d.id));
    if (!fresh) return false;
    d.file_reference = fresh.file_reference;
    return true;
  }
  async function download(job) {
    try {
      await fetchFile(job);
    } catch (e) {
      if (stopped || !/^FILE_REFERENCE_/.test(e && e.type || '') || !(await refresh(job.d))) throw e;
      await fetchFile(job);
    }
  }
  async function fetchFile({d, type, path}) {
    const location = {_: 'inputDocumentFileLocation', id: d.id, access_hash: d.access_hash, file_reference: d.file_reference, thumb_size: type};
    const parts = [];
    for (let offset = 0; ; offset += 1 << 20) {
      const r = await call('upload.getFile', {location, offset, limit: 1 << 20}, {dcId: d.dc_id, fileDownload: true});
      if (r._ !== 'upload.file') throw new Error(`collector: ${r._} for ${d.id}`);
      parts.push(r.bytes);
      if (r.bytes.length < (1 << 20)) break;
    }
    const size = parts.reduce((a, p) => a + p.length, 0);
    const body = new Uint8Array(size);
    let at = 0;
    for (const p of parts) { body.set(p, at); at += p.length; }
    if (!type && d.size && size !== Number(d.size)) throw new Error(`collector: ${d.id}: ${size} of ${d.size} bytes`);
    await put(path, body);
    if (++done % 500 === 0) log(`collector: ${done} of ${jobs.length} files`);
  }
  const queue = jobs.slice();
  const failed = [];
  await Promise.all(Array.from({length: maxParallel}, async () => {
    while (queue.length && !stopped) {
      const job = queue.shift();
      try {
        await download(job);
      } catch (e) {
        if (stopped) return;
        failed.push(`${job.d.id}/${job.type}: ${e.type || e.message || e}`);
      }
    }
  }));
  if (stopped) throw stopped;
  if (failed.length) {
    await put('failed.json', JSON.stringify(failed));
    throw new Error(`collector: ${failed.length} files failed (failed.json); no manifest written`);
  }
  await put('manifest.json', JSON.stringify(manifest));
  log(`collector: done, ${jobs.length} files`);
  return {answers: manifest.answers.length, sets: manifest.sets.length, documents: docs.size, files: jobs.length};
}
