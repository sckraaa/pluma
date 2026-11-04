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

package seqpeers

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

const self = int64(1)

func peerSettings(userId int64) *mtproto.Update {
	return mtproto.MakeTLUpdatePeerSettings(&mtproto.Update{
		Peer_PEER: mtproto.MakePeerUser(userId),
		Settings:  mtproto.MakeTLPeerSettings(&mtproto.PeerSettings{}).To_PeerSettings(),
	}).To_Update()
}

func chatParticipants(chatId int64, members ...[2]int64) *mtproto.Update {
	var ps []*mtproto.ChatParticipant
	for i, m := range members {
		if i == 0 {
			ps = append(ps, mtproto.MakeTLChatParticipantCreator(&mtproto.ChatParticipant{UserId: m[0]}).To_ChatParticipant())
			continue
		}
		ps = append(ps, mtproto.MakeTLChatParticipant(&mtproto.ChatParticipant{UserId: m[0], InviterId: m[1]}).To_ChatParticipant())
	}
	return mtproto.MakeTLUpdateChatParticipants(&mtproto.Update{
		Participants_CHATPARTICIPANTS: mtproto.MakeTLChatParticipants(&mtproto.ChatParticipants{
			ChatId:       chatId,
			Participants: ps,
			Version:      3,
		}).To_ChatParticipants(),
	}).To_Update()
}

func pinnedChannel(channelId int64) *mtproto.Update {
	return mtproto.MakeTLUpdateDialogPinned(&mtproto.Update{
		Peer_DIALOGPEER: mtproto.MakeTLDialogPeer(&mtproto.DialogPeer{Peer: mtproto.MakePeerChannel(channelId)}).To_DialogPeer(),
	}).To_Update()
}

func picked(updates ...*mtproto.Update) string {
	h := mtproto.NewIDListHelper()
	Pick(h, self, updates...)
	return fmt.Sprintf("users %v chats %v channels %v", h.UserIdList, h.ChatIdList, h.ChannelIdList)
}

func TestPick(t *testing.T) {
	for _, c := range []struct {
		name string
		u    *mtproto.Update
		want string
	}{
		{"the peer of settings", peerSettings(7), "users [7] chats [] channels []"},
		{"another user's name", mtproto.MakeTLUpdateUserName(&mtproto.Update{UserId: 8}).To_Update(), "users [8] chats [] channels []"},
		{"another user", mtproto.MakeTLUpdateUser(&mtproto.Update{UserId: 9}).To_Update(), "users [9] chats [] channels []"},
		{"a pinned group", mtproto.MakeTLUpdateDialogPinned(&mtproto.Update{
			Peer_DIALOGPEER: mtproto.MakeTLDialogPeer(&mtproto.DialogPeer{Peer: mtproto.MakePeerChat(4)}).To_DialogPeer(),
		}).To_Update(), "users [] chats [4] channels []"},
		{"a pinned channel", pinnedChannel(4), "users [] chats [] channels [4]"},
		{"the members of a group and who invited them",
			chatParticipants(5, [2]int64{2, 0}, [2]int64{3, 2}, [2]int64{self, 3}, [2]int64{6, 2}),
			"users [2 3 1 6] chats [5] channels []"},
		{"the own participant of a group left", mtproto.MakeTLUpdateChatParticipants(&mtproto.Update{
			Participants_CHATPARTICIPANTS: mtproto.MakeTLChatParticipantsForbidden(&mtproto.ChatParticipants{
				ChatId:          5,
				SelfParticipant: mtproto.MakeTLChatParticipant(&mtproto.ChatParticipant{UserId: self, InviterId: 2}).To_ChatParticipant(),
			}).To_ChatParticipants(),
		}).To_Update(), "users [1 2] chats [5] channels []"},
		{"a group", mtproto.MakeTLUpdateChat(&mtproto.Update{ChatId_INT64: 5}).To_Update(), "users [] chats [5] channels []"},
		{"the default rights of a group", mtproto.MakeTLUpdateChatDefaultBannedRights(&mtproto.Update{
			Peer_PEER:           mtproto.MakePeerChat(5),
			DefaultBannedRights: mtproto.MakeTLChatBannedRights(&mtproto.ChatBannedRights{}).To_ChatBannedRights(),
		}).To_Update(), "users [] chats [5] channels []"},
		{"join requests and the recent requesters", mtproto.MakeTLUpdatePendingJoinRequests(&mtproto.Update{
			Peer_PEER:        mtproto.MakePeerChat(5),
			RequestsPending:  2,
			RecentRequesters: []int64{11, 12},
		}).To_Update(), "users [11 12] chats [5] channels []"},
		{"a channel to catch up with", mtproto.MakeTLUpdateChannelTooLong(&mtproto.Update{
			ChannelId:     1000000004,
			Pts_FLAGINT32: mtproto.MakeFlagsInt32(9),
		}).To_Update(), "users [] chats [] channels [1000000004]"},
		{"no user 0", mtproto.MakeTLUpdateUserName(&mtproto.Update{}).To_Update(), "users [] chats [] channels []"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := picked(c.u); got != c.want {
				t.Errorf("%s, want %s", got, c.want)
			}
		})
	}
}

