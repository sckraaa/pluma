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
	"fmt"
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/notification/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/notification/internal/svc"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	chat_client "github.com/teamgram/teamgram-server/app/service/biz/chat/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// exceptionUsers is biz user with the notify settings of one account.
type exceptionUsers struct {
	user_client.UserClient
	settings []*userpb.PeerPeerNotifySettings
	topics   int
}

func (f *exceptionUsers) UserGetAllNotifySettings(context.Context, *userpb.TLUserGetAllNotifySettings) (*userpb.Vector_PeerPeerNotifySettings, error) {
	return &userpb.Vector_PeerPeerNotifySettings{Datas: f.settings}, nil
}

func (f *exceptionUsers) UserGetMutableUsers(_ context.Context, in *userpb.TLUserGetMutableUsers) (*userpb.Vector_ImmutableUser, error) {
	out := &userpb.Vector_ImmutableUser{}
	for _, id := range in.GetId() {
		out.Datas = append(out.Datas, mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{
			User: mtproto.MakeTLUserData(&mtproto.UserData{Id: id, FirstName: "U", UserType: userpb.UserTypeRegular}).To_UserData(),
		}).To_ImmutableUser())
	}
	return out, nil
}

func (f *exceptionUsers) UserGetTopicNotifyExceptions(context.Context, *userpb.TLUserGetTopicNotifyExceptions) (*mtproto.Updates, error) {
	f.topics++
	return mtproto.MakeEmptyUpdates(), nil
}

type exceptionChats struct{ chat_client.ChatClient }

func (exceptionChats) ChatGetChatListByIdList(context.Context, *chatpb.TLChatGetChatListByIdList) (*chatpb.Vector_MutableChat, error) {
	return &chatpb.Vector_MutableChat{}, nil
}

// exceptionChannels: the channels with an even id are supergroups, 999
// does not exist.
type exceptionChannels struct{ channel_client.ChannelClient }

func (exceptionChannels) ChannelGetChannelListByIdList(_ context.Context, in *channelpb.TLChannelGetChannelListByIdList) (*channelpb.Vector_MutableChannel, error) {
	out := &channelpb.Vector_MutableChannel{}
	for _, id := range in.GetIdList() {
		if id == 999 {
			continue
		}
		out.Datas = append(out.Datas, &channelpb.MutableChannel{Channel: &channelpb.ImmutableChannel{
			Id: id, AccessHash: id * 10, Title: "c", Megagroup: id%2 == 0, Broadcast: id%2 != 0, Username: "public",
		}})
	}
	return out, nil
}

func peerSettings(peerType int32, peerId int64, s *mtproto.PeerNotifySettings) *userpb.PeerPeerNotifySettings {
	return userpb.MakeTLPeerPeerNotifySettings(&userpb.PeerPeerNotifySettings{
		PeerType: peerType, PeerId: peerId, Settings: mtproto.MakeTLPeerNotifySettings(s).To_PeerNotifySettings(),
	}).To_PeerPeerNotifySettings()
}

