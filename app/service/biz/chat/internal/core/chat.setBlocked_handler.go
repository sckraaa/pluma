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
	"github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// ChatSetBlocked
// chat.setBlocked chat_id:long blocked:Bool = Bool;
//
// The owner of the server blocks a basic group, or unblocks it (the panel,
// docs/ops/admin-panel.md). A blocked group is read as a deactivated one, as
// it is after a delete: the clients take it as out of the chat and show no
// input (Android ChatObject.isNotInChat, iOS, tdesktop). chat#d has no
// restriction fields, unlike channel#. Its members stay, so that an unblock
// brings the group back as it was. The same state asked for again changes
// nothing; a group deleted or moved to a supergroup cannot be blocked.
func (c *ChatCore) ChatSetBlocked(in *chat.TLChatSetBlocked) (*mtproto.Bool, error) {
	creator, err := c.creatorOf(in.ChatId)
	if err != nil {
		c.Logger.Errorf("chat.setBlocked - chat %d: %v", in.ChatId, err)
		return nil, err
	}
	want := mtproto.FromBool(in.Blocked)
	_, err = c.changeChat(creator, in.ChatId, func(b *userbox.Box, t *chatTx) error {
		d := t.c.svcCtx.Dao
		blocked, err := d.ChatBlocked(b, in.ChatId)
		switch {
		case err != nil:
			return err
		case blocked == want:
			return nil
		case !blocked && t.Chat.Deactivated:
			return mtproto.ErrChatIdInvalid
		}
		if err := d.SetChatBlocked(b, in.ChatId, want); err != nil {
			return err
		}
		to := t.members()
		if !slices.Contains(to, creator) {
			to = append(to, creator)
		}
		for _, id := range to {
			t.changed(b, id)
		}
		t.Chat.Deactivated = want
		t.Chat.Version++
		return t.tell(b, to, t.chatUpdate())
	})
	if err != nil {
		c.Logger.Errorf("chat.setBlocked - chat %d: %v", in.ChatId, err)
		return nil, err
	}
	c.Logger.Infof("chat.setBlocked - chat %d, blocked %v", in.ChatId, want)
	return mtproto.BoolTrue, nil
}
