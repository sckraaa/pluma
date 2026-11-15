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
	"github.com/teamgram/teamgram-server/app/bff/webbrowser/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/webbrowser/internal/svc"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/pkg/webbrowser"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/status"
)

const (
	testUser    = 77
	testAuthKey = 7001
)

// pushes is sync that keeps what it was asked to send.
type pushes struct {
	sync_client.SyncClient
	notMe []*sync.TLSyncUpdatesNotMe
	all   []*sync.TLSyncPushUpdates
	err   error
}

func (p *pushes) SyncUpdatesNotMe(_ context.Context, in *sync.TLSyncUpdatesNotMe) (*mtproto.Void, error) {
	p.notMe = append(p.notMe, in)
	return mtproto.EmptyVoid, p.err
}

func (p *pushes) SyncPushUpdates(_ context.Context, in *sync.TLSyncPushUpdates) (*mtproto.Void, error) {
	p.all = append(p.all, in)
	return mtproto.EmptyVoid, p.err
}

func (p *pushes) reset() { p.notMe, p.all = nil, nil }

func testCore(store webbrowser.Store, sync *pushes, bot bool) *WebBrowserCore {
	ctx := context.Background()
	return &WebBrowserCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Dao: &dao.Dao{SyncClient: sync}, Store: store},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: testUser, PermAuthKeyId: testAuthKey, IsBot: bot},
	}
}

func wantRPCError(t *testing.T, what string, err error, want string) {
	t.Helper()
	if s, ok := status.FromError(err); !ok || s.Message() != want {
		t.Fatalf("%s: %v, want %s", what, err, want)
	}
}

func toggle(c *WebBrowserCore, del bool, external *mtproto.Bool, url string) (*mtproto.Updates, error) {
	return c.AccountToggleWebBrowserSettingsException60ED4229(&mtproto.TLAccountToggleWebBrowserSettingsException60ED4229{
		Delete:              del,
		OpenExternalBrowser: external,
		Url:                 url,
	})
}

// exceptionUpdateOf checks that updates has one updateWebBrowserException
// and returns it.
func exceptionUpdateOf(t *testing.T, updates *mtproto.Updates) *mtproto.Update {
	t.Helper()
	if updates.GetPredicateName() != mtproto.Predicate_updates || len(updates.GetUpdates()) != 1 || updates.GetSeq() != 0 {
		t.Fatalf("updates %v, want updates with one update and no seq", updates)
	}
	u := updates.GetUpdates()[0]
	if u.GetPredicateName() != mtproto.Predicate_updateWebBrowserException {
		t.Fatalf("update %s, want updateWebBrowserException", u.GetPredicateName())
	}
	return u
}

func TestGetWebBrowserSettings(t *testing.T) {
	store, sync := webbrowser.NewMemStore(), &pushes{}
	c := testCore(store, sync, false)

	r, err := c.AccountGetWebBrowserSettings(&mtproto.TLAccountGetWebBrowserSettings{Hash: 0})
	if err != nil {
		t.Fatal(err)
	}
	if r.GetPredicateName() != mtproto.Predicate_account_webBrowserSettings || r.GetOpenExternalBrowser() || !r.GetDisplayCloseButton() || r.GetHash() == 0 {
		t.Fatalf("settings of a new account %v, want the defaults with a hash", r)
	}
	if len(r.GetExternalExceptions())+len(r.GetInappExceptions()) != 0 {
		t.Fatalf("exceptions of a new account %v", r)
	}

	same, err := c.AccountGetWebBrowserSettings(&mtproto.TLAccountGetWebBrowserSettings{Hash: r.GetHash()})
	if err != nil {
		t.Fatal(err)
	}
	if same.GetPredicateName() != mtproto.Predicate_account_webBrowserSettingsNotModified {
		t.Errorf("the same hash: %s, want account.webBrowserSettingsNotModified", same.GetPredicateName())
	}

	// A change changes the hash: the old one gets the settings.
	if _, err = toggle(c, false, mtproto.BoolTrue, "https://Example.com/x"); err != nil {
		t.Fatal(err)
	}
	next, err := c.AccountGetWebBrowserSettings(&mtproto.TLAccountGetWebBrowserSettings{Hash: r.GetHash()})
	if err != nil {
		t.Fatal(err)
	}
	if next.GetPredicateName() != mtproto.Predicate_account_webBrowserSettings || next.GetHash() == r.GetHash() {
		t.Fatalf("after a change: %v", next)
	}
	ex := next.GetExternalExceptions()
	if len(ex) != 1 || ex[0].GetDomain() != "example.com" || ex[0].GetUrl() != "https://Example.com/x" || ex[0].GetTitle() != "example.com" || ex[0].GetFavicon() != nil {
		t.Errorf("exceptions %v", ex)
	}
}

