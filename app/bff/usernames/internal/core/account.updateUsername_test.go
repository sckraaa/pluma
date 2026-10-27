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
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/usernames/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/usernames/internal/svc"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"github.com/zeromicro/go-zero/core/logx"
)

// fakeUsers is biz user with the account's username as stored and as the
// cache has it, which may be stale.
type fakeUsers struct {
	user_client.UserClient
	stored, cached string
	updates        []string
}

func (f *fakeUsers) UserGetImmutableUser(_ context.Context, in *userpb.TLUserGetImmutableUser) (*mtproto.ImmutableUser, error) {
	return mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{
		User: mtproto.MakeTLUserData(&mtproto.UserData{Id: in.Id, AccessHash: 1, FirstName: "Me", Username: f.cached}).To_UserData(),
	}).To_ImmutableUser(), nil
}

// UserUpdateUsername answers whether the username changed, as biz does.
func (f *fakeUsers) UserUpdateUsername(_ context.Context, in *userpb.TLUserUpdateUsername) (*mtproto.Bool, error) {
	f.updates = append(f.updates, in.Username)
	changed := in.Username != f.stored
	f.stored, f.cached = in.Username, in.Username
	return mtproto.ToBool(changed), nil
}

// account.updateUsername with the username the account has is
// USERNAME_NOT_MODIFIED, as in Telegram, also when only biz sees it.
func TestUpdateUsernameNotModified(t *testing.T) {
	users := &fakeUsers{stored: "alice", cached: "alice"}
	ctx := context.Background()
	c := &UsernamesCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Dao: &dao.Dao{UserClient: users}},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: 1},
	}
	update := func(username string) (*mtproto.User, error) {
		return c.AccountUpdateUsername(&mtproto.TLAccountUpdateUsername{Username: username})
	}

	if _, err := update("alice"); !errors.Is(err, mtproto.ErrUsernameNotModified) {
		t.Errorf("the same username: %v, want USERNAME_NOT_MODIFIED", err)
	}
	if _, err := update("al"); !errors.Is(err, mtproto.ErrUsernameInvalid) {
		t.Errorf("a short username: %v, want USERNAME_INVALID", err)
	}
	// The case of the letters is a change.
	if u, err := update("Alice"); err != nil || u.GetUsername().GetValue() != "Alice" {
		t.Errorf("another case: %v, %v", u, err)
	}
	// A stale cache: biz finds the same username.
	users.cached = "bob_old"
	if _, err := update("Alice"); !errors.Is(err, mtproto.ErrUsernameNotModified) {
		t.Errorf("the same username behind a stale cache: %v, want USERNAME_NOT_MODIFIED", err)
	}
	if want := []string{"Alice", "Alice"}; !slices.Equal(users.updates, want) {
		t.Errorf("biz asked for %v, want %v", users.updates, want)
	}
}
