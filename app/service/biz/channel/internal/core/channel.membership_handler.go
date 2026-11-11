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
// (JoinChannel.swift:70-81). When an admin's approval is needed the request
// is written instead and the answer is INVITE_REQUEST_SENT.
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
	change, view, err := c.membership(in.SelfId, channelId, func(b *userbox.Box) (*dao.ChannelChange, error) {
		return c.svcCtx.Dao.Join(b, channelId, in.Link)
	}, in.PermAuthKeyId)
	if err != nil {
		return nil, c.membershipError("channel.joinChannel", err)
	}
	if change.Requested {
		// The request is committed: this is the answer the clients take
		// for it (core.telegram.org/api/invites).
		return nil, mtproto.ErrInviteRequestSent
	}
	return membershipAnswer(change, view), nil
}

// ChannelLeaveChannel
// channel.leaveChannel self_id:long perm_auth_key_id:long channel_id:long = Updates;
//
// self_id leaves the channel, as joining (ChannelJoinChannel) in reverse;
// the dialog goes. The answer holds updateChannel and the channel as it is
// now to self_id (left, or channelForbidden for a private one): Android
// drops the dialog on it (MessagesController.java:20153-20154).
func (c *ChannelCore) ChannelLeaveChannel(in *channel.TLChannelLeaveChannel) (*mtproto.Updates, error) {
	change, view, err := c.membership(in.SelfId, in.ChannelId, func(b *userbox.Box) (*dao.ChannelChange, error) {
		return c.svcCtx.Dao.Leave(b, in.ChannelId)
	}, in.PermAuthKeyId)
	if err != nil {
		return nil, c.membershipError("channel.leaveChannel", err)
	}
	return membershipAnswer(change, view), nil
}

// ChannelJoinHidden
// channel.joinHidden user_id:long channel_id:long = Bool;
//
// The ghost user_id joins the channel hidden, by pluma-admin ghost join
// (task 080, ADR 0048, p. 2d): any channel, public or private. As a join by
// the ghost itself, its box gets the dialog and updateChannel with seq;
// nobody else learns of it. true when it joined, false when it was in;
// CHANNEL_PRIVATE when a ban keeps it out, USER_ID_INVALID for an account
// that is no ghost.
func (c *ChannelCore) ChannelJoinHidden(in *channel.TLChannelJoinHidden) (*mtproto.Bool, error) {
	change, _, err := c.membership(in.UserId, in.ChannelId, func(b *userbox.Box) (*dao.ChannelChange, error) {
		return c.svcCtx.Dao.JoinHidden(b, in.ChannelId)
	}, 0)
	if err != nil {
		return nil, c.membershipError("channel.joinHidden", err)
	}
	return mtproto.ToBool(change.Self != nil), nil
}

// ChannelLeaveHidden
// channel.leaveHidden user_id:long channel_id:long = Bool;
//
// The ghost user_id leaves the channel where it is hidden, by pluma-admin
// ghost leave: its dialog goes, nobody else learns of it. true when it was
// in hidden.
func (c *ChannelCore) ChannelLeaveHidden(in *channel.TLChannelLeaveHidden) (*mtproto.Bool, error) {
	change, _, err := c.membership(in.UserId, in.ChannelId, func(b *userbox.Box) (*dao.ChannelChange, error) {
		return c.svcCtx.Dao.LeaveHidden(b, in.ChannelId)
	}, 0)
	if err != nil {
		return nil, c.membershipError("channel.leaveHidden", err)
	}
	return mtproto.ToBool(change.Self != nil), nil
}

// ChannelSetUserHidden
// channel.setUserHidden user_id:long hidden:Bool = Int32;
//
// The member rows of user_id become hidden (its account became a ghost) or
// shown (it stopped being one), with the counters of each channel, by
// pluma-admin ghost set (ADR 0048, p. 9): the number of channels changed.
func (c *ChannelCore) ChannelSetUserHidden(in *channel.TLChannelSetUserHidden) (*mtproto.Int32, error) {
	n, err := c.svcCtx.Dao.SetUserHidden(c.ctx, in.UserId, mtproto.FromBool(in.Hidden))
	if err != nil {
		c.Logger.Errorf("channel.setUserHidden - error: %v", err)
		return nil, err
	}
	return mtproto.MakeTLInt32(&mtproto.Int32{V: n}).To_Int32(), nil
}

// membershipError logs an error other than an answer of the method.
func (c *ChannelCore) membershipError(method string, err error) error {
	var rpcErr *mtproto.TLRpcError
	if !errors.As(err, &rpcErr) {
		c.Logger.Errorf("%s - error: %v", method, err)
	}
	return err
}

