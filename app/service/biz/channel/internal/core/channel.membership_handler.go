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
	"errors"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"google.golang.org/protobuf/proto"
)

// ChannelJoinChannel
// channel.joinChannel self_id:long perm_auth_key_id:long channel_id:long link:string = Updates;
//
// self_id joins a public channel by itself (link empty) or any channel by
// its invite link (channel_id 0, or the link's channel). The participant
// row, the counters, the dialog (read up to the last message) and, in a
// supergroup, the service message of the join are one box transaction of
// the user; the members get the message by push (ADR 0034, p. 5). The
// user's other devices get updateChannel with seq (p. 3). The answer holds
// updateChannel, the service message and the channel: Android reads the
// channel from the answer, iOS then asks channels.getParticipant(self)
// (JoinChannel.swift:70-81).
func (c *ChannelCore) ChannelJoinChannel(in *channel.TLChannelJoinChannel) (*mtproto.Updates, error) {
	channelId := in.ChannelId
	if in.Link != "" {
		id, err := c.svcCtx.Dao.InviteChannel(c.ctx, in.Link)
		if err != nil {
			return nil, err
		}
		if channelId != 0 && channelId != id {
			return nil, mtproto.ErrInviteHashInvalid
		}
		channelId = id
	}
	if channelId == 0 {
		return nil, mtproto.ErrChannelInvalid
	}
	var change *dao.ChannelChange
	_, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		var err error
		if change, err = c.svcCtx.Dao.Join(b, channelId, in.Link); err != nil {
			return err
		}
		return c.svcCtx.Dao.EmitChannelPush(b, in.PermAuthKeyId, change)
	})
	if err != nil {
		return nil, c.membershipError("channel.joinChannel", err)
	}
	return c.membershipAnswer(in.SelfId, change)
}

// ChannelLeaveChannel
// channel.leaveChannel self_id:long perm_auth_key_id:long channel_id:long = Updates;
//
// self_id leaves the channel, as joining (ChannelJoinChannel) in reverse;
// the dialog goes. The answer holds updateChannel and the channel as it is
// now to self_id (left, or channelForbidden for a private one): Android
// drops the dialog on it (MessagesController.java:20153-20154).
func (c *ChannelCore) ChannelLeaveChannel(in *channel.TLChannelLeaveChannel) (*mtproto.Updates, error) {
	var change *dao.ChannelChange
	_, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		var err error
		if change, err = c.svcCtx.Dao.Leave(b, in.ChannelId); err != nil {
			return err
		}
		return c.svcCtx.Dao.EmitChannelPush(b, in.PermAuthKeyId, change)
	})
	if err != nil {
		return nil, c.membershipError("channel.leaveChannel", err)
	}
	return c.membershipAnswer(in.SelfId, change)
}

// membershipError logs an error other than an answer of the method.
func (c *ChannelCore) membershipError(method string, err error) error {
	var rpcErr *mtproto.TLRpcError
	if !errors.As(err, &rpcErr) {
		c.Logger.Errorf("%s - error: %v", method, err)
	}
	return err
}

// membershipAnswer reads the channel as selfId sees it once committed,
// gives selfId's other devices updateChannel with seq and returns the
// answer of a join or a leave.
func (c *ChannelCore) membershipAnswer(selfId int64, change *dao.ChannelChange) (*mtproto.Updates, error) {
	mc, err := c.svcCtx.Dao.GetMutableChannel(c.ctx, change.ChannelId, []int64{selfId})
	if err != nil {
		c.Logger.Errorf("channel %d after a change of user %d: %v", change.ChannelId, selfId, err)
		return nil, err
	}
	now := int32(time.Now().Unix())
	view := mc.ToChat(selfId, now)
	updateChannel := mtproto.MakeTLUpdateChannel(&mtproto.Update{ChannelId: change.ChannelId}).To_Update()
	if _, err = c.svcCtx.Dao.RunBox(c.ctx, selfId, func(b *userbox.Box) error {
		return c.svcCtx.Dao.EmitSeqUpdate(b, updateChannel, nil, []*mtproto.Chat{view})
	}); err != nil {
		// The change is committed; the other devices learn it from the
		// dialogs and the channel's next update.
		c.Logger.Errorf("updateChannel %d of user %d: %v", change.ChannelId, selfId, err)
	}
	updates := []*mtproto.Update{mtproto.MakeTLUpdateChannel(&mtproto.Update{ChannelId: change.ChannelId}).To_Update()}
	if change.Message != nil {
		shown := proto.Clone(change.Message).(*mtproto.Message)
		shown.Out = true
		updates = append(updates, mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: shown, Pts_INT32: change.Pts, PtsCount: 1}).To_Update())
	}
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: updates,
		Users:   []*mtproto.User{},
		Chats:   []*mtproto.Chat{view},
		Date:    now,
	}).To_Updates(), nil
}

