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
	"github.com/teamgram/teamgram-server/app/bff/passport/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/passport/internal/svc"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	authsession_client "github.com/teamgram/teamgram-server/app/service/authsession/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	listUser    = 42
	listPermKey = 1001
)

// fakeAuthsession lists the authorizations of listUser as authsession does:
// the one on the key that asks first, current and with hash 0, here a new
// login that is still unconfirmed, then another new login.
type fakeAuthsession struct {
	authsession_client.AuthsessionClient
	asked []*authsession.TLAuthsessionGetAuthorizations
}

func (f *fakeAuthsession) AuthsessionGetAuthorizations(_ context.Context, in *authsession.TLAuthsessionGetAuthorizations) (*mtproto.Account_Authorizations, error) {
	f.asked = append(f.asked, in)
	return mtproto.MakeTLAccountAuthorizations(&mtproto.Account_Authorizations{
		Authorizations: []*mtproto.Authorization{
			mtproto.MakeTLAuthorization(&mtproto.Authorization{Current: true, Unconfirmed: true, DeviceModel: "Safari 26"}).To_Authorization(),
			mtproto.MakeTLAuthorization(&mtproto.Authorization{Hash: 7, Unconfirmed: true, DeviceModel: "Chrome 143"}).To_Authorization(),
			mtproto.MakeTLAuthorization(&mtproto.Authorization{Hash: 8, DeviceModel: "Samsung SM-G950F"}).To_Authorization(),
		},
	}).To_Account_Authorizations(), nil
}

type fakeUser struct {
	user_client.UserClient
}

func (fakeUser) UserGetAuthorizationTTL(context.Context, *userpb.TLUserGetAuthorizationTTL) (*mtproto.AccountDaysTTL, error) {
	return mtproto.MakeTLAccountDaysTTL(&mtproto.AccountDaysTTL{Days: 180}).To_AccountDaysTTL(), nil
}

// A session never sees itself unconfirmed in its list of sessions (task
// 019): Web K would ask the user about their own login. Another new login
// stays unconfirmed, so that the session can ask about it.
func TestAccountGetAuthorizations(t *testing.T) {
	ctx := context.Background()
	auths := &fakeAuthsession{}
	c := &PassportCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Dao: &dao.Dao{AuthsessionClient: auths, UserClient: fakeUser{}}},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: listUser, PermAuthKeyId: listPermKey},
	}

	r, err := c.AccountGetAuthorizations(&mtproto.TLAccountGetAuthorizations{})
	if err != nil {
		t.Fatal(err)
	}
	if len(auths.asked) != 1 || auths.asked[0].GetUserId() != listUser || auths.asked[0].GetExcludeAuthKeyId() != listPermKey {
		t.Fatalf("authsession asked: %v", auths.asked)
	}
	if r.GetAuthorizationTtlDays() != 180 {
		t.Errorf("authorization_ttl_days %d, want 180", r.GetAuthorizationTtlDays())
	}
	list := r.GetAuthorizations()
	if len(list) != 3 {
		t.Fatalf("%d authorizations, want 3", len(list))
	}
	if !list[0].GetCurrent() || list[0].GetUnconfirmed() {
		t.Errorf("the session itself: current %v, unconfirmed %v, want current and not unconfirmed",
			list[0].GetCurrent(), list[0].GetUnconfirmed())
	}
	if !list[1].GetUnconfirmed() || list[2].GetUnconfirmed() {
		t.Errorf("the other sessions: unconfirmed %v and %v, want true and false", list[1].GetUnconfirmed(), list[2].GetUnconfirmed())
	}
}
