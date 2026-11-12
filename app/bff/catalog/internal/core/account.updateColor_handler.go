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
)

// AccountUpdateColor
// account.updateColor#684d214e flags:# for_profile:flags.1?true color:flags.2?PeerColor = Bool;
//
// The color of the account's name (and of its replies and link previews)
// or, with for_profile, of its profile, each with a pattern of custom
// emoji. Open to everyone (ADR 0041).
//
// The clients say "none" by leaving the color out: tdesktop sends no color
// flag for kUnsetColorIndex (edit_peer_color_box.cpp:576-581), iOS sends
// peerColor without its color for a profile reset
// (UpdateAccountPeerName.swift:125-132), Android leaves req.color out when
// neither a profile color nor a pattern is chosen
// (PeerColorActivity.java:2008-2021). A pattern without a color is kept as
// sent: Android sends it for the profile (PeerColorActivity.java:2022-2030).
// A color must be one of help.getPeerColors or help.getPeerProfileColors
// (COLOR_INVALID) and the pattern a custom emoji (DOCUMENT_INVALID), as
// core.telegram.org/method/account.updateColor lists. A collectible color
// comes from a gift, and there are no gifts here: COLOR_INVALID.
//
// biz user tells every device of the account and the users who see it
// (updateUser with seq) in the transaction of the change.
func (c *CatalogCore) AccountUpdateColor(in *mtproto.TLAccountUpdateColor) (*mtproto.Bool, error) {
	var (
		color   = userpb.NoColor
		emojiId int64
	)
	switch in.GetConstructor() {
	case mtproto.TLConstructor_CRC32_account_updateColor_7cefa15d:
		if v := in.GetColor_FLAGINT32(); v != nil {
			color = v.GetValue()
		}
		emojiId = in.GetBackgroundEmojiId().GetValue()
	case mtproto.TLConstructor_CRC32_account_updateColor_a001cc43:
		color = in.GetColor_INT32()
		emojiId = in.GetBackgroundEmojiId().GetValue()
	default:
		switch p := in.GetColor_FLAGPEERCOLOR(); p.GetPredicateName() {
		case "":
		case mtproto.Predicate_peerColor:
			if v := p.GetColor(); v != nil {
				color = v.GetValue()
			}
			emojiId = p.GetBackgroundEmojiId_FLAGINT64().GetValue()
		default:
			err := errColorInvalid
			c.Logger.Errorf("account.updateColor - a collectible color: %v", err)
			return nil, err
		}
	}
	if color != userpb.NoColor {
		ok, err := c.colorListed(in.GetForProfile(), color)
		if err != nil {
			return nil, err
		}
		if !ok {
			err = errColorInvalid
			c.Logger.Errorf("account.updateColor - color %d: %v", color, err)
			return nil, err
		}
	}
	if err := c.checkCustomEmoji(emojiId); err != nil {
		return nil, err
	}
	if _, err := c.svcCtx.User.UserSetColor(c.ctx, &userpb.TLUserSetColor{
		UserId:            c.MD.UserId,
		ForProfile:        in.GetForProfile(),
		Color:             color,
		BackgroundEmojiId: emojiId,
	}); err != nil {
		c.Logger.Errorf("account.updateColor - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
