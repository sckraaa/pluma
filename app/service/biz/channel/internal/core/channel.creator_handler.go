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
	"context"
	"sync"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/logx"
)

// ChannelGetFutureCreator
// channel.getFutureCreator self_id:long channel_id:long = Int64;
//
// Who owns the channel if self_id, its owner, leaves it now
// (dao.FutureCreator): the user channel.leaveChannel hands it to, 0 for
// nobody.
func (c *ChannelCore) ChannelGetFutureCreator(in *channel.TLChannelGetFutureCreator) (*mtproto.Int64, error) {
	id, err := c.svcCtx.Dao.FutureCreator(c.ctx, in.SelfId, in.ChannelId)
	if err != nil {
		return nil, c.membershipError("channel.getFutureCreator", err)
	}
	return &mtproto.Int64{V: id}, nil
}

// ChannelGetInactiveChannels
// channel.getInactiveChannels self_id:long limit:int = messages.InactiveChats;
//
// The channels and supergroups of self_id, the least active first, each
// with the date of its last activity there (dao.InactiveChannels), as
// self_id sees them; dates[i] is of chats[i], as the clients read them
// (iOS InactiveChannels.swift:40-46). The users are the caller's.
func (c *ChannelCore) ChannelGetInactiveChannels(in *channel.TLChannelGetInactiveChannels) (*mtproto.Messages_InactiveChats, error) {
	rows, err := c.svcCtx.Dao.InactiveChannels(c.ctx, in.SelfId, in.Limit)
	if err != nil {
		c.Logger.Errorf("channel.getInactiveChannels - error: %v", err)
		return nil, err
	}
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ChannelId)
	}
	list, err := c.svcCtx.Dao.GetChannelListByIdList(c.ctx, in.SelfId, ids)
	if err != nil {
		c.Logger.Errorf("channel.getInactiveChannels - error: %v", err)
		return nil, err
	}
	byId := make(map[int64]*channel.MutableChannel, len(list))
	for _, mc := range list {
		byId[mc.GetChannel().GetId()] = mc
	}
	now := int32(time.Now().Unix())
	out := &mtproto.Messages_InactiveChats{
		Dates: make([]int32, 0, len(rows)),
		Chats: make([]*mtproto.Chat, 0, len(rows)),
		Users: []*mtproto.User{},
	}
	for _, r := range rows {
		mc, ok := byId[r.ChannelId]
		if !ok {
			// Deleted since the list was read.
			continue
		}
		out.Dates = append(out.Dates, int32(r.Date))
		out.Chats = append(out.Chats, mc.ToChat(in.SelfId, now))
	}
	return mtproto.MakeTLMessagesInactiveChats(out).To_Messages_InactiveChats(), nil
}

// ChannelSetMainProfileTab
// channel.setMainProfileTab self_id:long perm_auth_key_id:long channel_id:long tab:ProfileTab = Bool;
//
// self_id makes tab the tab the channel's profile opens on (dao.SetMainTab);
// the members, the editor's other devices among them, get updateChannel by
// push and load the full channel again.
func (c *ChannelCore) ChannelSetMainProfileTab(in *channel.TLChannelSetMainProfileTab) (*mtproto.Bool, error) {
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		change, err := c.svcCtx.Dao.SetMainTab(b, in.ChannelId, mtproto.FromProfileTabToType(in.GetTab()))
		if err != nil || change == nil {
			return err
		}
		return c.svcCtx.Dao.EmitChannelPush(b, in.PermAuthKeyId, change)
	}); err != nil {
		return nil, c.membershipError("channel.setMainProfileTab", err)
	}
	return mtproto.BoolTrue, nil
}

// TransferWorker hands the channels whose owners left CreatorTransferDelay
// ago to their next owners (dao.TransferDueCreators), every
// TransferInterval, from Start until Stop. A restart loses nothing: the
// transfers are rows, and one an instance did the next does not find.
type TransferWorker struct {
	dao    *dao.Dao
	ctx    context.Context
	cancel context.CancelFunc
	start  sync.Once
	halt   sync.Once
	done   chan struct{}
}

// TransferInterval is how often the worker looks for transfers due: an
// owner's channel goes to its next owner at most this late.
const TransferInterval = time.Minute

// NewTransferWorker returns the worker of d.
func NewTransferWorker(d *dao.Dao) *TransferWorker {
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // kept in the worker, called by Stop
	return &TransferWorker{dao: d, ctx: ctx, cancel: cancel, done: make(chan struct{})}
}

// Start runs the worker until Stop.
func (w *TransferWorker) Start() {
	w.start.Do(func() {
		go w.run()
	})
}

// Stop stops the worker: the pass in progress is cut short, its
// transaction rolled back, and the next start does it again.
func (w *TransferWorker) Stop() {
	w.halt.Do(w.cancel)
	w.start.Do(func() {
		close(w.done) // never started
	})
	<-w.done
}

func (w *TransferWorker) run() {
	defer close(w.done)
	t := time.NewTicker(TransferInterval)
	defer t.Stop()
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-t.C:
			if _, err := w.dao.TransferDueCreators(w.ctx, time.Now().Unix()); err != nil && w.ctx.Err() == nil {
				logx.Errorf("channel: transfer the channels of owners who left: %v", err)
			}
		}
	}
}
