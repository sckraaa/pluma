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
	"github.com/teamgram/teamgram-server/app/service/media/media"
)

// MessagesGetCustomEmojiDocuments
// messages.getCustomEmojiDocuments#d9ab0f54 document_id:Vector<long> = Vector<Document>;
//
// The custom emoji of the catalog by their ids, at most maxCustomEmoji of
// them; an id that is no document of the catalog is left out, so that no
// document of a user is fetched by a guessed id.
func (c *CatalogCore) MessagesGetCustomEmojiDocuments(in *mtproto.TLMessagesGetCustomEmojiDocuments) (*mtproto.Vector_Document, error) {
	ids := in.GetDocumentId()
	if len(ids) > maxCustomEmoji {
		return nil, mtproto.ErrLimitInvalid
	}
	known, err := c.svcCtx.Catalog.CatalogDocuments(c.ctx, ids)
	if err != nil {
		c.Logger.Errorf("messages.getCustomEmojiDocuments - the catalog: %v", err)
		return nil, err
	}
	if len(known) == 0 {
		return &mtproto.Vector_Document{Datas: []*mtproto.Document{}}, nil
	}
	docs, err := c.svcCtx.Media.MediaGetDocumentList(c.ctx, &media.TLMediaGetDocumentList{IdList: known})
	if err != nil {
		c.Logger.Errorf("messages.getCustomEmojiDocuments - media: %v", err)
		return nil, err
	}
	return &mtproto.Vector_Document{Datas: docs.GetDatas()}, nil
}

// maxCustomEmoji is the most custom emoji one request may ask for; tweb
// asks for 100 at once (appEmojiManager.ts, docs/tasks/024-research).
const maxCustomEmoji = 200