func TestUpdateWebBrowserSettings(t *testing.T) {
	store, sync := webbrowser.NewMemStore(), &pushes{}
	c := testCore(store, sync, false)
	r, err := c.AccountUpdateWebBrowserSettings(&mtproto.TLAccountUpdateWebBrowserSettings{OpenExternalBrowser: true, DisplayCloseButton: false})
	if err != nil {
		t.Fatal(err)
	}
	if r.GetPredicateName() != mtproto.Predicate_account_webBrowserSettings || !r.GetOpenExternalBrowser() || r.GetDisplayCloseButton() {
		t.Fatalf("answer %v", r)
	}
	if len(sync.notMe) != 1 || len(sync.all) != 0 {
		t.Fatalf("pushes: %d to the others, %d to all", len(sync.notMe), len(sync.all))
	}
	p := sync.notMe[0]
	if p.GetUserId() != testUser || p.GetPermAuthKeyId() != testAuthKey || len(p.GetUpdates().GetUpdates()) != 1 {
		t.Fatalf("push %v", p)
	}
	u := p.GetUpdates().GetUpdates()[0]
	if u.GetPredicateName() != mtproto.Predicate_updateWebBrowserSettings || !u.GetOpenExternalBrowser_FLAGBOOLEAN() || u.GetDisplayCloseButton() {
		t.Errorf("update %v", u)
	}

	// A push that fails does not fail the change, which is kept.
	sync.err = errors.New("sync down")
	if _, err = c.AccountUpdateWebBrowserSettings(&mtproto.TLAccountUpdateWebBrowserSettings{DisplayCloseButton: true}); err != nil {
		t.Fatalf("with sync down: %v", err)
	}
	if s, _ := store.Get(context.Background(), testUser); s.OpenExternal || !s.DisplayCloseButton {
		t.Errorf("kept %+v", s)
	}
}

