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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"google.golang.org/protobuf/proto"
)

// The direct messages of a channel (task 084, ADR 0055).

// ChannelUpdateDirectMessages
// channel.updateDirectMessages self_id:long perm_auth_key_id:long channel_id:long enabled:Bool = Updates;
//
// self_id switches the direct messages of the broadcast channel on or off
// (dao.SetDirectMessages) in its box transaction with the channel locked;
// the members get the service message and updateChannel by push. The answer
// holds them and both sides of the pair as self_id sees them: the clients
// link the channel to its monoforum from the chats of one answer (tdesktop
// data_session.cpp:1388-1399).
func (c *ChannelCore) ChannelUpdateDirectMessages(in *channel.TLChannelUpdateDirectMessages) (*mtproto.Updates, error) {
	var (
		change      *dao.ChannelChange
		monoforumId int64
	)
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		var err error
		if change, monoforumId, err = c.svcCtx.Dao.SetDirectMessages(b, in.ChannelId, mtproto.FromBool(in.Enabled)); err != nil {
			return err
		}
		return c.svcCtx.Dao.EmitChannelPush(b, in.PermAuthKeyId, change)
	}); err != nil {
		return nil, c.membershipError("channel.updateDirectMessages", err)
	}
	list, err := c.svcCtx.Dao.GetChannelListByIdList(c.ctx, in.SelfId, []int64{in.ChannelId, monoforumId})
	if err != nil {
		return nil, err
	}
	now := int32(time.Now().Unix())
	chats := make([]*mtproto.Chat, 0, len(list))
	for _, mc := range list {
		chats = append(chats, mc.ToChat(in.SelfId, now))
	}
	shown := proto.Clone(change.Message).(*mtproto.Message)
	shown.Out = true
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{
			mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: shown, Pts_INT32: change.Pts, PtsCount: 1}).To_Update(),
			mtproto.MakeTLUpdateChannel(&mtproto.Update{ChannelId: in.ChannelId}).To_Update(),
		},
		Users: []*mtproto.User{},
		Chats: chats,
		Date:  now,
	}).To_Updates(), nil
}

// ChannelGetSublists
// channel.getSublists self_id:long channel_id:long ids:Vector<long> offset_id:int limit:int = messages.SavedDialogs;
//
// The sublists of the monoforum for an admin of its direct messages
// (dao.Sublists): messages.savedDialogsSlice while more remain below the
// page, messages.savedDialogs for the last one and for the sublists of ids.
// Chats and users are the caller's to add.
func (c *ChannelCore) ChannelGetSublists(in *channel.TLChannelGetSublists) (*mtproto.Messages_SavedDialogs, error) {
	dialogs, messages, count, more, err := c.svcCtx.Dao.Sublists(c.ctx, in.SelfId, in.ChannelId, in.Ids, in.OffsetId, in.Limit, time.Now().Unix())
	if err != nil {
		return nil, c.membershipError("channel.getSublists", err)
	}
	r := &mtproto.Messages_SavedDialogs{
		Dialogs:  dialogs,
		Messages: messages,
		Chats:    []*mtproto.Chat{},
		Users:    []*mtproto.User{},
	}
	if more {
		r.Count = count
		return mtproto.MakeTLMessagesSavedDialogsSlice(r).To_Messages_SavedDialogs(), nil
	}
	return mtproto.MakeTLMessagesSavedDialogs(r).To_Messages_SavedDialogs(), nil
}

// ChannelGetSublistHistory
// channel.getSublistHistory self_id:long channel_id:long peer_id:long offset_id:int offset_date:int add_offset:int limit:int max_id:int min_id:int = messages.Messages;
//
// messages.channelMessages of one sublist, as channel.getHistory answers.
func (c *ChannelCore) ChannelGetSublistHistory(in *channel.TLChannelGetSublistHistory) (*mtproto.Messages_Messages, error) {
	messages, count, err := c.svcCtx.Dao.SublistHistory(c.ctx, in.SelfId, in.ChannelId, in.PeerId,
		in.OffsetId, in.OffsetDate, in.AddOffset, in.Limit, in.MaxId, in.MinId, time.Now().Unix())
	if err != nil {
		return nil, c.membershipError("channel.getSublistHistory", err)
	}
	return c.channelMessages(in.SelfId, in.ChannelId, messages, count)
}

