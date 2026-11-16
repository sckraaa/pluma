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
	"encoding/base64"
	"errors"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/botstore"
)

// What a bot prepares for the mini app of one user (task 106, part F): an
// inline message the app shares (messages.savePreparedInlineMessage,
// messages.getPreparedInlineMessage) and a button that asks the user for a
// chat (bots.requestWebViewButton, bots.getRequestedWebViewButton,
// messages.sendBotRequestedPeer). Both live in bot_prepared
// (pkg/botstore) for botstore.PreparedTTL.

// The kinds of chats of InlineQueryPeerType, by the names inlinePeerType
// gives them.
var peerTypeKinds = map[string]string{
	mtproto.Predicate_inlineQueryPeerTypeSameBotPM: "same_bot",
	mtproto.Predicate_inlineQueryPeerTypePM:        "pm",
	mtproto.Predicate_inlineQueryPeerTypeBotPM:     "bot",
	mtproto.Predicate_inlineQueryPeerTypeChat:      "chat",
	mtproto.Predicate_inlineQueryPeerTypeMegagroup: "megagroup",
	mtproto.Predicate_inlineQueryPeerTypeBroadcast: "broadcast",
}

// defaultPeerKinds are the chats of a prepared message whose bot named none
// (the brief: every kind). The private chat with the bot itself is among the
// bots' (bot): tdesktop reads inlineQueryPeerTypeSameBotPM as no kind at all
// (history_item_reply_markup.cpp:279-297), so a list must say "bot" to let
// the user pick a bot, and with no kind left the chooser would offer nothing.
var defaultPeerKinds = []string{"pm", "bot", "chat", "megagroup", "broadcast"}

// preparedPeerKinds checks the peer_types of messages.savePreparedInlineMessage
// and returns them as kinds, in their order, without repeats; none is
// every kind (defaultPeerKinds). An unknown type is PEER_TYPES_INVALID.
func preparedPeerKinds(in []*mtproto.InlineQueryPeerType) ([]string, error) {
	if len(in) == 0 {
		return append([]string(nil), defaultPeerKinds...), nil
	}
	kinds := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, t := range in {
		kind, ok := peerTypeKinds[t.GetPredicateName()]
		if !ok {
			return nil, mtproto.ErrPeerTypesInvalid
		}
		if !seen[kind] {
			seen[kind] = true
			kinds = append(kinds, kind)
		}
	}
	return kinds, nil
}

// peerTypesTL is kinds as the peer_types of messages.preparedInlineMessage.
func peerTypesTL(kinds []string) []*mtproto.InlineQueryPeerType {
	out := make([]*mtproto.InlineQueryPeerType, 0, len(kinds))
	for _, kind := range kinds {
		switch kind {
		case "same_bot":
			out = append(out, mtproto.MakeTLInlineQueryPeerTypeSameBotPM(nil).To_InlineQueryPeerType())
		case "pm":
			out = append(out, mtproto.MakeTLInlineQueryPeerTypePM(nil).To_InlineQueryPeerType())
		case "bot":
			out = append(out, mtproto.MakeTLInlineQueryPeerTypeBotPM(nil).To_InlineQueryPeerType())
		case "chat":
			out = append(out, mtproto.MakeTLInlineQueryPeerTypeChat(nil).To_InlineQueryPeerType())
		case "megagroup":
			out = append(out, mtproto.MakeTLInlineQueryPeerTypeMegagroup(nil).To_InlineQueryPeerType())
		case "broadcast":
			out = append(out, mtproto.MakeTLInlineQueryPeerTypeBroadcast(nil).To_InlineQueryPeerType())
		}
	}
	return out
}

// peerKindAllowed tells whether a chat of the kind is among kinds. The chat
// with the bot of the message is one of the bots' chats as well.
func peerKindAllowed(kinds []string, kind string) bool {
	for _, k := range kinds {
		if k == kind || kind == "same_bot" && k == "bot" {
			return true
		}
	}
	return false
}

// preparedMessage is the payload of a prepared inline message: the result as
// clients get it, the game of a game result (a botInlineMediaResult does not
// name its game, dao.InlineAnswer.Games) and the kinds of chats it may go to.
type preparedMessage struct {
	Result    *mtproto.BotInlineResult `json:"result"`
	Game      *dao.GameRef             `json:"game,omitempty"`
	PeerTypes []string                 `json:"peer_types"`
}

// preparedFor is the user a bot prepares something for: a user, not a bot,
// the bot itself or a deleted account (USER_ID_INVALID, as
// core.telegram.org/method/messages.savePreparedInlineMessage and
// /method/bots.requestWebViewButton list it): only a user opens mini apps.
func (c *BotsCore) preparedFor(in *mtproto.InputUser) (int64, error) {
	target := mtproto.FromInputUser(c.MD.UserId, in)
	if !target.IsUser() || target.PeerId == c.MD.UserId {
		return 0, mtproto.ErrUserIdInvalid
	}
	u, err := c.svcCtx.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: target.PeerId})
	if errors.Is(err, mtproto.ErrUserIdInvalid) {
		return 0, mtproto.ErrUserIdInvalid
	}
	if err != nil {
		return 0, err
	}
	if u.IsBot() || u.Deleted() {
		return 0, mtproto.ErrUserIdInvalid
	}
	return u.Id(), nil
}

// putPrepared stores a prepared item of the calling bot for the user.
func (c *BotsCore) putPrepared(method string, kind int8, userId int64, payload []byte) (botstore.Prepared, error) {
	p, err := c.svcCtx.Bots.PutPrepared(c.ctx, kind, c.MD.UserId, userId, payload, int32(time.Now().Unix()))
	if errors.Is(err, botstore.ErrPreparedInvalid) {
		// Only a payload over botstore.MaxPreparedPayload: a result whose
		// text, entities and markup are past every limit of a message.
		return botstore.Prepared{}, mtproto.ErrMessageTooLong
	}
	if err != nil {
		c.Logger.Errorf("%s - bot %d: %v", method, c.MD.UserId, err)
		return botstore.Prepared{}, mtproto.ErrInternalServerError
	}
	return p, nil
}

// wellFormedPreparedId tells whether id can be an id of bot_prepared: the
// base64url of botstore's 18 random bytes.
func wellFormedPreparedId(id string) bool {
	b, err := base64.RawURLEncoding.DecodeString(id)
	return err == nil && len(b) == 18
}

// preparedOf returns the live item of the kind the bot prepared for the
// calling user, nil for none: an unknown or expired id, or one of another
// bot or user.
func (c *BotsCore) preparedOf(method string, kind int8, botId int64, id string) (*botstore.Prepared, error) {
	p, err := c.svcCtx.Bots.PreparedOf(c.ctx, id, kind, int32(time.Now().Unix()))
	if err != nil {
		c.Logger.Errorf("%s - prepared item of bot %d: %v", method, botId, err)
		return nil, mtproto.ErrInternalServerError
	}
	if p == nil || p.BotId != botId || p.UserId != c.MD.UserId {
		return nil, nil
	}
	return p, nil
}
