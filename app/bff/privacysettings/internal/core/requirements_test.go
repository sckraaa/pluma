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
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/privacysettings/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/privacysettings/internal/svc"
	dialog_client "github.com/teamgram/teamgram-server/app/service/biz/dialog/client"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/status"
)

// testUser is a user of the fakes: Premium, the setting that only
// contacts, chats there already and Premium users may write, contacts.
type testUser struct {
	premium, requires bool
	contacts          []int64
}

type fakeUsers struct {
	user_client.UserClient
	users    map[int64]testUser
	settings *mtproto.GlobalPrivacySettings
}

func (f *fakeUsers) immutable(id int64) *mtproto.ImmutableUser {
	u := f.users[id]
	m := mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{
		User: mtproto.MakeTLUserData(&mtproto.UserData{
			Id:                               id,
			Premium:                          u.premium,
			NewNoncontactPeersRequirePremium: u.requires,
		}).To_UserData(),
	}).To_ImmutableUser()
	for _, c := range u.contacts {
		m.Contacts = append(m.Contacts, &mtproto.ContactData{UserId: id, ContactUserId: c})
	}
	return m
}

func (f *fakeUsers) UserGetMutableUsers(_ context.Context, in *userpb.TLUserGetMutableUsers) (*userpb.Vector_ImmutableUser, error) {
	out := &userpb.Vector_ImmutableUser{}
	for _, id := range in.Id {
		if _, ok := f.users[id]; ok {
			out.Datas = append(out.Datas, f.immutable(id))
		}
	}
	return out, nil
}

func (f *fakeUsers) UserGetImmutableUser(_ context.Context, in *userpb.TLUserGetImmutableUser) (*mtproto.ImmutableUser, error) {
	return f.immutable(in.Id), nil
}

func (f *fakeUsers) UserGetGlobalPrivacySettings(context.Context, *userpb.TLUserGetGlobalPrivacySettings) (*mtproto.GlobalPrivacySettings, error) {
	return f.settings, nil
}

func (f *fakeUsers) UserSetGlobalPrivacySettings(_ context.Context, in *userpb.TLUserSetGlobalPrivacySettings) (*mtproto.Bool, error) {
	f.settings = in.Settings
	return mtproto.BoolTrue, nil
}

// fakeDialogs has the private chats in chats: user -> peers.
type fakeDialogs struct {
	dialog_client.DialogClient
	chats map[int64][]int64
	asked int
}

func (f *fakeDialogs) DialogGetDialogById(_ context.Context, in *dialog.TLDialogGetDialogById) (*dialog.DialogExt, error) {
	f.asked++
	if in.PeerType == mtproto.PEER_USER && slices.Contains(f.chats[in.UserId], in.PeerId) {
		return &dialog.DialogExt{}, nil
	}
	return nil, mtproto.ErrPeerIdInvalid
}

// The requester is 1, without Premium. 2 has no setting; 3, 4 and 5 let
// only contacts, chats there already and Premium users write: 3 has 1 in
// its contacts, 4 a chat with 1, 5 neither; 6 has the setting but no
// Premium, so it does not work.
func newCore() (*PrivacySettingsCore, *fakeUsers, *fakeDialogs) {
	ctx := context.Background()
	users := &fakeUsers{users: map[int64]testUser{
		1: {},
		2: {premium: true},
		3: {premium: true, requires: true, contacts: []int64{1}},
		4: {premium: true, requires: true},
		5: {premium: true, requires: true},
		6: {requires: true},
	}}
	dialogs := &fakeDialogs{chats: map[int64][]int64{4: {1}}}
	return &PrivacySettingsCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Dao: &dao.Dao{UserClient: users, DialogClient: dialogs}},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: 1},
	}, users, dialogs
}

func inputUsers(ids ...int64) []*mtproto.InputUser {
	out := make([]*mtproto.InputUser, 0, len(ids))
	for _, id := range ids {
		out = append(out, mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: id}).To_InputUser())
	}
	return out
}

