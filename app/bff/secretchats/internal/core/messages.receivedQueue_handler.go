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
)

// MessagesReceivedQueue
// messages.receivedQueue#55a5bb66 max_qts:int = Vector<long>;
//
// It answers the random ids of the confirmed messages
// (core.telegram.org/method/messages.receivedQueue).
func (c *SecretChatsCore) MessagesReceivedQueue(in *mtproto.TLMessagesReceivedQueue) (*mtproto.Vector_Long, error) {
	if err := c.checkUser(); err != nil {
		return nil, err
	}
	if in.GetMaxQts() < 0 {
		return nil, mtproto.ErrMaxQtsInvalid
	}
	ids, err := c.svcCtx.Dao.Store.Ack(c.ctx, c.MD.PermAuthKeyId, in.GetMaxQts())
	if err != nil {
		c.Logger.Errorf("messages.receivedQueue - user %d: %v", c.MD.UserId, err)
		return nil, mtproto.ErrInternalServerError
	}
	if ids == nil {
		ids = []int64{}
	}
	return &mtproto.Vector_Long{Datas: ids}, nil
}
