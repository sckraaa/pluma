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
	mediapb "github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/catalog/stickerlists"
)

// sentSticker is the sticker a message of media sent, 0 for none: the
// document inputMediaDocument names, when it is a sticker with that access
// hash.
func sentSticker(in *mtproto.InputMedia, media *mtproto.MessageMedia) int64 {
	if in.GetPredicateName() != mtproto.Predicate_inputMediaDocument {
		return 0
	}
	id := in.GetId_INPUTDOCUMENT()
	doc := media.GetDocument()
	if stickerlists.StickerAttribute(doc) == nil || doc.GetId() != id.GetId() || doc.GetAccessHash() != id.GetAccessHash() {
		return 0
	}
	return doc.GetId()
}

// attachedStickers are the stickers the account attached to an uploaded
// photo or video (the stickers of inputMediaUploadedPhoto and
// inputMediaUploadedDocument).
func attachedStickers(in *mtproto.InputMedia) []*mtproto.InputDocument {
	switch in.GetPredicateName() {
	case mtproto.Predicate_inputMediaUploadedPhoto, mtproto.Predicate_inputMediaUploadedDocument:
		return in.GetStickers()
	}
	return nil
}

// addRecentStickers puts the stickers a message the account sent used at the
// top of its recent stickers: the sticker it sent, and those it attached to
// a photo or video at the top of the attached ones. Android and Telegram
// Desktop put a sticker they send at the top of their own list and never
// ask for it (MediaDataController.addRecentSticker sends
// messages.saveRecentSticker only to take one off; Telegram Desktop
// Stickers::incrementSticker), and both replace their list with the
// server's when its hash differs (processLoadedRecentDocuments,
// specialSetReceived): the list keeps the sticker only if the server adds
// it. The message is sent by then, so a failure here is logged, not
// answered: the sticker is only missing from the list.
func (c *MessagesCore) addRecentStickers(in *mtproto.InputMedia, media *mtproto.MessageMedia) {
	if c.MD.IsBot {
		return
	}
	if id := sentSticker(in, media); id != 0 {
		if err := c.svcCtx.StickerLists.Add(c.ctx, c.MD.UserId, stickerlists.Recent, id); err != nil {
			c.Logger.Errorf("recent stickers - sticker %d: %v", id, err)
		}
	}
	for _, id := range attachedStickers(in) {
		if id.GetPredicateName() != mtproto.Predicate_inputDocument {
			continue
		}
		doc, err := c.svcCtx.Dao.MediaClient.MediaGetDocument(c.ctx, &mediapb.TLMediaGetDocument{Id: id.GetId()})
		if err != nil {
			c.Logger.Errorf("recent stickers - attached sticker %d: %v", id.GetId(), err)
			continue
		}
		if stickerlists.StickerAttribute(doc) == nil || doc.GetAccessHash() != id.GetAccessHash() {
			continue
		}
		if err := c.svcCtx.StickerLists.Add(c.ctx, c.MD.UserId, stickerlists.RecentAttached, id.GetId()); err != nil {
			c.Logger.Errorf("recent stickers - attached sticker %d: %v", id.GetId(), err)
		}
	}
}

// markAttachedStickers records the stickers the account put on a photo or
// a video it uploads, and marks media that has them (attached.Store.Mark,
// task 084): clients then offer the sets of those stickers
// (messages.getAttachedStickers). The media is made by then, so a failure
// is logged, not answered: the media only goes without the mark.
func (c *MessagesCore) markAttachedStickers(in *mtproto.InputMedia, media *mtproto.MessageMedia) {
	if c.svcCtx.Attached == nil || media == nil {
		return
	}
	if err := c.svcCtx.Attached.Mark(c.ctx, c.svcCtx.Dao.MediaClient, in, media); err != nil {
		c.Logger.Errorf("attached stickers - error: %v", err)
	}
}