// ChannelCheckChatInvite
// channel.checkChatInvite self_id:long link:string = ChatInvite;
//
// What a channel's invite link shows: chatInviteAlready with the channel to
// a member, whatever the link's state; to anyone else chatInvite (title,
// about, photo, the count, the kind), INVITE_HASH_EXPIRED for a link that
// no longer works, CHANNEL_PRIVATE to one kicked from the channel (as
// channels.joinChannel). The members' avatars (participants) are left
// out: the field is optional.
func (c *ChannelCore) ChannelCheckChatInvite(in *channel.TLChannelCheckChatInvite) (*mtproto.ChatInvite, error) {
	inv, err := c.svcCtx.Dao.Invite(c.ctx, in.Link)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	mc, err := c.svcCtx.Dao.GetMutableChannel(c.ctx, inv.ChatId, []int64{in.SelfId})
	if err != nil {
		return nil, err
	}
	p, _ := mc.GetParticipant(in.SelfId)
	switch {
	case p.Member():
		// A member is shown the chat whatever the link's state.
		return mtproto.MakeTLChatInviteAlready(&mtproto.ChatInvite{
			Chat: mc.ToChat(in.SelfId, int32(now.Unix())),
		}).To_ChatInvite(), nil
	case !inv.Works(now.Unix()):
		return nil, mtproto.ErrInviteHashExpired
	case p.Kicked(int32(now.Unix())):
		return nil, mtproto.ErrChannelPrivate
	}
	ch := mc.GetChannel()
	photo := ch.GetPhoto()
	if photo == nil {
		photo = mtproto.MakeTLPhotoEmpty(nil).To_Photo()
	}
	return mtproto.MakeTLChatInvite(&mtproto.ChatInvite{
		Channel:           true,
		Broadcast:         ch.GetBroadcast(),
		Megagroup:         ch.GetMegagroup(),
		Public:            ch.Public(),
		RequestNeeded:     inv.RequestNeeded,
		Title:             ch.GetTitle(),
		About:             mtproto.MakeFlagsString(ch.GetAbout()),
		Photo:             photo,
		ParticipantsCount: ch.GetParticipantsCount(),
	}).To_ChatInvite(), nil
}

// ChannelGetParticipants
// channel.getParticipants self_id:long channel_id:long filter:string q:string offset:int limit:int = channels.ChannelParticipants;
//
// A page of the members (filter: the predicate of channelParticipantsFilter)
// as self_id sees them: anyone who may read a supergroup, only its admins
// in a channel (CHAT_ADMIN_REQUIRED otherwise; the list of a channel's
// subscribers is its admins', server.md §2.6 of task 035).
func (c *ChannelCore) ChannelGetParticipants(in *channel.TLChannelGetParticipants) (*mtproto.Channels_ChannelParticipants, error) {
	mc, err := c.svcCtx.Dao.GetMutableChannel(c.ctx, in.ChannelId, []int64{in.SelfId})
	if err != nil {
		return nil, err
	}
	me, _ := mc.GetParticipant(in.SelfId)
	admin := me.Member() && me.GetRole() != channel.RoleMember
	// One not in it reads a public channel unless kicked from it.
	reader := me.Member() || mc.GetChannel().Public() && !me.Kicked(int32(time.Now().Unix()))
	switch {
	case mc.GetChannel().GetDeleted():
		return nil, mtproto.ErrChannelInvalid
	case !reader:
		return nil, mtproto.ErrChannelPrivate
	case mc.GetChannel().GetBroadcast() && !admin:
		return nil, mtproto.ErrChatAdminRequired
	}
	list, count, err := c.svcCtx.Dao.Participants(c.ctx, in.ChannelId, in.SelfId, in.Filter, in.Q, in.Offset, in.Limit)
	if errors.Is(err, dao.ErrUnknownFilter) {
		return nil, mtproto.ErrInputConstructorInvalid
	}
	if err != nil {
		c.Logger.Errorf("channel.getParticipants - channel %d: %v", in.ChannelId, err)
		return nil, err
	}
	participants := make([]*mtproto.ChannelParticipant, 0, len(list))
	for _, p := range list {
		participants = append(participants, p.ToChannelParticipant(in.SelfId))
	}
	return mtproto.MakeTLChannelsChannelParticipants(&mtproto.Channels_ChannelParticipants{
		Count:        count,
		Participants: participants,
		Chats:        []*mtproto.Chat{},
		Users:        []*mtproto.User{},
	}).To_Channels_ChannelParticipants(), nil
}
