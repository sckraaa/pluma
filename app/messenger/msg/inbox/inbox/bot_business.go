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

package inbox

import (
	"fmt"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
)

// BotBusinessUpdate tells whether u is an update a connected bot of
// Telegram Business is told with qts (inbox.botBusinessUpdateToInbox):
// "updateBotBusinessConnect", "updateBotNewBusinessMessage",
// "updateBotEditBusinessMessage" and "updateBotDeleteBusinessMessage" carry
// a qts (api.tl:450-458); updateBusinessBotCallbackQuery does not and goes
// the way of the other presses.
func BotBusinessUpdate(predicate string) bool {
	switch predicate {
	case mtproto.Predicate_updateBotBusinessConnect,
		mtproto.Predicate_updateBotNewBusinessMessage,
		mtproto.Predicate_updateBotEditBusinessMessage,
		mtproto.Predicate_updateBotDeleteBusinessMessage:
		return true
	}
	return false
}

// EmitBotBusinessUpdate adds to b's transaction, a box transaction of the
// account whose connection u is of, the inbox event that tells u to the
// connected bot botId (inbox.botBusinessUpdateToInbox): the inbox consumer
// of msg queues it with the next qts of each authorization of the bot, in
// the bot's box transaction, and pushes it. The events of one account reach
// its bot in the order of its transactions.
//
// The event is checked here, not when it is applied: an update that does
// not go with qts would make it dead and park every later event of the
// bot. EmitBotBusinessUpdate refuses (userbox.ErrInvalidEvent, the caller's
// transaction rolls back) a bot that is nobody or b's own account, an
// update of another kind and a topic that is not of inbox events.
func EmitBotBusinessUpdate(b *userbox.Box, inboxTopic string, botId int64, u *mtproto.Update) error {
	if err := checkBotBusinessUpdate(b.UserId(), inboxTopic, botId, u); err != nil {
		return err
	}
	in := &TLInboxBotBusinessUpdateToInbox{
		UserId: botId,
		FromId: b.UserId(),
		Update: u,
	}
	data, err := jsonx.Marshal(in)
	if err != nil {
		return err
	}
	b.Emit(userbox.Event{
		Topic:     inboxTopic,
		Recipient: botId,
		Method:    string(proto.MessageName(in)),
		Data:      data,
	})
	return nil
}

// checkBotBusinessUpdate tells whether initiator may write the event of u
// to botId on topic (EmitBotBusinessUpdate).
func checkBotBusinessUpdate(initiator int64, topic string, botId int64, u *mtproto.Update) error {
	switch {
	case botId <= 0:
		return fmt.Errorf("%w: a business update to user %d", userbox.ErrInvalidEvent, botId)
	case botId == initiator:
		return fmt.Errorf("%w: a business update of user %d to itself", userbox.ErrInvalidEvent, botId)
	case u == nil || !BotBusinessUpdate(u.GetPredicateName()):
		return fmt.Errorf("%w: %q to bot %d is not a business update with qts", userbox.ErrInvalidEvent, u.GetPredicateName(), botId)
	}
	if class, ok := userbox.TopicClassOf(topic); !ok || class != userbox.TopicInbox {
		return fmt.Errorf("%w: topic %q is not of inbox events", userbox.ErrInvalidEvent, topic)
	}
	return nil
}

// PickBotBusinessUpdate adds to h the peers u, a business update with qts,
// names: the messages and the one replied to, the chat of a deletion, the
// account of a connection.
func PickBotBusinessUpdate(h *mtproto.IDListHelper, u *mtproto.Update) {
	switch u.GetPredicateName() {
	case mtproto.Predicate_updateBotNewBusinessMessage, mtproto.Predicate_updateBotEditBusinessMessage:
		h.PickByMessage(u.GetMessage_MESSAGE())
		h.PickByMessage(u.GetReplyToMessage())
	case mtproto.Predicate_updateBotDeleteBusinessMessage:
		h.PickByPeer(u.GetPeer_PEER())
	case mtproto.Predicate_updateBotBusinessConnect:
		h.AppendUsers(u.GetConnection().GetUserId())
	}
}
