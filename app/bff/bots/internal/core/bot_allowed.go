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
	"encoding/binary"
	"errors"
	"hash/fnv"
	"time"

	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
)

// botAllowedRandomId is the random_id of the messageActionBotAllowed of the
// user and the bot: one message per pair, whatever let the bot write first
// and however often that is repeated.
func botAllowedRandomId(userId, botId int64) int64 {
	h := fnv.New64a()
	var b [len("bot_allowed") + 16]byte
	n := copy(b[:], "bot_allowed")
	binary.BigEndian.PutUint64(b[n:], uint64(userId))
	binary.BigEndian.PutUint64(b[n+8:], uint64(botId))
	_, _ = h.Write(b[:])
	return int64(h.Sum64() >> 1)
}

// allowWrite records that c's user lets the bot write to them
// (bot_write_allowed, ADR 0068 p. 4) and, the first time, posts action, a
// messageActionBotAllowed that says why, from the user to the bot: the user
// sees it in the chat with the bot (tdesktop lng_action_bot_allowed_from_domain
// and its kin), the bot gets it as Bot API's write_access_allowed. source is
// one of botstore.WriteAllowed*.
//
// The message goes first, with a random_id of its own for the pair
// (botAllowedRandomId), and the permission after it: a request that fails
// between the two is repeated whole, and msg answers the repeated message
// with the one sent (dao.SentBefore). A message the user deleted since is
// RANDOM_ID_DUPLICATE, which means it was sent. It is pushed to every
// session of the user, the one of the request too: the methods that post it
// answer with something other than its Updates. The updates of the message
// are returned, nil when the bot was let write before.
func (c *BotsCore) allowWrite(botId int64, source int8, action *mtproto.MessageAction) (*mtproto.Updates, error) {
	allowed, err := c.svcCtx.Bots.WriteAllowed(c.ctx, c.MD.UserId, botId)
	if err != nil || allowed {
		return nil, err
	}
	updates, err := c.svcCtx.MsgSendMessageV2(c.ctx, &msgpb.TLMsgSendMessageV2{
		UserId:    c.MD.UserId,
		AuthKeyId: 0,
		PeerType:  mtproto.PEER_USER,
		PeerId:    botId,
		Message: []*msgpb.OutboxMessage{msgpb.MakeTLOutboxMessage(&msgpb.OutboxMessage{
			NoWebpage: true,
			RandomId:  botAllowedRandomId(c.MD.UserId, botId),
			Message: mtproto.MakeTLMessageService(&mtproto.Message{
				Out:    true,
				FromId: mtproto.MakePeerUser(c.MD.UserId),
				PeerId: mtproto.MakePeerUser(botId),
				Date:   int32(time.Now().Unix()),
				Action: action,
			}).To_Message(),
		}).To_OutboxMessage()},
	})
	if errors.Is(err, mtproto.ErrRandomIdDuplicate) {
		updates, err = nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err = c.svcCtx.Bots.AllowWrite(c.ctx, c.MD.UserId, botId, source, int32(time.Now().Unix())); err != nil {
		return nil, err
	}
	return updates, nil
}
