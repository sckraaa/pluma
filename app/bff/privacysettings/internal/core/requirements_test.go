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
	"github.com/teamgram/teamgram-server/app/bff/privacysettings/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/privacysettings/internal/svc"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/status"
)

type fakeUsers struct {
	user_client.UserClient
	ids map[int64]bool
}

func (f *fakeUsers) UserGetMutableUsers(_ context.Context, in *userpb.TLUserGetMutableUsers) (*userpb.Vector_ImmutableUser, error) {
	out := &userpb.Vector_ImmutableUser{}
	for _, id := range in.Id {
		if f.ids[id] {
			out.Datas = append(out.Datas, mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{
				User: mtproto.MakeTLUserData(&mtproto.UserData{Id: id}).To_UserData(),
			}).To_ImmutableUser())
		}
	}
	return out, nil
}

func newCore() *PrivacySettingsCore {
	ctx := context.Background()
	return &PrivacySettingsCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Dao: &dao.Dao{UserClient: &fakeUsers{ids: map[int64]bool{1: true, 2: true, 3: true}}}},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: 1},
	}
}

func users(ids ...int64) []*mtproto.InputUser {
	out := make([]*mtproto.InputUser, 0, len(ids))
	for _, id := range ids {
		out = append(out, mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: id}).To_InputUser())
	}
	return out
}

// Pluma has neither Premium nor paid messages: nobody requires anything to
// be written to, one answer per user at its offset.
func TestRequirementsToContact(t *testing.T) {
	c := newCore()
	in := append(users(2, 3), mtproto.MakeTLInputUserSelf(nil).To_InputUser())

	premium, err := c.UsersGetIsPremiumRequiredToContact(&mtproto.TLUsersGetIsPremiumRequiredToContact{Id: in})
	if err != nil || len(premium.GetDatas()) != 3 {
		t.Fatalf("%v, %v", premium, err)
	}
	for _, b := range premium.GetDatas() {
		if mtproto.FromBool(b) {
			t.Errorf("premium required: %v", premium)
		}
	}

	req, err := c.UsersGetRequirementsToContact(&mtproto.TLUsersGetRequirementsToContact{Id: in})
	if err != nil || len(req.GetDatas()) != 3 {
		t.Fatalf("%v, %v", req, err)
	}
	for _, r := range req.GetDatas() {
		if r.GetPredicateName() != mtproto.Predicate_requirementToContactEmpty {
			t.Errorf("requirement %v", r)
		}
	}
}

func TestRequirementsToContactOfUnknownUsers(t *testing.T) {
	c := newCore()
	for _, in := range [][]*mtproto.InputUser{users(2, 99), {mtproto.MakeTLInputUserEmpty(nil).To_InputUser()}} {
		_, err := c.UsersGetRequirementsToContact(&mtproto.TLUsersGetRequirementsToContact{Id: in})
		if st, _ := status.FromError(err); st.Message() != "USER_ID_INVALID" {
			t.Errorf("%v: %v", in, err)
		}
		_, err = c.UsersGetIsPremiumRequiredToContact(&mtproto.TLUsersGetIsPremiumRequiredToContact{Id: in})
		if st, _ := status.FromError(err); st.Message() != "USER_ID_INVALID" {
			t.Errorf("%v: %v", in, err)
		}
	}
}
