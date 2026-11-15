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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/notification/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/notification/internal/svc"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const notifyUser = 7

// notifyChannels is biz channel with four channels: 10 a supergroup and 11
// a broadcast channel the account is in, 12 a private supergroup it never
// joined, 13 a broadcast channel it was kicked from. The access_hash of a
// channel is its id times 100.
type notifyChannels struct{ channel_client.ChannelClient }

func (notifyChannels) ChannelGetMutableChannel(_ context.Context, in *channelpb.TLChannelGetMutableChannel) (*channelpb.MutableChannel, error) {
	ch := &channelpb.ImmutableChannel{
		Id:         in.GetChannelId(),
		AccessHash: in.GetChannelId() * 100,
		Title:      "c",
		Megagroup:  in.GetChannelId()%2 == 0,
		Broadcast:  in.GetChannelId()%2 != 0,
	}
	in2 := &channelpb.ImmutableChannelParticipant{ChannelId: ch.Id, UserId: notifyUser, Role: channelpb.RoleMember, State: channelpb.StateIn}
	switch in.GetChannelId() {
	case 10, 11:
		return &channelpb.MutableChannel{Channel: ch, Participants: []*channelpb.ImmutableChannelParticipant{in2}}, nil
	case 12:
		return &channelpb.MutableChannel{Channel: ch}, nil
	case 13:
		in2.State = channelpb.StateKicked
		in2.BannedRights = mtproto.BANNED_VIEW_MESSAGES
		return &channelpb.MutableChannel{Channel: ch, Participants: []*channelpb.ImmutableChannelParticipant{in2}}, nil
	}
	return nil, mtproto.ErrChannelInvalid
}

// notifySetter is biz user that keeps the last UserSetNotifySettings.
type notifySetter struct {
	user_client.UserClient
	set *userpb.TLUserSetNotifySettings
}

func (f *notifySetter) UserSetNotifySettings(_ context.Context, in *userpb.TLUserSetNotifySettings) (*mtproto.Bool, error) {
	f.set = in
	return mtproto.BoolTrue, nil
}

func muteChannel(c *NotificationCore, channelId, accessHash int64) (*mtproto.Bool, error) {
	return c.AccountUpdateNotifySettings(&mtproto.TLAccountUpdateNotifySettings{
		Peer: mtproto.MakeTLInputNotifyPeer(&mtproto.InputNotifyPeer{
			Peer: mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: channelId, AccessHash: accessHash}).To_InputPeer(),
		}).To_InputNotifyPeer(),
		Settings: mtproto.MakeTLInputPeerNotifySettings(&mtproto.InputPeerNotifySettings{
			MuteUntil: wrapperspb.Int32(2147483647),
		}).To_InputPeerNotifySettings(),
	})
}

// A supergroup and a broadcast channel are muted like any chat: biz keeps
// the settings for PEER_CHANNEL and pushes them with the channel; a wrong
// access_hash and a channel the account cannot read are refused before.
func TestUpdateNotifySettingsChannel(t *testing.T) {
	ctx := context.Background()
	users := &notifySetter{}
	c := &NotificationCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Dao: &dao.Dao{UserClient: users, Channel: notifyChannels{}}},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: notifyUser},
	}

	for _, id := range []int64{10, 11} {
		users.set = nil
		r, err := muteChannel(c, id, id*100)
		if err != nil {
			t.Fatalf("channel %d: %v", id, err)
		}
		if !mtproto.FromBool(r) {
			t.Errorf("channel %d: %v, want boolTrue", id, r)
		}
		set := users.set
		if set == nil || set.GetPeerType() != mtproto.PEER_CHANNEL || set.GetPeerId() != id || set.GetUserId() != notifyUser {
			t.Fatalf("channel %d: biz got %v", id, set)
		}
		if set.GetSettings().GetMuteUntil().GetValue() != 2147483647 {
			t.Errorf("channel %d: mute_until %v", id, set.GetSettings().GetMuteUntil())
		}
		if len(set.GetChats()) != 1 || set.GetChats()[0].GetId() != id || set.GetChats()[0].GetPredicateName() != mtproto.Predicate_channel {
			t.Errorf("channel %d: chats of the push %v", id, set.GetChats())
		}
	}

	for _, tc := range []struct {
		name       string
		id, hash   int64
		want       string
		wantNotSet bool
	}{
		{"wrong access_hash", 10, 999, "PEER_ID_INVALID", true},
		{"private, never joined", 12, 1200, "CHANNEL_PRIVATE", true},
		{"kicked", 13, 1300, "CHANNEL_PRIVATE", true},
		{"no such channel", 14, 1400, "CHANNEL_INVALID", true},
	} {
		users.set = nil
		_, err := muteChannel(c, tc.id, tc.hash)
		wantRpcError(t, tc.name, err, tc.want)
		if users.set != nil {
			t.Errorf("%s: biz got %v", tc.name, users.set)
		}
	}
}

// channelOf serves the forum topics too: a topic of a channel the account
// cannot read is refused the same way.
func TestForumOfUsesChannelOf(t *testing.T) {
	ctx := context.Background()
	c := &NotificationCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Dao: &dao.Dao{Channel: notifyChannels{}}},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: notifyUser},
	}
	topic := func(id, hash int64) *mtproto.InputNotifyPeer {
		return mtproto.MakeTLInputNotifyForumTopic(&mtproto.InputNotifyPeer{
			Peer:     mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: id, AccessHash: hash}).To_InputPeer(),
			TopMsgId: 1,
		}).To_InputNotifyPeer()
	}
	if id, err := c.forumOf(topic(10, 1000)); err != nil || id != 10 {
		t.Errorf("member: %d, %v", id, err)
	}
	_, err := c.forumOf(topic(10, 1))
	wantRpcError(t, "wrong access_hash", err, "PEER_ID_INVALID")
	_, err = c.forumOf(topic(12, 1200))
	wantRpcError(t, "private", err, "CHANNEL_PRIVATE")
}