// source serves users with names as viewers see them: a contact of the
// viewer by the name the viewer gave them.
type source struct {
	users    map[int64]*mtproto.ImmutableUser
	chats    map[int64]bool
	channels map[int64]*channelpb.MutableChannel
	userErr  error
	chatErr  error
	chanErr  error
	userAsks [][]int64
	chanAsks []*channelpb.TLChannelGetChannelListByIdList
}

func (s *source) ChannelGetChannelListByIdList(_ context.Context, in *channelpb.TLChannelGetChannelListByIdList) (*channelpb.Vector_MutableChannel, error) {
	s.chanAsks = append(s.chanAsks, in)
	if s.chanErr != nil {
		return nil, s.chanErr
	}
	out := &channelpb.Vector_MutableChannel{}
	for _, id := range in.IdList {
		if c, ok := s.channels[id]; ok {
			out.Datas = append(out.Datas, c)
		}
	}
	return out, nil
}

// mutableChannel is a private supergroup with the viewer's row in state.
func mutableChannel(id int64, state int32) *channelpb.MutableChannel {
	return channelpb.MakeTLMutableChannel(&channelpb.MutableChannel{
		Channel: channelpb.MakeTLImmutableChannel(&channelpb.ImmutableChannel{Id: id, AccessHash: id * 3, Title: "channel", Megagroup: true}).To_ImmutableChannel(),
		Participants: []*channelpb.ImmutableChannelParticipant{
			channelpb.MakeTLImmutableChannelParticipant(&channelpb.ImmutableChannelParticipant{ChannelId: id, UserId: self, State: state}).To_ImmutableChannelParticipant(),
		},
	}).To_MutableChannel()
}

func (s *source) UserGetMutableUsers(_ context.Context, in *userpb.TLUserGetMutableUsers) (*userpb.Vector_ImmutableUser, error) {
	s.userAsks = append(s.userAsks, in.Id)
	if s.userErr != nil {
		return nil, s.userErr
	}
	out := &userpb.Vector_ImmutableUser{}
	for _, id := range in.Id {
		if u, ok := s.users[id]; ok {
			out.Datas = append(out.Datas, u)
		}
	}
	return out, nil
}

func (s *source) ChatGetChatListByIdList(_ context.Context, in *chatpb.TLChatGetChatListByIdList) (*chatpb.Vector_MutableChat, error) {
	if s.chatErr != nil {
		return nil, s.chatErr
	}
	out := &chatpb.Vector_MutableChat{}
	for _, id := range in.IdList {
		if s.chats[id] {
			out.Datas = append(out.Datas, mtproto.MakeTLMutableChat(&mtproto.MutableChat{
				Chat: mtproto.MakeTLImmutableChat(&mtproto.ImmutableChat{Id: id, Creator: 2, Title: "group"}).To_ImmutableChat(),
				ChatParticipants: []*mtproto.ImmutableChatParticipant{
					mtproto.MakeTLImmutableChatParticipant(&mtproto.ImmutableChatParticipant{ChatId: id, UserId: self, State: mtproto.ChatMemberStateNormal}).To_ImmutableChatParticipant(),
				},
			}).To_MutableChat())
		}
	}
	return out, nil
}

