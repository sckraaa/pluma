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

package core

import (
	"github.com/teamgram/proto/mtproto"
)

// isGif tells whether clients show the document as a GIF: image/gif, or
// video/mp4 with documentAttributeAnimated (Android
// MessageObject.isGifDocument and isNewGifDocument; Android also limits the
// size of the video to show it, which is the client's choice).
func isGif(doc *mtproto.Document) bool {
	if doc.GetPredicateName() != mtproto.Predicate_document {
		return false
	}
	switch doc.GetMimeType() {
	case "image/gif":
		return true
	case "video/mp4":
		for _, a := range doc.GetAttributes() {
			if a.GetPredicateName() == mtproto.Predicate_documentAttributeAnimated {
				return true
			}
		}
	}
	return false
}

// documentsHash is the hash of the ids of docs in their order
// (core.telegram.org/api/offsets#hash-generation, with unsigned shifts), as
// Android computes it for messages.getSavedGifs
// (MediaDataController.calcDocumentsHash).
func documentsHash(docs []*mtproto.Document) int64 {
	var hash uint64
	for _, d := range docs {
		hash ^= hash >> 21
		hash ^= hash << 35
		hash ^= hash >> 4
		hash += uint64(d.GetId())
	}
	return int64(hash)
}

// inOrder returns the documents of docs in the order of ids, without the
// ids that have no document.
func inOrder(ids []int64, docs []*mtproto.Document) []*mtproto.Document {
	byId := make(map[int64]*mtproto.Document, len(docs))
	for _, d := range docs {
		if d.GetPredicateName() == mtproto.Predicate_document {
			byId[d.GetId()] = d
		}
	}
	out := make([]*mtproto.Document, 0, len(ids))
	for _, id := range ids {
		if d, ok := byId[id]; ok {
			out = append(out, d)
		}
	}
	return out
}
