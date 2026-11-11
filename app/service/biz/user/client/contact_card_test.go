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

package userclient

import (
	"context"
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// phoneBook answers the two calls ContactCardUserId makes.
type phoneBook struct {
	UserClient
	byPhone map[string]int64
	ghosts  map[int64]bool
	fail    bool
}

func (p *phoneBook) UserGetUserIdByPhone(_ context.Context, in *user.TLUserGetUserIdByPhone) (*mtproto.Int64, error) {
	id, ok := p.byPhone[in.GetPhone()]
	if !ok {
		return nil, mtproto.ErrPhoneNotOccupied
	}
	return &mtproto.Int64{V: id}, nil
}

func (p *phoneBook) UserGetImmutableUser(_ context.Context, in *user.TLUserGetImmutableUser) (*mtproto.ImmutableUser, error) {
	if p.fail {
		return nil, errors.New("user service is down")
	}
	return mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{
		User: mtproto.MakeTLUserData(&mtproto.UserData{Id: in.GetId(), Ghost: p.ghosts[in.GetId()]}).To_UserData(),
	}).To_ImmutableUser(), nil
}

func TestContactCardUserId(t *testing.T) {
	const (
		sender  = int64(10)
		other   = int64(20)
		support = int64(30)
	)
	book := &phoneBook{
		byPhone: map[string]int64{"100": sender, "200": other, "300": support},
		ghosts:  map[int64]bool{support: true},
	}
	cases := []struct {
		name   string
		sender int64
		phone  string
		want   int64
	}{
		{"an ordinary account", sender, "200", other},
		{"a number without an account", sender, "999", 0},
		{"the ghost's number to another", sender, "300", 0},
		{"the ghost's own card", support, "300", support},
		{"one's own card", sender, "100", sender},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ContactCardUserId(context.Background(), book, tc.sender, tc.phone)
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("user id = %d, want %d", got, tc.want)
			}
		})
	}

	book.fail = true
	if _, err := ContactCardUserId(context.Background(), book, sender, "300"); err == nil {
		t.Fatal("a failed check of the owner linked the card")
	}
}
