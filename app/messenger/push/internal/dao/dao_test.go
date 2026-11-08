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

package dao

import (
	"context"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/push/internal/core"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

type recipientsChannel struct {
	channel_client.ChannelClient
	members []int64
}

func (c *recipientsChannel) ChannelGetMemberIdList(context.Context, *channelpb.TLChannelGetMemberIdList) (*channelpb.Vector_Long, error) {
	return &channelpb.Vector_Long{Datas: c.members}, nil
}

func (c *recipientsChannel) ChannelGetMutableChannel(context.Context, *channelpb.TLChannelGetMutableChannel) (*channelpb.MutableChannel, error) {
	return &channelpb.MutableChannel{Channel: &channelpb.ImmutableChannel{Id: 100, Megagroup: true}}, nil
}

// recipientsUser answers as biz user does: the rows of the ids asked for.
type recipientsUser struct {
	user_client.UserClient
}

func (recipientsUser) UserGetMutableUsersV2(_ context.Context, in *user.TLUserGetMutableUsersV2) (*mtproto.MutableUsers, error) {
	out := &mtproto.MutableUsers{}
	for _, id := range in.Id {
		out.Users = append(out.Users, mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{
			User: mtproto.MakeTLUserData(&mtproto.UserData{Id: id, AccessHash: id * 10, FirstName: "User", UserType: user.UserTypeRegular}).To_UserData(),
		}).To_ImmutableUser())
	}
	return out, nil
}

// TestRecipientsHaveTheSender: each member's push carries the sender of the
// message, whose name the notification shows.
func TestRecipientsHaveTheSender(t *testing.T) {
	const sender, member = 1, 2
	d := &Channels{Channel: &recipientsChannel{members: []int64{sender, member}}, User: recipientsUser{}}
	m := mtproto.MakeTLMessage(&mtproto.Message{Id: 7, FromId: mtproto.MakePeerUser(sender), PeerId: mtproto.MakePeerChannel(100)}).To_Message()
	events, err := d.Recipients(context.Background(), &core.ChannelEvent{
		ChannelId: 100,
		FromId:    sender,
		Updates:   mtproto.MakeUpdatesByUpdates(mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: m}).To_Update()),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].UserId != member {
		t.Fatalf("events %v, want one for the member", events)
	}
	users := events[0].Updates.GetUsers()
	if len(users) != 1 || users[0].GetId() != sender {
		t.Errorf("users of the push: %v, want the sender", users)
	}
}
