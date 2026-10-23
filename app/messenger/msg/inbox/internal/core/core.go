// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package core

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/internal/svc"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/logx"
)

// InboxCore applies inbox events. Each event changes the box of one user,
// its user_id, in one box transaction (or one per portion), and pushes what
// changed to that user's devices after commit.
type InboxCore struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func New(ctx context.Context, svcCtx *svc.ServiceContext) *InboxCore {
	return &InboxCore{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// run runs fn as a box transaction of userId whose dialog with peer may
// change, and publishes its events.
func (c *InboxCore) run(userId int64, peer *mtproto.PeerUtil, fn func(b *userbox.Box) error) error {
	var keys []string
	if peer != nil {
		keys = []string{dialog.GetDialogCacheKeyByPeer(userId, peer.PeerType, peer.PeerId)}
	}
	_, err := c.svcCtx.Dao.RunBox(c.ctx, userId, keys, fn)
	return err
}

// push adds the push of journaled updates to the box owner's devices.
func (c *InboxCore) push(b *userbox.Box, updates ...*mtproto.Update) error {
	return c.svcCtx.Dao.EmitPushUpdates(b, b.UserId(), false, mtproto.MakeUpdatesByUpdates(updates...))
}

// recipientPeer is the recipient's dialog of an event (dao.RecipientPeer).
func recipientPeer(fromId int64, peerType int32, peerId int64) *mtproto.PeerUtil {
	return dao.RecipientPeer(fromId, peerType, peerId)
}

// isBot tells whether userId is a bot by the users of an event.
func isBot(userId int64, users []*mtproto.User) bool {
	for _, u := range users {
		if u.GetId() == userId {
			return u.GetBot()
		}
	}
	return false
}
