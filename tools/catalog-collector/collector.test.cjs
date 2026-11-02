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

// node --test tools/catalog-collector/collector.test.cjs
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

// A Telegram of two sets: the reaction's document is in set 1, whose
// sticker refers to set 2 by id; set 2 comes only through that reference.
function fakeTelegram({failOn} = {}) {
  const doc = (id, set) => ({_: 'document', id, access_hash: 5, file_reference: new Uint8Array([1]), dc_id: 2, size: 3,
    mime_type: 'application/x-tgsticker',
    thumbs: [{_: 'photoPathSize', type: 'j', bytes: new Uint8Array([9])}, {_: 'photoSize', type: 'm', w: 1, h: 1, size: 2}],
    attributes: [{_: 'documentAttributeSticker', alt: '', stickerset: {_: 'inputStickerSetID', id: set, access_hash: 7}}]});
  const sets = {
    1: {_: 'messages.stickerSet', set: {_: 'stickerSet', id: 1, access_hash: 7, short_name: 'One'}, packs: [], keywords: [], documents: [doc(11, 2)]},
    2: {_: 'messages.stickerSet', set: {_: 'stickerSet', id: 2, access_hash: 7, short_name: 'Two'}, packs: [], keywords: [], documents: [doc(22, 2)]},
  };
  const calls = [];
  const invokeApi = async (method, params) => {
    calls.push(method);
    if (failOn && method === failOn) throw {code: 401, type: 'AUTH_KEY_UNREGISTERED'};
    switch (method) {
      case 'messages.getAvailableReactions':
        return {_: 'messages.availableReactions', hash: 1, reactions: [{_: 'availableReaction', reaction: '😀', static_icon: doc(11, 1)}]};
      case 'messages.getOldFeaturedStickers':
        return {_: 'messages.featuredStickers', count: 0, sets: [], unread: []};
      case 'messages.getEmojiKeywordsLanguages':
        return [{_: 'emojiLanguage', lang_code: 'en'}];
      case 'messages.getStickerSet': {
        const s = params.stickerset;
        if (s._ === 'inputStickerSetID') return sets[s.id];
        if (s._ === 'inputStickerSetAnimatedEmoji') return sets[1];
        throw {code: 400, type: 'STICKERSET_INVALID'};
      }
      case 'account.getWallPaper':
        throw {code: 400, type: 'WALLPAPER_INVALID'};
      case 'messages.getCustomEmojiDocuments':
        return params.document_id.includes('5269590556232664327') ? [doc(5269590556232664327n.toString(), 2)] : [];
      case 'upload.getFile':
        return {_: 'upload.file', bytes: params.location.thumb_size ? new Uint8Array([4, 4]) : new Uint8Array([1, 2, 3])};
      default:
        return {_: 'emojiList', hash: 0, document_id: []};
    }
  };
  return {invokeApi, calls};
}

function load(tg) {
  const ctx = {rootScope: {managers: {apiManager: {invokeApi: tg.invokeApi}}}, App: {version: 'test'}, btoa, setTimeout, Uint8Array, JSON, Math, Date, Promise, Error, String, Number, Array, Object, Map, Set, RegExp};
  vm.createContext(ctx);
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'collector.js'), 'utf8'), ctx);
  return ctx.collectCatalog;
}

test('collects answers, every named set and the files, the manifest last', async () => {
  const tg = fakeTelegram();
  const out = new Map();
  const r = await load(tg)({put: async (p, b) => out.set(p, b), pauseMs: [0, 0]});
  const names = [...out.keys()];
  assert.equal(names[names.length - 1], 'manifest.json');
  const m = JSON.parse(out.get('manifest.json'));
  assert.equal(m.layer, 228);
  assert.deepEqual(m.sets.map((s) => s.kind).sort(), ['', 'inputStickerSetAnimatedEmoji']);
  assert.ok(out.has('sets/2.json'), 'the set named only by a sticker');
  assert.deepEqual(m.files.map((f) => f.id).sort(), ['11', '22', '5269590556232664327']);
  assert.deepEqual(m.extra_documents, ['documents/5269590556232664327.json']);
  assert.deepEqual(m.files[0].thumbs, {m: `files/${m.files[0].id}.m`});
  assert.deepEqual([...out.get(`files/${m.files[0].id}`)], [1, 2, 3]);
  assert.equal(r.files, 6);
  // Bytes go as $b64, ids as they are.
  assert.match(out.get('sets/1.json'), /"bytes":\{"\$b64":"CQ=="\}/);
});

test('stops on an error that could hurt the session, and writes no manifest', async () => {
  const tg = fakeTelegram({failOn: 'messages.getStickerSet'});
  const out = new Map();
  await assert.rejects(load(tg)({put: async (p, b) => out.set(p, b), pauseMs: [0, 0]}), /stopped on messages.getStickerSet/);
  assert.ok(!out.has('manifest.json'));
  assert.ok(!tg.calls.includes('upload.getFile'));
});
