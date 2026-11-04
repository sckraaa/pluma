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
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/pkg/reactions"
)

// MessagesSetChatAvailableReactions
// messages.setChatAvailableReactions#864b2581 flags:# peer:InputPeer available_reactions:ChatReactions reactions_limit:flags.0?int paid_enabled:flags.1?Bool = Updates;
//
// The reactions the members of a basic group may use; its admins with the
// right to change its info set them (biz chat checks it). The answer holds
// the group, whose full info the clients load again (task 053).
func (c *CatalogCore) MessagesSetChatAvailableReactions(in *mtproto.TLMessagesSetChatAvailableReactions) (*mtproto.Updates, error) {
	peer, err := c.reactionPeer(in.Peer)
	if err != nil {
		return nil, err
	}
	if peer.PeerType != mtproto.PEER_CHAT {
		return nil, mtproto.ErrPeerIdInvalid
	}
	t, list := in.GetAvailableReactions_CHATREACTIONS().ToChatReactions()
	if len(list) > reactions.InChatMax {
		return nil, mtproto.ErrReactionsTooMany
	}
	chat, err := c.svcCtx.Chat.ChatSetChatAvailableReactions(c.ctx, &chatpb.TLChatSetChatAvailableReactions{
		SelfId:                 c.MD.UserId,
		ChatId:                 peer.PeerId,
		AvailableReactionsType: t,
		AvailableReactions:     list,
	})
	if err != nil {
		c.Logger.Errorf("messages.setChatAvailableReactions - error: %v", err)
		return nil, err
	}
	return mtproto.MakeUpdatesByUpdatesUsersChats(
		[]*mtproto.User{},
		[]*mtproto.Chat{chat.ToUnsafeChat(c.MD.UserId)},
		mtproto.MakeTLUpdateChat(&mtproto.Update{ChatId_INT64: peer.PeerId}).To_Update()), nil
}
