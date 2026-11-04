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
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// Joining and leaving channels (task 041, ADR 0034, p. 3); biz channel
// writes them and answers as the clients take them.

// ChannelsJoinChannel24B524C5
// channels.joinChannel#24b524c5 channel:InputChannel = Updates;
//
// The account joins a public channel. A private one is CHANNEL_PRIVATE:
// it is joined by its link (messages.importChatInvite).
func (c *ChannelsCore) ChannelsJoinChannel24B524C5(in *mtproto.TLChannelsJoinChannel24B524C5) (*mtproto.Updates, error) {
	return c.join(in.Channel)
}

// ChannelsJoinChannel7F6A1E22
// channels.joinChannel#7f6a1e22 channel:InputChannel = messages.ChatInviteJoinResult;
//
// As channels.joinChannel#24b524c5, the updates in chatInviteJoinResultOk.
func (c *ChannelsCore) ChannelsJoinChannel7F6A1E22(in *mtproto.TLChannelsJoinChannel7F6A1E22) (*mtproto.Messages_ChatInviteJoinResult, error) {
	r, err := c.join(in.Channel)
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesChatInviteJoinResultOk(&mtproto.Messages_ChatInviteJoinResult{Updates: r}).To_Messages_ChatInviteJoinResult(), nil
}

func (c *ChannelsCore) join(in *mtproto.InputChannel) (*mtproto.Updates, error) {
	mc, err := c.mutableChannel(in)
	if err != nil {
		return nil, err
	}
	r, err := c.svcCtx.Channel.ChannelJoinChannel(c.ctx, &channelpb.TLChannelJoinChannel{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		ChannelId:     mc.GetChannel().GetId(),
	})
	if err != nil {
		return nil, err
	}
	return r, c.addUsers(r)
}

// ChannelsLeaveChannel
// channels.leaveChannel#f836aa95 channel:InputChannel = Updates;
func (c *ChannelsCore) ChannelsLeaveChannel(in *mtproto.TLChannelsLeaveChannel) (*mtproto.Updates, error) {
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	r, err := c.svcCtx.Channel.ChannelLeaveChannel(c.ctx, &channelpb.TLChannelLeaveChannel{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		ChannelId:     mc.GetChannel().GetId(),
	})
	if err != nil {
		return nil, err
	}
	return r, c.addUsers(r)
}

// ChannelsDeleteChannel
// channels.deleteChannel#c0111fe3 channel:InputChannel = Updates;
//
// The creator deletes the channel for everyone (channel.deleteChannel,
// task 055). Android removes the dialog before it asks and drops an error
// unseen (MessagesController.java:15865-15869, :15917-15922 at aac1efcb),
// iOS falls back to channels.leaveChannel on one
// (ManagedCloudChatRemoveMessagesOperations.swift:262-266 at 6ad963e5): the
// creator must not fail here.
func (c *ChannelsCore) ChannelsDeleteChannel(in *mtproto.TLChannelsDeleteChannel) (*mtproto.Updates, error) {
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	r, err := c.svcCtx.Channel.ChannelDeleteChannel(c.ctx, &channelpb.TLChannelDeleteChannel{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		ChannelId:     mc.GetChannel().GetId(),
	})
	if err != nil {
		return nil, err
	}
	return r, c.addUsers(r)
}

// addUsers puts the account and the users of r's messages in r's users.
func (c *ChannelsCore) addUsers(r *mtproto.Updates) error {
	h := mtproto.NewIDListHelper(c.MD.UserId)
	h.AppendUsers(c.MD.UserId)
	h.PickByUpdates(r.GetUpdates()...)
	users, err := c.svcCtx.User.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      h.UserIdList,
		Privacy: true,
		HasTo:   true,
		To:      []int64{c.MD.UserId},
	})
	if err != nil {
		return err
	}
	r.Users = users.GetUserListByIdList(c.MD.UserId, h.UserIdList...)
	return nil
}

// ChannelsGetParticipants
// channels.getParticipants#77ced9d0 channel:InputChannel filter:ChannelParticipantsFilter offset:int limit:int hash:long = channels.ChannelParticipants;
//
// A page of the members by the filter (task 041). The hash is not kept:
// the list is always sent, which channels.channelParticipantsNotModified
// only saves.
func (c *ChannelsCore) ChannelsGetParticipants(in *mtproto.TLChannelsGetParticipants) (*mtproto.Channels_ChannelParticipants, error) {
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	r, err := c.svcCtx.Channel.ChannelGetParticipants(c.ctx, &channelpb.TLChannelGetParticipants{
		SelfId:    c.MD.UserId,
		ChannelId: mc.GetChannel().GetId(),
		Filter:    in.GetFilter().GetPredicateName(),
		Q:         filterQuery(in.GetFilter()),
		Offset:    in.Offset,
		Limit:     in.Limit,
	})
	if err != nil {
		return nil, err
	}
	h := mtproto.NewIDListHelper(c.MD.UserId)
	for _, p := range r.GetParticipants() {
		h.AppendUsers(participantUser(p))
	}
	users, err := c.svcCtx.User.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      h.UserIdList,
		Privacy: true,
		HasTo:   true,
		To:      []int64{c.MD.UserId},
	})
	if err != nil {
		return nil, err
	}
	r.Users = users.GetUserListByIdList(c.MD.UserId, h.UserIdList...)
	r.Chats = []*mtproto.Chat{mc.ToChat(c.MD.UserId, int32(time.Now().Unix()))}
	return r, nil
}

// participantUser is the user of a channelParticipant*.
func participantUser(p *mtproto.ChannelParticipant) int64 {
	if id := p.GetUserId(); id != 0 {
		return id
	}
	return p.GetPeer().GetUserId()
}

// filterQuery is the text of a filter: q of search, kicked, banned and
// contacts, the optional q of mentions.
func filterQuery(f *mtproto.ChannelParticipantsFilter) string {
	if q := f.GetQ_STRING(); q != "" {
		return q
	}
	return f.GetQ_FLAGSTRING().GetValue()
}
