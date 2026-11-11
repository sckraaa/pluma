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
	"slices"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

// ChannelsUpdateEmojiStatus
// channels.updateEmojiStatus#f0d3e6a8 channel:InputChannel emoji_status:EmojiStatus = Updates;
//
// The emoji status of a channel: emojiStatus, a custom emoji of the catalog
// until a date or for good, or emojiStatusEmpty for none. An emoji that is
// no custom emoji of the catalog, or one of
// account.getChannelRestrictedStatusEmojis, is DOCUMENT_INVALID; this
// server has no collectibles, so a collectible status is too. biz channel
// checks the rights and keeps the change; the answer holds updateChannel
// and the channel (Android ChannelColorActivity.java:804-808).
func (c *CatalogCore) ChannelsUpdateEmojiStatus(in *mtproto.TLChannelsUpdateEmojiStatus) (*mtproto.Updates, error) {
	channelId, err := c.channelOf(in.GetChannel())
	if err != nil {
		return nil, err
	}
	var (
		documentId int64
		until      int32
	)
	switch s := in.GetEmojiStatus(); s.GetPredicateName() {
	case mtproto.Predicate_emojiStatusEmpty:
	case mtproto.Predicate_emojiStatus:
		documentId, until = s.GetDocumentId(), s.GetUntil_FLAGINT32().GetValue()
		if err = c.checkCustomEmoji(documentId); err != nil {
			return nil, err
		}
		restricted := &mtproto.EmojiList{}
		if _, _, err = c.stored("account.getChannelRestrictedStatusEmojis", "", restricted); err != nil {
			return nil, err
		}
		if slices.Contains(restricted.GetDocumentId(), documentId) {
			return nil, mtproto.ErrDocumentInvalid
		}
	default:
		return nil, mtproto.ErrDocumentInvalid
	}
	r, err := c.svcCtx.Channel.ChannelUpdateEmojiStatus(c.ctx, &channelpb.TLChannelUpdateEmojiStatus{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		ChannelId:     channelId,
		DocumentId:    documentId,
		Until:         until,
	})
	if err != nil {
		c.Logger.Errorf("channels.updateEmojiStatus - error: %v", err)
		return nil, err
	}
	return r, nil
}
