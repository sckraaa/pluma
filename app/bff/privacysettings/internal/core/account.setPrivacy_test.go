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
	"testing"

	"github.com/teamgram/proto/mtproto"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	chat_client "github.com/teamgram/teamgram-server/app/service/biz/chat/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"google.golang.org/protobuf/proto"
)

// privacyUsers keeps the requests of user.setPrivacy.
type privacyUsers struct {
	*fakeUsers
	set []*userpb.TLUserSetPrivacy
}

func (f *privacyUsers) UserSetPrivacy(_ context.Context, in *userpb.TLUserSetPrivacy) (*mtproto.Bool, error) {
	f.set = append(f.set, in)
	return mtproto.BoolTrue, nil
}

// privacyChats has the basic groups of the ids asked for.
type privacyChats struct {
	chat_client.ChatClient
}

func (privacyChats) ChatGetChatListByIdList(_ context.Context, in *chatpb.TLChatGetChatListByIdList) (*chatpb.Vector_MutableChat, error) {
	out := &chatpb.Vector_MutableChat{}
	for _, id := range in.IdList {
		out.Datas = append(out.Datas, mtproto.MakeTLMutableChat(&mtproto.MutableChat{
			Chat: mtproto.MakeTLImmutableChat(&mtproto.ImmutableChat{Id: id, Title: "G"}).To_ImmutableChat(),
			ChatParticipants: []*mtproto.ImmutableChatParticipant{
				mtproto.MakeTLImmutableChatParticipant(&mtproto.ImmutableChatParticipant{ChatId: id, UserId: 1, State: mtproto.ChatMemberStateNormal}).To_ImmutableChatParticipant(),
			},
		}).To_MutableChat())
	}
	return out, nil
}

func peerIds(users []*mtproto.User, chats []*mtproto.Chat) string {
	var s []string
	for _, u := range users {
		s = append(s, fmt.Sprintf("u%d", u.GetId()))
	}
	for _, c := range chats {
		s = append(s, fmt.Sprintf("c%d", c.GetId()))
	}
	return fmt.Sprint(s)
}

// account.setPrivacy passes the rules and the users and chats they name to
// biz, which stores them and pushes updatePrivacy with seq to every device
// of the account (ADR 0020); the answer has the same rules and peers.
func TestSetPrivacyPassesTheNamedPeers(t *testing.T) {
	c, fake, _ := newCore()
	users := &privacyUsers{fakeUsers: fake}
	c.svcCtx.Dao.UserClient = users
	c.svcCtx.Dao.ChatClient = privacyChats{}

	r, err := c.AccountSetPrivacy(&mtproto.TLAccountSetPrivacy{
		Key: mtproto.MakeTLInputPrivacyKeyStatusTimestamp(nil).To_InputPrivacyKey(),
		Rules: []*mtproto.InputPrivacyRule{
			mtproto.MakeTLInputPrivacyValueAllowContacts(nil).To_InputPrivacyRule(),
			mtproto.MakeTLInputPrivacyValueAllowUsers(&mtproto.InputPrivacyRule{Users: inputUsers(2)}).To_InputPrivacyRule(),
			mtproto.MakeTLInputPrivacyValueAllowChatParticipants(&mtproto.InputPrivacyRule{Chats: []int64{10}}).To_InputPrivacyRule(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(users.set) != 1 {
		t.Fatalf("%d requests of user.setPrivacy", len(users.set))
	}
	in := users.set[0]
	if in.GetUserId() != 1 || in.GetKeyType() != mtproto.STATUS_TIMESTAMP || len(in.GetRules()) != 3 || len(in.GetRules()) != len(r.GetRules()) {
		t.Fatalf("request %v, answer %v", in, r)
	}
	for i := range in.GetRules() {
		if !proto.Equal(in.GetRules()[i], r.GetRules()[i]) {
			t.Errorf("rule %d: stored %v, answered %v", i, in.GetRules()[i], r.GetRules()[i])
		}
	}
	if got, want := peerIds(in.GetUsers(), in.GetChats()), "[u1 u2 c10]"; got != want {
		t.Errorf("peers of the push %s, want %s", got, want)
	}
	if got, want := peerIds(r.GetUsers(), r.GetChats()), "[u1 u2 c10]"; got != want {
		t.Errorf("peers of the answer %s, want %s", got, want)
	}
}
