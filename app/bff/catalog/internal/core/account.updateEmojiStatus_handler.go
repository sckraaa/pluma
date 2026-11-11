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
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"google.golang.org/grpc/status"
)

// errCollectibleInvalid: "The specified collectible is invalid"
// (core.telegram.org/method/account.updateEmojiStatus).
var errCollectibleInvalid = status.Error(mtproto.ErrBadRequest, "COLLECTIBLE_INVALID")

// AccountUpdateEmojiStatus
// account.updateEmojiStatus#fbd3de6b emoji_status:EmojiStatus = Bool;
//
// The emoji status next to the account's name: a custom emoji of the
// catalog, until a date or without an end, or none (emojiStatusEmpty).
// Open to everyone (ADR 0041). A document that is no custom emoji is
// DOCUMENT_INVALID; a collectible status comes from a gift, and there are
// no gifts here: COLLECTIBLE_INVALID
// (core.telegram.org/method/account.updateEmojiStatus).
//
// biz user tells every device of the account and the users who see it
// (updateUserEmojiStatus with seq) and adds the status to the recent ones
// in the transaction of the change.
func (c *CatalogCore) AccountUpdateEmojiStatus(in *mtproto.TLAccountUpdateEmojiStatus) (*mtproto.Bool, error) {
	var (
		documentId int64
		until      int32
	)
	switch s := in.GetEmojiStatus(); s.GetPredicateName() {
	case mtproto.Predicate_emojiStatusEmpty:
	case mtproto.Predicate_emojiStatus, mtproto.Predicate_emojiStatusUntil:
		documentId = s.GetDocumentId()
		until = s.GetUntil_FLAGINT32().GetValue()
		if until == 0 {
			until = s.GetUntil_INT32()
		}
		if documentId == 0 || until < 0 {
			err := mtproto.ErrDocumentInvalid
			c.Logger.Errorf("account.updateEmojiStatus - error: %v", err)
			return nil, err
		}
		if err := c.checkCustomEmoji(documentId); err != nil {
			return nil, err
		}
	case mtproto.Predicate_inputEmojiStatusCollectible, mtproto.Predicate_emojiStatusCollectible:
		err := errCollectibleInvalid
		c.Logger.Errorf("account.updateEmojiStatus - %s: %v", s.GetPredicateName(), err)
		return nil, err
	default:
		err := mtproto.ErrDocumentInvalid
		c.Logger.Errorf("account.updateEmojiStatus - %s: %v", s.GetPredicateName(), err)
		return nil, err
	}
	if _, err := c.svcCtx.User.UserUpdateEmojiStatus(c.ctx, &userpb.TLUserUpdateEmojiStatus{
		UserId:                c.MD.UserId,
		EmojiStatusDocumentId: documentId,
		EmojiStatusUntil:      until,
	}); err != nil {
		c.Logger.Errorf("account.updateEmojiStatus - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