// membership runs a join or a leave of selfId (fn) in its box
// transaction and, in the same transaction, journals updateChannel with seq
// for its devices and pushes the service message to the members (ADR 0034,
// p. 2-3: the dialog, the seq row and the channel's row commit together).
// The channel as selfId sees it afterwards is the one read before the
// transaction, which calls no service, with the participant row and the
// count the transaction wrote; it goes with updateChannel and in the
// answer.
func (c *ChannelCore) membership(selfId, channelId int64, fn func(b *userbox.Box) (*dao.ChannelChange, error), permAuthKeyId int64) (*dao.ChannelChange, *mtproto.Chat, error) {
	mc, err := c.svcCtx.Dao.GetMutableChannel(c.ctx, channelId, []int64{selfId})
	if err != nil {
		return nil, nil, err
	}
	var (
		change *dao.ChannelChange
		view   *mtproto.Chat
	)
	_, err = c.svcCtx.Dao.RunBox(c.ctx, selfId, func(b *userbox.Box) error {
		var err error
		if change, err = fn(b); err != nil {
			return err
		}
		if change.Requested || change.Self == nil {
			// A request, or nothing changed (a ghost's hidden join or
			// leave that found it as it was).
			return nil
		}
		after := proto.Clone(mc).(*channel.MutableChannel)
		after.Channel.ParticipantsCount = change.Count
		after.Participants = []*channel.ImmutableChannelParticipant{change.Self}
		view = after.ToChat(selfId, int32(b.Date()))
		if err = c.svcCtx.Dao.EmitSeqUpdate(b, mtproto.MakeTLUpdateChannel(&mtproto.Update{ChannelId: channelId}).To_Update(), nil, []*mtproto.Chat{view}); err != nil {
			return err
		}
		return c.svcCtx.Dao.EmitChannelPush(b, permAuthKeyId, change)
	})
	return change, view, err
}

// membershipAnswer is the answer of a join or a leave: updateChannel, the
// service message as its author sees it, and the channel.
func membershipAnswer(change *dao.ChannelChange, view *mtproto.Chat) *mtproto.Updates {
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
		Date:    int32(time.Now().Unix()),
	}).To_Updates()
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
	admin := me.Admin()
	now := int32(time.Now().Unix())
	// One not in it reads a public channel unless kicked from it, and the
	// discussion group of a channel it is in (task 048): iOS asks for the
	// members of the group of the comments it opens, again and again on an
	// error, and the comments never opened (the owner's report of 08.11).
	reader := me.Member() || (mc.GetChannel().Public() || me.GetState() == channel.StateLinked) && !me.Kicked(now)
	switch {
	case mc.GetChannel().GetDeleted():
		return nil, mtproto.ErrChannelInvalid
	case !reader:
		return nil, mtproto.ErrChannelPrivate
	case mc.GetChannel().GetBroadcast() && !admin:
		return nil, mtproto.ErrChatAdminRequired
	}
	if !admin && (in.Filter == mtproto.Predicate_channelParticipantsKicked || in.Filter == mtproto.Predicate_channelParticipantsBanned) {
		// Who is kicked or restricted is for admins. An empty list, not an
		// error: iOS asks again and again on any error
		// (ChannelMembers.swift:91, Network.swift:1208-1215 at 6ad963e5).
		return mtproto.MakeTLChannelsChannelParticipants(&mtproto.Channels_ChannelParticipants{
			Participants: []*mtproto.ChannelParticipant{},
			Chats:        []*mtproto.Chat{},
			Users:        []*mtproto.User{},
		}).To_Channels_ChannelParticipants(), nil
	}
	filter := in.Filter
	if mc.GetChannel().GetParticipantsHidden() && !admin {
		// The members are hidden from members who are not admins (task
		// 044): the admins only, whatever was asked. A list, not an
		// error, which iOS asks again and again.
		filter = mtproto.Predicate_channelParticipantsAdmins
	}
	list, count, err := c.svcCtx.Dao.Participants(c.ctx, in.ChannelId, in.SelfId, filter, in.Q, in.Offset, in.Limit, now)
	if errors.Is(err, dao.ErrUnknownFilter) {
		return nil, mtproto.ErrInputConstructorInvalid
	}
	if err != nil {
		c.Logger.Errorf("channel.getParticipants - channel %d: %v", in.ChannelId, err)
		return nil, err
	}
	participants := make([]*mtproto.ChannelParticipant, 0, len(list))
	for _, p := range list {
		participants = append(participants, p.ToChannelParticipant(me, now))
	}
	return mtproto.MakeTLChannelsChannelParticipants(&mtproto.Channels_ChannelParticipants{
		Count:        count,
		Participants: participants,
		Chats:        []*mtproto.Chat{},
		Users:        []*mtproto.User{},
	}).To_Channels_ChannelParticipants(), nil
}