// ChannelReadSublist
// channel.readSublist self_id:long perm_auth_key_id:long channel_id:long peer_id:long max_id:int = Bool;
func (c *ChannelCore) ChannelReadSublist(in *channel.TLChannelReadSublist) (*mtproto.Bool, error) {
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		return c.svcCtx.Dao.ReadSublist(b, in.ChannelId, in.PeerId, in.PermAuthKeyId, in.MaxId)
	}); err != nil {
		return nil, c.membershipError("channel.readSublist", err)
	}
	return mtproto.BoolTrue, nil
}

// ChannelDeleteSublistHistory
// channel.deleteSublistHistory self_id:long perm_auth_key_id:long channel_id:long peer_id:long max_id:int min_date:int max_date:int = messages.AffectedHistory;
//
// A portion of the sublist goes for everyone (dao.DeleteSublistHistory);
// offset is the number left, above 0 the client repeats the call, as for
// channel.deleteParticipantHistory.
func (c *ChannelCore) ChannelDeleteSublistHistory(in *channel.TLChannelDeleteSublistHistory) (*mtproto.Messages_AffectedHistory, error) {
	var del *dao.SublistDeleted
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		var err error
		if del, err = c.svcCtx.Dao.DeleteSublistHistory(b, in.ChannelId, in.PeerId, in.MaxId, in.MinDate, in.MaxDate); err != nil {
			return err
		}
		return c.svcCtx.Dao.EmitDeletePush(b, in.PermAuthKeyId, del.Deleted)
	}); err != nil {
		return nil, c.membershipError("channel.deleteSublistHistory", err)
	}
	pts := del.Pts
	if pts == 0 {
		var err error
		if pts, err = c.svcCtx.Dao.ChannelPts(c.ctx, in.ChannelId); err != nil {
			return nil, err
		}
	}
	return mtproto.MakeTLMessagesAffectedHistory(&mtproto.Messages_AffectedHistory{
		Pts:      pts,
		PtsCount: del.PtsCount,
		Offset:   del.Left,
	}).To_Messages_AffectedHistory(), nil
}

// ChannelGetMessageAuthor
// channel.getMessageAuthor self_id:long channel_id:long id:int = Int64;
func (c *ChannelCore) ChannelGetMessageAuthor(in *channel.TLChannelGetMessageAuthor) (*mtproto.Int64, error) {
	author, err := c.svcCtx.Dao.MessageAuthor(c.ctx, in.SelfId, in.ChannelId, in.Id, time.Now().Unix())
	if err != nil {
		return nil, c.membershipError("channel.getMessageAuthor", err)
	}
	return &mtproto.Int64{V: author}, nil
}

// ChannelMarkSublistUnread
// channel.markSublistUnread self_id:long perm_auth_key_id:long channel_id:long peer_id:long unread:Bool = Bool;
func (c *ChannelCore) ChannelMarkSublistUnread(in *channel.TLChannelMarkSublistUnread) (*mtproto.Bool, error) {
	changed := false
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		var err error
		changed, err = c.svcCtx.Dao.MarkSublistUnread(b, in.ChannelId, in.PeerId, in.PermAuthKeyId, mtproto.FromBool(in.Unread))
		return err
	}); err != nil {
		return nil, c.membershipError("channel.markSublistUnread", err)
	}
	return mtproto.ToBool(changed), nil
}

// ChannelGetSublistUnreadMarks
// channel.getSublistUnreadMarks self_id:long channel_id:long = Vector<long>;
func (c *ChannelCore) ChannelGetSublistUnreadMarks(in *channel.TLChannelGetSublistUnreadMarks) (*channel.Vector_Long, error) {
	peers, err := c.svcCtx.Dao.SublistUnreadMarks(c.ctx, in.SelfId, in.ChannelId, time.Now().Unix())
	if err != nil {
		return nil, c.membershipError("channel.getSublistUnreadMarks", err)
	}
	return &channel.Vector_Long{Datas: peers}, nil
}

// ChannelGetMessageSublists
// channel.getMessageSublists channel_id:long id:Vector<int> = Vector<long>;
func (c *ChannelCore) ChannelGetMessageSublists(in *channel.TLChannelGetMessageSublists) (*channel.Vector_Long, error) {
	peers, err := c.svcCtx.Dao.MessageSublists(c.ctx, in.ChannelId, in.Id)
	if err != nil {
		c.Logger.Errorf("channel.getMessageSublists - error: %v", err)
		return nil, err
	}
	return &channel.Vector_Long{Datas: peers}, nil
}