// The exceptions are the chats with settings of their own, by category: a
// user, a basic group or a supergroup, a channel. The settings of a class
// and a chat with nothing set are none; a sound alone counts only with
// compare_sound.
func TestGetNotifyExceptions(t *testing.T) {
	muted := &mtproto.PeerNotifySettings{MuteUntil: wrapperspb.Int32(2147483647)}
	users := &exceptionUsers{settings: []*userpb.PeerPeerNotifySettings{
		peerSettings(mtproto.PEER_USERS, 0, &mtproto.PeerNotifySettings{MuteUntil: wrapperspb.Int32(0), ShowPreviews: mtproto.BoolTrue}),
		peerSettings(mtproto.PEER_USER, 11, muted),
		peerSettings(mtproto.PEER_USER, 12, &mtproto.PeerNotifySettings{}),                                    // nothing set
		peerSettings(mtproto.PEER_USER, 13, &mtproto.PeerNotifySettings{Sound: wrapperspb.String("chime")}),   // a sound alone
		peerSettings(mtproto.PEER_USER, 14, &mtproto.PeerNotifySettings{Sound: wrapperspb.String("default")}), // the default sound
		peerSettings(mtproto.PEER_USER, 15, &mtproto.PeerNotifySettings{MuteUntil: wrapperspb.Int32(0)}),      // unmuted on its own
		peerSettings(mtproto.PEER_CHAT, 21, &mtproto.PeerNotifySettings{ShowPreviews: mtproto.BoolFalse}),
		peerSettings(mtproto.PEER_CHANNEL, 32, muted), // a supergroup
		peerSettings(mtproto.PEER_CHANNEL, 33, muted), // a channel
		peerSettings(mtproto.PEER_CHANNEL, 999, muted),
	}}
	ctx := context.Background()
	c := &NotificationCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Dao: &dao.Dao{UserClient: users, ChatClient: exceptionChats{}, Channel: exceptionChannels{}}},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: 1},
	}
	get := func(in *mtproto.TLAccountGetNotifyExceptions) (peers []string, userIds, chatIds []int64) {
		t.Helper()
		r, err := c.AccountGetNotifyExceptions(in)
		if err != nil {
			t.Fatal(err)
		}
		for _, u := range r.GetUpdates() {
			if u.GetPredicateName() != mtproto.Predicate_updateNotifySettings || u.GetNotifySettings() == nil {
				t.Fatalf("update %v", u)
			}
			p := u.GetPeer_NOTIFYPEER().GetPeer()
			peers = append(peers, fmt.Sprintf("%s:%d", p.GetPredicateName(), p.GetUserId()+p.GetChatId()+p.GetChannelId()))
		}
		for _, u := range r.GetUsers() {
			userIds = append(userIds, u.GetId())
		}
		for _, ch := range r.GetChats() {
			chatIds = append(chatIds, ch.GetId())
		}
		slices.Sort(peers)
		slices.Sort(userIds)
		slices.Sort(chatIds)
		return peers, userIds, chatIds
	}
	usersCategory := mtproto.MakeTLInputNotifyUsers(nil).To_InputNotifyPeer()
	chatsCategory := mtproto.MakeTLInputNotifyChats(nil).To_InputNotifyPeer()
	broadcastsCategory := mtproto.MakeTLInputNotifyBroadcasts(nil).To_InputNotifyPeer()

	for _, k := range []struct {
		name  string
		in    *mtproto.TLAccountGetNotifyExceptions
		peers string
		users string
		chats string
	}{
		{"every category", &mtproto.TLAccountGetNotifyExceptions{},
			"[peerChannel:32 peerChannel:33 peerChannel:999 peerChat:21 peerUser:11 peerUser:15]", "[11 15]", "[32 33]"},
		{"every category with sounds", &mtproto.TLAccountGetNotifyExceptions{CompareSound: true},
			"[peerChannel:32 peerChannel:33 peerChannel:999 peerChat:21 peerUser:11 peerUser:13 peerUser:15]", "[11 13 15]", "[32 33]"},
		{"stories add nothing", &mtproto.TLAccountGetNotifyExceptions{CompareStories: true},
			"[peerChannel:32 peerChannel:33 peerChannel:999 peerChat:21 peerUser:11 peerUser:15]", "[11 15]", "[32 33]"},
		{"users", &mtproto.TLAccountGetNotifyExceptions{Peer: usersCategory}, "[peerUser:11 peerUser:15]", "[11 15]", "[]"},
		{"users with sounds", &mtproto.TLAccountGetNotifyExceptions{Peer: usersCategory, CompareSound: true}, "[peerUser:11 peerUser:13 peerUser:15]", "[11 13 15]", "[]"},
		{"groups and supergroups", &mtproto.TLAccountGetNotifyExceptions{Peer: chatsCategory}, "[peerChannel:32 peerChat:21]", "[]", "[32]"},
		{"channels", &mtproto.TLAccountGetNotifyExceptions{Peer: broadcastsCategory}, "[peerChannel:33]", "[]", "[33]"},
		{"one user", &mtproto.TLAccountGetNotifyExceptions{Peer: mtproto.MakeTLInputNotifyPeer(&mtproto.InputNotifyPeer{
			Peer: mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: 11}).To_InputPeer(),
		}).To_InputNotifyPeer()}, "[peerUser:11]", "[11]", "[]"},
		{"one user with nothing set", &mtproto.TLAccountGetNotifyExceptions{Peer: mtproto.MakeTLInputNotifyPeer(&mtproto.InputNotifyPeer{
			Peer: mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: 12}).To_InputPeer(),
		}).To_InputNotifyPeer()}, "[]", "[]", "[]"},
	} {
		peers, userIds, chatIds := get(k.in)
		if fmt.Sprint(peers) != k.peers || fmt.Sprint(userIds) != k.users || fmt.Sprint(chatIds) != k.chats {
			t.Errorf("%s: peers %v, users %v, chats %v; want %s, %s, %s", k.name, peers, userIds, chatIds, k.peers, k.users, k.chats)
		}
	}

	// A channel as the peer: the exceptions of its topics, from biz user.
	if _, err := c.AccountGetNotifyExceptions(&mtproto.TLAccountGetNotifyExceptions{Peer: mtproto.MakeTLInputNotifyPeer(&mtproto.InputNotifyPeer{
		Peer: mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: 32, AccessHash: 320}).To_InputPeer(),
	}).To_InputNotifyPeer()}); err != nil || users.topics != 1 {
		t.Errorf("a forum as the peer: %v, biz asked %d times", err, users.topics)
	}
}
