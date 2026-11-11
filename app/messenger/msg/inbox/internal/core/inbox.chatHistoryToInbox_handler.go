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
	"errors"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// InboxChatHistoryToInbox
// inbox.chatHistoryToInbox user_id:long from_id:long chat_id:long mention:Bool skip:Vector<long> = Void;
//
// The ghost user_id joined the basic group chat_id hidden (task 080, ADR
// 0048, p. 4): its box gets a copy of the group's whole history, a portion
// per transaction (dao.CopyChatHistory), the event kept until the last
// one. Telegram shows a new member of a basic group none of the history
// (Android ChatEditActivity.java:1825); the ghost sees all of it, as in a
// supergroup. A group the ghost was mentioned into is muted for it first:
// there only the mentions of it and the replies to it notify, which the
// push service sends through a mute (push/internal/core/core.go,
// n.Mention). The message that mentioned it follows this event from the
// same transaction and lands after the history, unread.
func (c *InboxCore) InboxChatHistoryToInbox(in *inbox.TLInboxChatHistoryToInbox) error {
	peer := &mtproto.PeerUtil{PeerType: mtproto.PEER_CHAT, PeerId: in.ChatId}
	if in.Mention {
		// The chat goes with the push of the setting, as the ghost sees it.
		chat, err := c.svcCtx.Dao.ChatClient.ChatGetMutableChat(c.ctx, &chatpb.TLChatGetMutableChat{ChatId: in.ChatId})
		if errors.Is(err, mtproto.ErrChatIdInvalid) {
			return c.apply(in.UserId, peer, func(*userbox.Box) (bool, error) { return true, nil })
		}
		if err != nil {
			return err
		}
		// Muting again on a repeated event changes nothing.
		if _, err = c.svcCtx.Dao.UserClient.UserSetNotifySettings(c.ctx, &userpb.TLUserSetNotifySettings{
			UserId:   in.UserId,
			PeerType: mtproto.PEER_CHAT,
			PeerId:   in.ChatId,
			Settings: mtproto.MakeTLPeerNotifySettings(&mtproto.PeerNotifySettings{
				MuteUntil: &wrapperspb.Int32Value{Value: muteForever},
			}).To_PeerNotifySettings(),
			Chats: []*mtproto.Chat{chat.ToUnsafeChat(in.UserId)},
		}); err != nil {
			return err
		}
	}
	return c.apply(in.UserId, peer, func(b *userbox.Box) (bool, error) {
		return c.svcCtx.Dao.CopyChatHistory(b, in.ChatId, in.Skip)
	})
}