func immutableUser(id int64, bot bool, contacts ...*mtproto.ContactData) *mtproto.ImmutableUser {
	return mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{
		User: mtproto.MakeTLUserData(&mtproto.UserData{
			Id:         id,
			AccessHash: id * 7,
			FirstName:  fmt.Sprintf("U%d", id),
			Bot:        botData(bot),
		}).To_UserData(),
		Contacts: contacts,
	}).To_ImmutableUser()
}

func botData(bot bool) *mtproto.BotData {
	if !bot {
		return nil
	}
	return mtproto.MakeTLBotData(&mtproto.BotData{}).To_BotData()
}

func newSource() *source {
	return &source{
		users: map[int64]*mtproto.ImmutableUser{
			self: immutableUser(self, false, mtproto.MakeTLContactData(&mtproto.ContactData{
				UserId: self, ContactUserId: 7, FirstName: mtproto.MakeFlagsString("Seventh"),
			}).To_ContactData()),
			7: immutableUser(7, false),
			8: immutableUser(8, false),
		},
		chats: map[int64]bool{5: true},
		// A channel with the id of the group: ids of groups and of
		// channels are apart.
		channels: map[int64]*channelpb.MutableChannel{
			5:  mutableChannel(5, channelpb.StateIn),
			50: mutableChannel(50, channelpb.StateLeft),
		},
	}
}

func TestResolve(t *testing.T) {
	ctx := context.Background()
	updates := []*mtproto.Update{
		peerSettings(7),
		mtproto.MakeTLUpdateUserName(&mtproto.Update{UserId: 8}).To_Update(),
		mtproto.MakeTLUpdateChat(&mtproto.Update{ChatId_INT64: 5}).To_Update(),
		peerSettings(99), // not found
	}
	src := newSource()
	h := mtproto.NewIDListHelper()
	Pick(h, self, updates...)
	p, err := Resolve(ctx, src, self, h)
	if err != nil {
		t.Fatal(err)
	}
	if p.Viewer.Id() != self {
		t.Errorf("viewer %v", p.Viewer)
	}
	// One look-up of the users, the viewer first, each once.
	if len(src.userAsks) != 1 || !slices.Equal(src.userAsks[0], []int64{self, 7, 8, 99}) {
		t.Errorf("users asked %v", src.userAsks)
	}
	var names []string
	for _, u := range p.Users {
		names = append(names, fmt.Sprintf("%d:%s:%v", u.GetId(), u.GetFirstName().GetValue(), u.GetContact()))
	}
	if want := []string{"7:Seventh:true", "8:U8:false"}; !slices.Equal(names, want) {
		t.Errorf("users %v, want %v (as the viewer sees them)", names, want)
	}
	if len(p.Chats) != 1 || p.Chats[0].GetId() != 5 {
		t.Errorf("chats %v", p.Chats)
	}

	// Each update goes with its own peers.
	for i, want := range []string{"[7] []", "[8] []", "[] [5]", "[] []"} {
		users, chats := p.Of(updates[i])
		var uids, cids []int64
		for _, u := range users {
			uids = append(uids, u.GetId())
		}
		for _, c := range chats {
			cids = append(cids, c.GetId())
		}
		if got := fmt.Sprintf("%v %v", uids, cids); got != want {
			t.Errorf("peers of %s: %s, want %s", updates[i].GetPredicateName(), got, want)
		}
	}

	// What was asked for and not found: user 99 here, a chat below.
	if users, chats, channels := p.Missing(h); !slices.Equal(users, []int64{99}) || len(chats) != 0 || len(channels) != 0 {
		t.Errorf("missing users %v, chats %v; want [99] []", users, chats)
	}
	noChat := mtproto.NewIDListHelper()
	Pick(noChat, self, mtproto.MakeTLUpdateChat(&mtproto.Update{ChatId_INT64: 6}).To_Update(), updates[0])
	pc, err := Resolve(ctx, src, self, noChat)
	if err != nil {
		t.Fatal(err)
	}
	if users, chats, channels := pc.Missing(noChat); len(users) != 0 || !slices.Equal(chats, []int64{6}) || len(channels) != 0 {
		t.Errorf("missing users %v, chats %v; want [] [6]", users, chats)
	}

	// The viewer is looked up without peers too, and found a bot.
	src.users[self] = immutableUser(self, true)
	if p, err = Resolve(ctx, src, self, mtproto.NewIDListHelper()); err != nil || !p.Viewer.IsBot() || len(p.Users) != 0 {
		t.Errorf("viewer alone: %v, %v", p, err)
	}

	// A failed look-up keeps what the other found.
	src.chatErr = errors.New("chat unavailable")
	p, err = Resolve(ctx, src, self, h)
	if !errors.Is(err, src.chatErr) || len(p.Users) != 2 || len(p.Chats) != 0 || p.Viewer == nil {
		t.Errorf("chats failed: %v, users %d, chats %d", err, len(p.Users), len(p.Chats))
	}
	if len(src.chanAsks) != 0 {
		t.Errorf("channels asked for without channels: %v", src.chanAsks)
	}
	src.userErr = errors.New("user unavailable")
	p, err = Resolve(ctx, src, self, h)
	if !errors.Is(err, src.userErr) || !errors.Is(err, src.chatErr) || p.Viewer != nil || len(p.Users) != 0 {
		t.Errorf("both failed: %v, %v", err, p)
	}
}

