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
	"testing"

	"github.com/teamgram/proto/mtproto"
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
	userErr  error
	chatErr  error
	userAsks [][]int64
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
	src.userErr = errors.New("user unavailable")
	p, err = Resolve(ctx, src, self, h)
	if !errors.Is(err, src.userErr) || !errors.Is(err, src.chatErr) || p.Viewer != nil || len(p.Users) != 0 {
		t.Errorf("both failed: %v, %v", err, p)
	}
}
