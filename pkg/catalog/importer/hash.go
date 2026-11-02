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

package importer

import (
	"crypto/md5" //nolint:gosec // the reaction hash Telegram defines takes MD5 (core.telegram.org/api/reactions)
	"crypto/sha256"
	"encoding/binary"
	"strings"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/proto"
)

// The hashes of the catalog's answers, over the ids of this server.
// Where clients count the hash themselves and compare it with the server's
// (docs/tasks/024-research/semantics.md, "How hashes are computed"), it is
// their formula; elsewhere clients send back what the server gave, and a
// digest of the answer changes when the answer does.

// hashUpdate is one step of Telegram's hash over a list
// (core.telegram.org/api/offsets#hash-generation; Android
// MediaDataController.calcHash, tdesktop Api::HashUpdate).
func hashUpdate(h, v uint64) uint64 {
	h ^= h >> 21
	h ^= h << 35
	h ^= h >> 4
	return h + v
}

// featuredHash is the hash of trending sets as clients count it: each set's
// id in order, and 1 after each unread one (tdesktop CountFeaturedHash,
// iOS hashForIdsReverse, Android calcFeaturedStickersHash).
func featuredHash(f *mtproto.Messages_FeaturedStickers) int64 {
	unread := map[int64]bool{}
	for _, id := range f.GetUnread() {
		unread[id] = true
	}
	var h uint64
	for _, s := range f.GetSets() {
		id := s.GetSet().GetId()
		h = hashUpdate(h, uint64(id))
		if unread[id] {
			h = hashUpdate(h, 1)
		}
	}
	return int64(h)
}

// wallPapersHash is the hash of the gallery as Android counts it: the ids
// in order, those below 0 left out (WallpapersListActivity).
func wallPapersHash(w *mtproto.Account_WallPapers) int64 {
	var h uint64
	for _, p := range w.GetWallpapers() {
		if p.GetId() < 0 {
			continue
		}
		h = hashUpdate(h, uint64(p.GetId()))
	}
	return int64(h)
}

// reactionsHash is the hash of a list of reactions
// (core.telegram.org/api/reactions): for an emoji, 0 and the first four
// bytes of the MD5 of the emoji without U+FE0F as an int32; for a custom
// emoji, the halves of its document id.
func reactionsHash(r *mtproto.Messages_Reactions) int64 {
	var h uint64
	for _, x := range r.GetReactions() {
		switch x.GetPredicateName() {
		case mtproto.Predicate_reactionEmoji:
			sum := md5.Sum([]byte(strings.ReplaceAll(x.GetEmoticon(), "️", ""))) //nolint:gosec // see the import
			h = hashUpdate(h, 0)
			h = hashUpdate(h, uint64(int64(int32(binary.BigEndian.Uint32(sum[:4])))))
		case mtproto.Predicate_reactionCustomEmoji:
			id := uint64(x.GetDocumentId())
			h = hashUpdate(h, id>>32)
			h = hashUpdate(h, id&0xFFFFFFFF)
		}
	}
	return int64(h)
}

// digestHash is a hash of an answer's content: the SHA-256 of its encoding
// with its own hash field left at 0.
func digestHash(m proto.Message) int64 {
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if err != nil {
		panic(err) // a message that was just decoded always encodes
	}
	sum := sha256.Sum256(b)
	return int64(binary.LittleEndian.Uint64(sum[:8]) >> 1)
}

// int32Hash is a hash for a method whose hash is an int.
func int32Hash(h int64) int64 {
	return int64(int32(h))
}