// One answer per user at its offset; only 5 needs Premium.
func TestRequirementsToContact(t *testing.T) {
	c, users, dialogs := newCore()
	in := append(inputUsers(2, 3, 4, 5, 6), mtproto.MakeTLInputUserSelf(nil).To_InputUser())
	want := []bool{false, false, false, true, false, false}

	premium, err := c.UsersGetIsPremiumRequiredToContact(&mtproto.TLUsersGetIsPremiumRequiredToContact{Id: in})
	if err != nil || len(premium.GetDatas()) != len(want) {
		t.Fatalf("%v, %v", premium, err)
	}
	for i, b := range premium.GetDatas() {
		if mtproto.FromBool(b) != want[i] {
			t.Errorf("premium required for %d: %v, want %v", i, mtproto.FromBool(b), want[i])
		}
	}
	// The chats are asked about only where the rest does not decide: 4, 5.
	if dialogs.asked != 2 {
		t.Errorf("%d chats asked about, want 2", dialogs.asked)
	}

	req, err := c.UsersGetRequirementsToContact(&mtproto.TLUsersGetRequirementsToContact{Id: in})
	if err != nil || len(req.GetDatas()) != len(want) {
		t.Fatalf("%v, %v", req, err)
	}
	for i, r := range req.GetDatas() {
		got := r.GetPredicateName() == mtproto.Predicate_requirementToContactPremium
		if got != want[i] || (!got && r.GetPredicateName() != mtproto.Predicate_requirementToContactEmpty) {
			t.Errorf("requirement for %d: %v", i, r)
		}
	}

	// A requester with Premium needs nothing.
	users.users[1] = testUser{premium: true}
	premium, err = c.UsersGetIsPremiumRequiredToContact(&mtproto.TLUsersGetIsPremiumRequiredToContact{Id: in})
	if err != nil || slices.ContainsFunc(premium.GetDatas(), mtproto.FromBool) {
		t.Fatalf("with Premium: %v, %v", premium, err)
	}
}

func TestRequirementsToContactOfUnknownUsers(t *testing.T) {
	c, _, _ := newCore()
	for _, in := range [][]*mtproto.InputUser{inputUsers(2, 99), {mtproto.MakeTLInputUserEmpty(nil).To_InputUser()}} {
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

// Only a user with Premium turns the setting on, and it shows on while the
// user has Premium.
func TestGlobalPrivacySettingsRequirePremium(t *testing.T) {
	c, users, _ := newCore()
	on := mtproto.MakeTLGlobalPrivacySettings(&mtproto.GlobalPrivacySettings{NewNoncontactPeersRequirePremium: true}).To_GlobalPrivacySettings()
	_, err := c.AccountSetGlobalPrivacySettings(&mtproto.TLAccountSetGlobalPrivacySettings{Settings: on})
	if st, _ := status.FromError(err); st.Message() != "PREMIUM_ACCOUNT_REQUIRED" {
		t.Fatalf("without Premium: %v", err)
	}
	if users.settings != nil {
		t.Fatal("stored without Premium")
	}

	users.users[1] = testUser{premium: true}
	if _, err = c.AccountSetGlobalPrivacySettings(&mtproto.TLAccountSetGlobalPrivacySettings{Settings: on}); err != nil {
		t.Fatal(err)
	}
	got, err := c.AccountGetGlobalPrivacySettings(&mtproto.TLAccountGetGlobalPrivacySettings{})
	if err != nil || !got.GetNewNoncontactPeersRequirePremium() {
		t.Fatalf("with Premium: %v, %v", got, err)
	}

	users.users[1] = testUser{}
	got, err = c.AccountGetGlobalPrivacySettings(&mtproto.TLAccountGetGlobalPrivacySettings{})
	if err != nil || got.GetNewNoncontactPeersRequirePremium() {
		t.Fatalf("after Premium ended: %v, %v", got, err)
	}
}