func TestToggleWebBrowserSettingsException(t *testing.T) {
	store, sync := webbrowser.NewMemStore(), &pushes{}
	c := testCore(store, sync, false)
	ctx := context.Background()

	// Into the external list: the answer and the other devices get the
	// same update.
	r, err := toggle(c, false, mtproto.BoolTrue, "https://a.com/page")
	if err != nil {
		t.Fatal(err)
	}
	u := exceptionUpdateOf(t, r)
	if u.GetDelete() || !mtproto.FromBool(u.GetOpenExternalBrowser_FLAGBOOL()) || u.GetException_WEBDOMAINEXCEPTION().GetDomain() != "a.com" ||
		u.GetException_WEBDOMAINEXCEPTION().GetUrl() != "https://a.com/page" {
		t.Errorf("update %v", u)
	}
	if len(sync.notMe) != 1 || exceptionUpdateOf(t, sync.notMe[0].GetUpdates()).GetException_WEBDOMAINEXCEPTION().GetDomain() != "a.com" {
		t.Fatalf("pushes to the others %v", sync.notMe)
	}

	// Into the other list: it moves.
	if _, err = toggle(c, false, mtproto.BoolFalse, "a.com"); err != nil {
		t.Fatal(err)
	}
	s, _ := store.Get(ctx, testUser)
	if len(s.External) != 0 || len(s.InApp) != 1 || s.InApp[0].Domain != "a.com" {
		t.Fatalf("after the move %+v", s)
	}

	// Removed from either list, by its address or its domain; again: the
	// same answer.
	for _, url := range []string{"https://A.com/other", "a.com"} {
		r, err = toggle(c, true, nil, url)
		if err != nil {
			t.Fatal(err)
		}
		u = exceptionUpdateOf(t, r)
		if !u.GetDelete() || u.GetOpenExternalBrowser_FLAGBOOL() != nil || u.GetException_WEBDOMAINEXCEPTION().GetDomain() != "a.com" {
			t.Errorf("delete update %v", u)
		}
	}
	if s, _ = store.Get(ctx, testUser); len(s.External)+len(s.InApp) != 0 {
		t.Fatalf("after delete %+v", s)
	}

	// Removed from one list only.
	for _, d := range []string{"x.com", "y.com"} {
		if _, err = toggle(c, false, mtproto.BoolTrue, d); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = toggle(c, true, mtproto.BoolFalse, "x.com"); err != nil {
		t.Fatal(err)
	}
	if s, _ = store.Get(ctx, testUser); len(s.External) != 2 {
		t.Fatalf("delete from the other list removed it: %+v", s)
	}
	r, err = toggle(c, true, mtproto.BoolTrue, "x.com")
	if err != nil {
		t.Fatal(err)
	}
	if u = exceptionUpdateOf(t, r); !u.GetDelete() || !mtproto.FromBool(u.GetOpenExternalBrowser_FLAGBOOL()) {
		t.Errorf("delete from a list: update %v", u)
	}
	if s, _ = store.Get(ctx, testUser); len(s.External) != 1 || s.External[0].Domain != "y.com" {
		t.Fatalf("after delete from its list %+v", s)
	}

	// Neither flag, a bad address, a full list.
	sync.reset()
	_, err = toggle(c, false, nil, "z.com")
	wantRPCError(t, "no flag", err, "INPUT_REQUEST_INVALID")
	_, err = toggle(c, false, mtproto.BoolTrue, "https://")
	wantRPCError(t, "no host", err, "URL_INVALID")
	for i := range webbrowser.MaxExceptions - 1 {
		if _, err = toggle(c, false, mtproto.BoolTrue, fmt.Sprintf("d%d.com", i)); err != nil {
			t.Fatal(err)
		}
	}
	sync.reset()
	_, err = toggle(c, false, mtproto.BoolTrue, "over.com")
	wantRPCError(t, "full list", err, "LIMIT_INVALID")
	if len(sync.notMe) != 0 {
		t.Errorf("a refused change was pushed: %v", sync.notMe)
	}
}

// The older constructor answers boolTrue and pushes the update to every
// device, the caller's too: its answer does not carry it.
func TestToggleWebBrowserSettingsExceptionOld(t *testing.T) {
	store, sync := webbrowser.NewMemStore(), &pushes{}
	c := testCore(store, sync, false)
	r, err := c.AccountToggleWebBrowserSettingsException2D0A0571(&mtproto.TLAccountToggleWebBrowserSettingsException2D0A0571{
		OpenExternalBrowser: mtproto.BoolFalse,
		Url:                 "https://b.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !mtproto.FromBool(r) {
		t.Errorf("answer %v, want boolTrue", r)
	}
	if len(sync.all) != 1 || len(sync.notMe) != 0 || sync.all[0].GetUserId() != testUser {
		t.Fatalf("pushes: %d to all, %d to the others", len(sync.all), len(sync.notMe))
	}
	if u := exceptionUpdateOf(t, sync.all[0].GetUpdates()); u.GetException_WEBDOMAINEXCEPTION().GetDomain() != "b.com" || mtproto.FromBool(u.GetOpenExternalBrowser_FLAGBOOL()) {
		t.Errorf("update %v", u)
	}
	if s, _ := store.Get(context.Background(), testUser); len(s.InApp) != 1 {
		t.Errorf("kept %+v", s)
	}
}

func TestDeleteWebBrowserSettingsExceptions(t *testing.T) {
	store, sync := webbrowser.NewMemStore(), &pushes{}
	c := testCore(store, sync, false)
	ctx := context.Background()
	if err := store.SetFlags(ctx, testUser, true, false); err != nil {
		t.Fatal(err)
	}
	for _, e := range []struct {
		url      string
		external bool
	}{{"a.com", true}, {"b.com", false}, {"c.com", true}} {
		if err := store.PutException(ctx, testUser, webbrowser.Exception{Domain: e.url, URL: e.url}, e.external); err != nil {
			t.Fatal(err)
		}
	}
	r, err := c.AccountDeleteWebBrowserSettingsExceptions(&mtproto.TLAccountDeleteWebBrowserSettingsExceptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !r.GetOpenExternalBrowser() || r.GetDisplayCloseButton() || len(r.GetExternalExceptions())+len(r.GetInappExceptions()) != 0 {
		t.Fatalf("answer %v, want the flags and no exceptions", r)
	}
	if len(sync.notMe) != 1 {
		t.Fatalf("%d pushes, want one", len(sync.notMe))
	}
	var got []string
	for _, u := range sync.notMe[0].GetUpdates().GetUpdates() {
		if u.GetPredicateName() != mtproto.Predicate_updateWebBrowserException || !u.GetDelete() {
			t.Fatalf("update %v", u)
		}
		got = append(got, fmt.Sprintf("%s:%v", u.GetException_WEBDOMAINEXCEPTION().GetDomain(), mtproto.FromBool(u.GetOpenExternalBrowser_FLAGBOOL())))
	}
	if fmt.Sprint(got) != "[a.com:true c.com:true b.com:false]" {
		t.Errorf("updates %v", got)
	}

	// Nothing to remove: nothing pushed.
	sync.reset()
	if _, err = c.AccountDeleteWebBrowserSettingsExceptions(&mtproto.TLAccountDeleteWebBrowserSettingsExceptions{}); err != nil {
		t.Fatal(err)
	}
	if len(sync.notMe) != 0 {
		t.Errorf("pushed %v", sync.notMe)
	}
}

func TestWebBrowserSettingsNotForBots(t *testing.T) {
	c := testCore(webbrowser.NewMemStore(), &pushes{}, true)
	_, err := c.AccountGetWebBrowserSettings(&mtproto.TLAccountGetWebBrowserSettings{})
	wantRPCError(t, "get", err, "BOT_METHOD_INVALID")
	_, err = c.AccountUpdateWebBrowserSettings(&mtproto.TLAccountUpdateWebBrowserSettings{})
	wantRPCError(t, "update", err, "BOT_METHOD_INVALID")
	_, err = toggle(c, false, mtproto.BoolTrue, "a.com")
	wantRPCError(t, "toggle", err, "BOT_METHOD_INVALID")
	_, err = c.AccountToggleWebBrowserSettingsException2D0A0571(&mtproto.TLAccountToggleWebBrowserSettingsException2D0A0571{OpenExternalBrowser: mtproto.BoolTrue, Url: "a.com"})
	wantRPCError(t, "toggle#2d0a0571", err, "BOT_METHOD_INVALID")
	_, err = c.AccountDeleteWebBrowserSettingsExceptions(&mtproto.TLAccountDeleteWebBrowserSettingsExceptions{})
	wantRPCError(t, "delete all", err, "BOT_METHOD_INVALID")
}
