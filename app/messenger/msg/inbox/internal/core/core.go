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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/internal/svc"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/logx"
)

// InboxCore applies one inbox event, eventId, its mq_outbox row. The event
// changes the box of one user, its user_id, in one box transaction (or one
// per portion), which takes the event first (userbox.Box.TakeEvent) and
// deletes its row with the change: exactly once, and only when it is next
// in its recipient's lane (ADR 0012, p. 5). What the transaction writes to
// that user's devices is pushed by the relay once committed.
type InboxCore struct {
	ctx     context.Context
	svcCtx  *svc.ServiceContext
	eventId int64
	// createdAt is when the initiator's transaction wrote the event.
	createdAt time.Time
	outcome   userbox.TakeOutcome
	logx.Logger
}

// New is the core of the event eventId, written at createdAt (unix ms, the
// created_at of its row).
func New(ctx context.Context, svcCtx *svc.ServiceContext, eventId, createdAt int64) *InboxCore {
	return &InboxCore{
		ctx:       ctx,
		svcCtx:    svcCtx,
		eventId:   eventId,
		createdAt: time.UnixMilli(createdAt),
		Logger:    logx.WithContext(ctx),
	}
}

// Outcome is what the last transaction decided about the event.
func (c *InboxCore) Outcome() userbox.TakeOutcome { return c.outcome }

// apply applies the event to the box of userId, whose dialog with peer may
// change: fn is the body of the handler, run when the event is next in its
// lane. done tells that the event is applied: its row goes with the
// transaction. A handler of portions returns false while portions are
// left; the next transaction takes the event again, which checks its lane
// again, and goes on with the portions left. Every branch that finds
// nothing to do returns done: an event never stays without a reason.
func (c *InboxCore) apply(userId int64, peer *mtproto.PeerUtil, fn func(b *userbox.Box) (done bool, err error)) error {
	var keys []string
	if peer != nil {
		keys = []string{dialog.GetDialogCacheKeyByPeer(userId, peer.PeerType, peer.PeerId)}
	}
	for {
		done := true
		_, err := c.svcCtx.Dao.RunBox(c.ctx, userId, keys, func(b *userbox.Box) error {
			done = true
			outcome, err := b.TakeEvent(c.eventId)
			if err != nil {
				return err
			}
			c.outcome = outcome
			if outcome != userbox.TakeApply {
				return nil
			}
			if done, err = fn(b); err != nil {
				return err
			}
			if done {
				return b.FinishEvent()
			}
			b.KeepEvent()
			return nil
		})
		if err != nil || done {
			return err
		}
	}
}

// Fail makes the event dead in a box transaction of its recipient: cause,
// a deterministic error, came back after attempts tries. Its recipient's
// later events park behind it until the relay replays the lane.
func Fail(ctx context.Context, svcCtx *svc.ServiceContext, eventId, userId int64, cause error, attempts int) error {
	_, err := svcCtx.Dao.RunBox(ctx, userId, nil, func(b *userbox.Box) error {
		outcome, err := b.TakeEvent(eventId)
		if err != nil || outcome != userbox.TakeApply {
			return err
		}
		return b.FailEvent(cause, attempts)
	})
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
