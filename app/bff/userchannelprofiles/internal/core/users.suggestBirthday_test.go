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
	"errors"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/userchannelprofiles/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/userchannelprofiles/internal/svc"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func birthday(day, month, year int32) *mtproto.Birthday {
	b := &mtproto.Birthday{Day: day, Month: month}
	if year != 0 {
		b.Year = wrapperspb.Int32(year)
	}
	return mtproto.MakeTLBirthday(b).To_Birthday()
}

// A birthday is a day of the calendar, with a year or without one, not in
// the future and at most 150 years ago.
func TestValidBirthday(t *testing.T) {
	now := time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)
	for _, k := range []struct {
		day, month, year int32
		want             bool
	}{
		{1, 1, 0, true},
		{29, 2, 0, true}, // without a year the 29th of February is a day
		{30, 2, 0, false},
		{31, 4, 0, false},
		{31, 12, 0, true},
		{0, 5, 0, false},
		{5, 0, 0, false},
		{5, 13, 0, false},
		{-1, 5, 0, false},
		{29, 2, 2024, true},
		{29, 2, 2023, false},
		{12, 11, 2026, true},  // today
		{13, 11, 2026, false}, // tomorrow
		{12, 11, 1876, true},  // 150 years ago
		{11, 11, 1876, false},
		{1, 1, -5, false},
	} {
		if got := validBirthday(birthday(k.day, k.month, k.year), now); got != k.want {
			t.Errorf("%d.%d.%d: %v, want %v", k.day, k.month, k.year, got, k.want)
		}
	}
	if validBirthday(nil, now) {
		t.Error("no birthday is valid")
	}
}

// birthdayUsers are users by id: 2 has no birthday, 3 has one, 4 is a bot,
// 5 is deleted; nobody else exists.
type birthdayUsers struct{ user_client.UserClient }

func (birthdayUsers) UserGetImmutableUser(_ context.Context, in *userpb.TLUserGetImmutableUser) (*mtproto.ImmutableUser, error) {
	data := &mtproto.UserData{Id: in.Id, FirstName: "U"}
	switch in.Id {
	case 2:
	case 3:
		data.Birthday = "0000-03-04"
	case 4:
		data.Bot = mtproto.MakeTLBotData(&mtproto.BotData{Creator: 1}).To_BotData()
	case 5:
		data.Deleted = true
	default:
		return nil, mtproto.ErrUserIdInvalid
	}
	return mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{User: mtproto.MakeTLUserData(data).To_UserData()}).To_ImmutableUser(), nil
}

// A suggestion is a service message of the private chat with the user,
// from the account, with the date; it goes to a user without a birthday.
func TestUsersSuggestBirthday(t *testing.T) {
	msg := &fakeMsg{}
	ctx := context.Background()
	c := &UserChannelProfilesCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Dao: &dao.Dao{UserClient: birthdayUsers{}, MsgClient: msg}},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: 1, PermAuthKeyId: 77},
	}
	user := func(id int64) *mtproto.InputUser {
		return mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: id, AccessHash: id}).To_InputUser()
	}
	for name, k := range map[string]struct {
		id   *mtproto.InputUser
		b    *mtproto.Birthday
		want error
	}{
		"the account itself":     {mtproto.MakeTLInputUserSelf(nil).To_InputUser(), birthday(1, 2, 0), mtproto.ErrUserIdInvalid},
		"the account by id":      {user(1), birthday(1, 2, 0), mtproto.ErrUserIdInvalid},
		"nobody":                 {user(99), birthday(1, 2, 0), mtproto.ErrUserIdInvalid},
		"a bot":                  {user(4), birthday(1, 2, 0), mtproto.ErrUserIdInvalid},
		"a deleted account":      {user(5), birthday(1, 2, 0), mtproto.ErrUserIdInvalid},
		"a user with a birthday": {user(3), birthday(1, 2, 0), errBirthdayAlready},
		"a day that is none":     {user(2), birthday(31, 2, 0), errBirthdayInvalid},
		"no birthday":            {user(2), nil, errBirthdayInvalid},
		"no user":                {nil, birthday(1, 2, 0), mtproto.ErrUserIdInvalid},
	} {
		if _, err := c.UsersSuggestBirthday(&mtproto.TLUsersSuggestBirthday{Id: k.id, Birthday: k.b}); !errors.Is(err, k.want) {
			t.Errorf("%s: %v, want %v", name, err, k.want)
		}
	}
	if len(msg.sent) != 0 {
		t.Fatalf("%d messages sent by refused requests", len(msg.sent))
	}

	r, err := c.UsersSuggestBirthday(&mtproto.TLUsersSuggestBirthday{Id: user(2), Birthday: birthday(4, 3, 1990)})
	if err != nil || r == nil || len(msg.sent) != 1 {
		t.Fatalf("a suggestion: %v, %v, %d messages", r, err, len(msg.sent))
	}
	sent := msg.sent[0]
	if sent.GetUserId() != 1 || sent.GetAuthKeyId() != 77 || sent.GetPeerType() != mtproto.PEER_USER || sent.GetPeerId() != 2 || len(sent.GetMessage()) != 1 {
		t.Fatalf("sent %v, want one message from 1 to user 2", sent)
	}
	m := sent.GetMessage()[0].GetMessage()
	a := m.GetAction()
	if m.GetPredicateName() != mtproto.Predicate_messageService || a.GetPredicateName() != mtproto.Predicate_messageActionSuggestBirthday ||
		a.GetBirthday().GetDay() != 4 || a.GetBirthday().GetMonth() != 3 || a.GetBirthday().GetYear().GetValue() != 1990 {
		t.Errorf("the message %v, want messageActionSuggestBirthday of 4.3.1990", m)
	}
	if sent.GetMessage()[0].GetRandomId() == 0 {
		t.Error("the message has no random id")
	}

	// Without a year the date goes without one.
	if _, err = c.UsersSuggestBirthday(&mtproto.TLUsersSuggestBirthday{Id: user(2), Birthday: birthday(29, 2, 0)}); err != nil {
		t.Fatal(err)
	}
	if b := msg.sent[1].GetMessage()[0].GetMessage().GetAction().GetBirthday(); b.GetYear() != nil || b.GetDay() != 29 {
		t.Errorf("a date without a year sent as %v", b)
	}
}
