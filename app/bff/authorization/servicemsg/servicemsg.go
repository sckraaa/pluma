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

// Package servicemsg sends the messages of the service notifications
// account (777000) to a user (ADR 0026): real messages of the user's
// history with that account, delivered with pts like any incoming message,
// so that every session, a new device's too, finds them in the chat. The
// account keeps no copy.
package servicemsg

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
)

// UserId is the service notifications account.
const UserId = 777000

// Msg is what servicemsg needs of the msg service.
type Msg interface {
	MsgPushUserMessage(ctx context.Context, in *msgpb.TLMsgPushUserMessage) (*mtproto.Bool, error)
}

// Send sends userId the message text with its entities, dated date.
func Send(ctx context.Context, m Msg, userId int64, date time.Time, text string, entities []*mtproto.MessageEntity) error {
	message := mtproto.MakeTLMessage(&mtproto.Message{
		Out:      true,
		Date:     int32(date.Unix()),
		FromId:   mtproto.MakePeerUser(UserId),
		PeerId:   mtproto.MakeTLPeerUser(&mtproto.Peer{UserId: userId}).To_Peer(),
		Message:  text,
		Entities: entities,
	}).To_Message()
	_, err := m.MsgPushUserMessage(ctx, &msgpb.TLMsgPushUserMessage{
		UserId:    UserId,
		AuthKeyId: 0,
		PeerType:  mtproto.PEER_USER,
		PeerId:    userId,
		// Not a regular send: the message goes to the user's box only.
		PushType: 1,
		Message: msgpb.MakeTLOutboxMessage(&msgpb.OutboxMessage{
			RandomId: rand.Int64(), //nolint:gosec // G404: a random_id is unique, not secret
			Message:  message,
		}).To_OutboxMessage(),
	})
	return err
}
