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

package sync_client

import (
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
)

// EmitSeqUpdate journals u with the next seq of b's account and pushes it,
// as the transaction's event on topic (the service's sync topic), to every
// device of the account, the one that made the change included: a method
// that answers Bool tells its device nothing, and the device would see a
// gap at the next update with seq (ADR 0020). The push is u alone in an
// updates container with that seq and the date of the transaction; users
// and chats are the ones u refers to, as the account sees them.
//
// u must be an update with seq (userbox.Box.AddSeqUpdate); another one
// rolls the transaction back.
func EmitSeqUpdate(b *userbox.Box, topic string, u *mtproto.Update, users []*mtproto.User, chats []*mtproto.Chat) error {
	seq := b.AddSeqUpdate(u)
	if seq == 0 {
		return nil // the misuse rolls the transaction back
	}
	in := &sync.TLSyncPushUpdates{
		UserId: b.UserId(),
		Updates: mtproto.MakeTLUpdates(&mtproto.Updates{
			Updates: []*mtproto.Update{u},
			Users:   users,
			Chats:   chats,
			Date:    int32(b.Date()),
			Seq:     seq,
		}).To_Updates(),
	}
	data, err := jsonx.Marshal(in)
	if err != nil {
		return err
	}
	b.Emit(userbox.Event{
		Topic:     topic,
		Recipient: b.UserId(),
		Method:    string(proto.MessageName(in)),
		Data:      data,
	})
	return nil
}
