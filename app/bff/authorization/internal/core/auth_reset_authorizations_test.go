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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/authorization/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/authorization/internal/svc"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	authsession_client "github.com/teamgram/teamgram-server/app/service/authsession/client"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	resetUser    = 42
	resetPermKey = 1001
)

// fakeAuthorizations: the authorizations of resetUser, the current one
// created at currentCreated; other keys are resetKeys.
type fakeAuthorizations struct {
	authsession_client.AuthsessionClient
	currentCreated int64
	resetKeys      []int64
	resets         []*authsession.TLAuthsessionResetAuthorization
	drops          []*authsession.TLAuthsessionDropTempAuthKeys
}

func (f *fakeAuthorizations) AuthsessionGetAuthorizations(context.Context, *authsession.TLAuthsessionGetAuthorizations) (*mtproto.Account_Authorizations, error) {
	return mtproto.MakeTLAccountAuthorizations(&mtproto.Account_Authorizations{
		Authorizations: []*mtproto.Authorization{
			mtproto.MakeTLAuthorization(&mtproto.Authorization{Current: true, DateCreated: int32(f.currentCreated)}).To_Authorization(),
			mtproto.MakeTLAuthorization(&mtproto.Authorization{Hash: 7, DateCreated: 1}).To_Authorization(),
		},
	}).To_Account_Authorizations(), nil
}

func (f *fakeAuthorizations) AuthsessionResetAuthorization(_ context.Context, in *authsession.TLAuthsessionResetAuthorization) (*authsession.Vector_Long, error) {
	f.resets = append(f.resets, in)
	return &authsession.Vector_Long{Datas: f.resetKeys}, nil
}

func (f *fakeAuthorizations) AuthsessionDropTempAuthKeys(_ context.Context, in *authsession.TLAuthsessionDropTempAuthKeys) (*authsession.Vector_Long, error) {
	f.drops = append(f.drops, in)
	return &authsession.Vector_Long{}, nil
}

type fakeSyncMe struct {
	sync_client.SyncClient
	pushed []*sync.TLSyncUpdatesMe
}

func (f *fakeSyncMe) SyncUpdatesMe(_ context.Context, in *sync.TLSyncUpdatesMe) (*mtproto.Void, error) {
	f.pushed = append(f.pushed, in)
	return mtproto.EmptyVoid, nil
}

func resetCore(auths *fakeAuthorizations, push *fakeSyncMe) *AuthorizationCore {
	ctx := context.Background()
	return &AuthorizationCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Dao: &dao.Dao{AuthsessionClient: auths, SyncClient: push}},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: resetUser, PermAuthKeyId: resetPermKey},
	}
}

func TestAuthResetAuthorizations(t *testing.T) {
	now := time.Now().Unix()

	// A session younger than a day may not terminate the others.
	fresh := &fakeAuthorizations{currentCreated: now - 3600, resetKeys: []int64{2002}}
	if _, err := resetCore(fresh, &fakeSyncMe{}).AuthResetAuthorizations(&mtproto.TLAuthResetAuthorizations{}); rpcErrorMessage(err) != "FRESH_RESET_AUTHORISATION_FORBIDDEN" {
		t.Fatalf("fresh session: %v, want FRESH_RESET_AUTHORISATION_FORBIDDEN", err)
	}
	if len(fresh.resets) != 0 {
		t.Fatal("a fresh session terminated others")
	}

	old := &fakeAuthorizations{currentCreated: now - 2*86400, resetKeys: []int64{2002, 3003}}
	push := &fakeSyncMe{}
	if _, err := resetCore(old, push).AuthResetAuthorizations(&mtproto.TLAuthResetAuthorizations{}); err != nil {
		t.Fatal(err)
	}
	if len(old.resets) != 1 || old.resets[0].Hash != 0 || old.resets[0].AuthKeyId != resetPermKey || old.resets[0].UserId != resetUser {
		t.Fatalf("reset asked: %+v", old.resets)
	}
	var told []int64
	for _, p := range push.pushed {
		if p.GetUpdates().GetPredicateName() == mtproto.Predicate_updateAccountResetAuthorization {
			told = append(told, p.GetPermAuthKeyId())
		}
	}
	if !slices.Equal(told, []int64{2002, 3003}) {
		t.Fatalf("terminated sessions told: %v", told)
	}
}

func TestAuthDropTempAuthKeys(t *testing.T) {
	auths := &fakeAuthorizations{}
	if _, err := resetCore(auths, &fakeSyncMe{}).AuthDropTempAuthKeys(&mtproto.TLAuthDropTempAuthKeys{ExceptAuthKeys: []int64{5}}); err != nil {
		t.Fatal(err)
	}
	if len(auths.drops) != 1 || auths.drops[0].PermAuthKeyId != resetPermKey || !slices.Equal(auths.drops[0].ExceptAuthKeys, []int64{5}) {
		t.Fatalf("drop asked: %+v", auths.drops)
	}

	noPerm := resetCore(auths, &fakeSyncMe{})
	noPerm.MD.PermAuthKeyId = 0
	if _, err := noPerm.AuthDropTempAuthKeys(&mtproto.TLAuthDropTempAuthKeys{}); rpcErrorMessage(err) != "AUTH_KEY_PERM_EMPTY" {
		t.Fatalf("without a permanent key: %v", err)
	}
}