func TestResolveChannels(t *testing.T) {
	ctx := context.Background()
	updates := []*mtproto.Update{
		pinnedChannel(5),
		mtproto.MakeTLUpdateChat(&mtproto.Update{ChatId_INT64: 5}).To_Update(),
		pinnedChannel(50),
		pinnedChannel(77), // no such channel
	}
	src := newSource()
	h := mtproto.NewIDListHelper()
	Pick(h, self, updates...)
	p, err := Resolve(ctx, src, self, h)
	if err != nil {
		t.Fatal(err)
	}
	// One look-up of the channels, as the viewer sees them.
	if len(src.chanAsks) != 1 || src.chanAsks[0].SelfId != self || !slices.Equal(src.chanAsks[0].IdList, []int64{5, 50, 77}) {
		t.Fatalf("channels asked %v", src.chanAsks)
	}
	var got []string
	for _, c := range p.Chats {
		got = append(got, fmt.Sprintf("%d:%s", c.GetId(), c.GetPredicateName()))
	}
	// The group first, then the channels: the member's channel and the
	// private channel the viewer left (channelForbidden).
	if want := []string{"5:chat", "5:channel", "50:channelForbidden"}; !slices.Equal(got, want) {
		t.Errorf("chats %v, want %v", got, want)
	}

	// Each update goes with its own peer, a group and a channel of one id
	// kept apart.
	for i, want := range []string{"5:channel", "5:chat", "50:channelForbidden", ""} {
		_, chats := p.Of(updates[i])
		var ids []string
		for _, c := range chats {
			ids = append(ids, fmt.Sprintf("%d:%s", c.GetId(), c.GetPredicateName()))
		}
		if g := fmt.Sprint(ids); g != fmt.Sprint(strings.Fields(want)) {
			t.Errorf("peers of update %d: %s, want [%s]", i, g, want)
		}
	}
	if users, chats, channels := p.Missing(h); len(users) != 0 || len(chats) != 0 || !slices.Equal(channels, []int64{77}) {
		t.Errorf("missing users %v, chats %v, channels %v; want [] [] [77]", users, chats, channels)
	}

	// A failed look-up of the channels keeps the users and the chats, and
	// every channel is missing.
	src.chanErr = errors.New("channel unavailable")
	p, err = Resolve(ctx, src, self, h)
	if !errors.Is(err, src.chanErr) || p.Viewer == nil || len(p.Chats) != 1 || p.Chats[0].GetPredicateName() != mtproto.Predicate_chat {
		t.Errorf("channels failed: %v, chats %v", err, p.Chats)
	}
	if _, _, channels := p.Missing(h); !slices.Equal(channels, []int64{5, 50, 77}) {
		t.Errorf("missing channels %v after a failed look-up", channels)
	}
	// And a failed look-up of the chats keeps the channels.
	src.chanErr, src.chatErr = nil, errors.New("chat unavailable")
	p, err = Resolve(ctx, src, self, h)
	if !errors.Is(err, src.chatErr) || len(p.Chats) != 2 {
		t.Errorf("chats failed: %v, chats %v", err, p.Chats)
	}
	if _, chats, channels := p.Missing(h); !slices.Equal(chats, []int64{5}) || !slices.Equal(channels, []int64{77}) {
		t.Errorf("missing chats %v, channels %v; want [5] [77]", chats, channels)
	}
}
