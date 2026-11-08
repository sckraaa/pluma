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

// Package dao gives the push core what it reads from the other services.
package dao

import (
	"context"
	"slices"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/push/internal/core"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	authsession_client "github.com/teamgram/teamgram-server/app/service/authsession/client"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	message_client "github.com/teamgram/teamgram-server/app/service/biz/message/client"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	status_client "github.com/teamgram/teamgram-server/app/service/status/client"
	"github.com/teamgram/teamgram-server/app/service/status/status"

	"google.golang.org/protobuf/proto"
)

// Online reads the live sessions of status.
type Online struct {
	Client status_client.StatusClient
	now    func() time.Time
}

// OnlineAuthorizations returns the permanent keys of userId with a session
// that has not expired.
func (o *Online) OnlineAuthorizations(ctx context.Context, userId int64) (map[int64]bool, error) {
	list, err := o.Client.StatusGetUserOnlineSessions(ctx, &status.TLStatusGetUserOnlineSessions{UserId: userId})
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	if o.now != nil {
		now = o.now().Unix()
	}
	online := map[int64]bool{}
	for _, s := range list.GetUserSessions() {
		if s.GetExpired() < now {
			continue
		}
		online[s.GetAuthKeyId()] = true
		if s.GetPermAuthKeyId() != 0 {
			online[s.GetPermAuthKeyId()] = true
		}
	}
	return online, nil
}

// Authorizations reads authsession.
type Authorizations struct {
	Client authsession_client.AuthsessionClient
}

func (a *Authorizations) UserOf(ctx context.Context, authKeyId int64) (int64, error) {
	r, err := a.Client.AuthsessionGetUserId(ctx, &authsession.TLAuthsessionGetUserId{AuthKeyId: authKeyId})
	if err != nil {
		return 0, err
	}
	return r.GetV(), nil
}

func (a *Authorizations) LastActive(ctx context.Context, authKeyId int64) (time.Time, error) {
	r, err := a.Client.AuthsessionGetAuthorization(ctx, &authsession.TLAuthsessionGetAuthorization{AuthKeyId: authKeyId})
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(int64(r.GetDateActive()), 0), nil
}

// Settings reads the notification settings of the user service.
type Settings struct {
	Client user_client.UserClient
}

func (s *Settings) NotifySettings(ctx context.Context, userId int64, peerType int32, peerId int64) (*mtproto.PeerNotifySettings, error) {
	return s.Client.UserGetNotifySettings(ctx, &user.TLUserGetNotifySettings{UserId: userId, PeerType: peerType, PeerId: peerId})
}

// Messages reads the message service.
type Messages struct {
	Client message_client.MessageClient
}

func (m *Messages) UserMessage(ctx context.Context, userId int64, id int32) (*mtproto.Message, error) {
	box, err := m.Client.MessageGetUserMessage(ctx, &message.TLMessageGetUserMessage{UserId: userId, Id: id})
	if err != nil {
		return nil, err
	}
	return box.GetMessage(), nil
}

// Channels makes the pushes of a channel's event from the channel and user
// services, as sync makes the updates of the live sessions
// (sync.pushChannelUpdates).
type Channels struct {
	Channel channel_client.ChannelClient
	User    user_client.UserClient
}

// Recipients returns an event for each member of the channel but the
// author: the updates with the channel and the users as that member sees
// them, a message that mentions the member marked so.
func (d *Channels) Recipients(ctx context.Context, ev *core.ChannelEvent) ([]*core.Event, error) {
	members, err := d.Channel.ChannelGetMemberIdList(ctx, &channelpb.TLChannelGetMemberIdList{ChannelId: ev.ChannelId})
	if err != nil {
		return nil, err
	}
	var recipients []int64
	for _, id := range members.GetDatas() {
		if id != ev.FromId {
			recipients = append(recipients, id)
		}
	}
	if len(recipients) == 0 {
		return nil, nil
	}
	mc, err := d.Channel.ChannelGetMutableChannel(ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: ev.ChannelId, Id: recipients})
	if err != nil {
		return nil, err
	}
	h := mtproto.NewIDListHelper()
	h.PickByUpdates(ev.Updates.GetUpdates()...)
	// The recipients as well as the users of the messages: each one's view of
	// the users is made from its own row (GetUserListByIdList), and without
	// it the notification has no sender's name.
	users, err := d.User.UserGetMutableUsersV2(ctx, &user.TLUserGetMutableUsersV2{
		Id:      append(append([]int64{}, h.UserIdList...), recipients...),
		Privacy: true,
		HasTo:   true,
		To:      recipients,
	})
	if err != nil {
		return nil, err
	}
	now := int32(time.Now().Unix())
	out := make([]*core.Event, 0, len(recipients))
	for _, id := range recipients {
		updates := proto.Clone(ev.Updates).(*mtproto.Updates)
		mentioned := slices.Contains(ev.Mentioned, id)
		for _, u := range updates.GetUpdates() {
			if m := u.GetMessage_MESSAGE(); m != nil && mentioned {
				m.Mentioned = true
			}
		}
		updates.Chats = []*mtproto.Chat{mc.ToChat(id, now)}
		updates.Users = users.GetUserListByIdList(id, h.UserIdList...)
		out = append(out, &core.Event{UserId: id, Updates: updates})
	}
	return out, nil
}
