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
	"fmt"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/usernames/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/usernames/internal/svc"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"github.com/zeromicro/go-zero/core/logx"
)

// editUsers is biz user answering user.editUsernames with err.
type editUsers struct {
	user_client.UserClient
	err  error
	reqs []string
}

func (f *editUsers) UserEditUsernames(_ context.Context, in *userpb.TLUserEditUsernames) (*mtproto.Bool, error) {
	f.reqs = append(f.reqs, fmt.Sprintf("%d:%d:%s:%v:%v", in.UserId, in.Action, in.Username, mtproto.FromBool(in.Active), in.Order))
	if f.err != nil {
		return nil, f.err
	}
	return mtproto.BoolTrue, nil
}

func editCore(users *editUsers, bot bool) *UsernamesCore {
	ctx := context.Background()
	return &UsernamesCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Dao: &dao.Dao{UserClient: users}},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: 7, IsBot: bot},
	}
}

// account.toggleUsername and account.reorderUsernames ask biz user for the
// calling account, with the action of each, and answer with what biz
// answers, its errors included; a bot calling them is BOT_METHOD_INVALID
// and reaches no biz.
func TestAccountUsernames(t *testing.T) {
	users := &editUsers{}
	c := editCore(users, false)
	if r, err := c.AccountToggleUsername(&mtproto.TLAccountToggleUsername{Username: "alice_two", Active: mtproto.BoolTrue}); err != nil || !mtproto.FromBool(r) {
		t.Errorf("toggle: %v, %v", r, err)
	}
	if r, err := c.AccountToggleUsername(&mtproto.TLAccountToggleUsername{Username: "alice_two"}); err != nil || !mtproto.FromBool(r) {
		t.Errorf("toggle without active: %v, %v", r, err)
	}
	if r, err := c.AccountReorderUsernames(&mtproto.TLAccountReorderUsernames{Order: []string{"b", "a"}}); err != nil || !mtproto.FromBool(r) {
		t.Errorf("reorder: %v, %v", r, err)
	}
	want := fmt.Sprint([]string{
		fmt.Sprintf("7:%d:alice_two:true:[]", userpb.UsernamesToggle),
		fmt.Sprintf("7:%d:alice_two:false:[]", userpb.UsernamesToggle),
		fmt.Sprintf("7:%d::false:[b a]", userpb.UsernamesReorder),
	})
	if got := fmt.Sprint(users.reqs); got != want {
		t.Errorf("biz asked for %s, want %s", got, want)
	}

	for _, e := range []error{mtproto.ErrUsernameInvalid, mtproto.ErrUsernameNotModified, mtproto.ErrUsernamesActiveTooMuch, mtproto.ErrOrderInvalid} {
		users.err = e
		if _, err := c.AccountToggleUsername(&mtproto.TLAccountToggleUsername{Username: "x", Active: mtproto.BoolFalse}); !errors.Is(err, e) {
			t.Errorf("toggle with biz answering %v: %v", e, err)
		}
		if _, err := c.AccountReorderUsernames(&mtproto.TLAccountReorderUsernames{Order: []string{"x"}}); !errors.Is(err, e) {
			t.Errorf("reorder with biz answering %v: %v", e, err)
		}
	}

	bots := &editUsers{}
	b := editCore(bots, true)
	if _, err := b.AccountToggleUsername(&mtproto.TLAccountToggleUsername{Username: "x", Active: mtproto.BoolTrue}); !errors.Is(err, mtproto.ErrBotMethodInvalid) {
		t.Errorf("a bot toggles: %v, want BOT_METHOD_INVALID", err)
	}
	if _, err := b.AccountReorderUsernames(&mtproto.TLAccountReorderUsernames{Order: []string{"x"}}); !errors.Is(err, mtproto.ErrBotMethodInvalid) {
		t.Errorf("a bot reorders: %v, want BOT_METHOD_INVALID", err)
	}
	if len(bots.reqs) != 0 {
		t.Errorf("a bot reached biz: %v", bots.reqs)
	}
}
