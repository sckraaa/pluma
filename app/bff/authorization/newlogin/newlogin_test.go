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

package newlogin

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

const (
	userId = int64(42)
	newKey = int64(7001)
	hash   = int64(555)
)

// fake is the account: the session on newKey and others in recentLang.
type fake struct {
	others     int
	recentLang string
	recentAsks int
	pushFails  bool // the push of the message fails
	pushes     []*sync.TLSyncUpdatesNotMe
	reviews    []*user.TLUserNewAuthorization
}

func (f *fake) AuthsessionGetAuthorizations(_ context.Context, in *authsession.TLAuthsessionGetAuthorizations) (*mtproto.Account_Authorizations, error) {
	list := []*mtproto.Authorization{
		mtproto.MakeTLAuthorization(&mtproto.Authorization{
			Current:       in.GetExcludeAuthKeyId() == newKey,
			Unconfirmed:   true,
			AppName:       "Pluma Android",
			AppVersion:    "12.9.1",
			DeviceModel:   "OnePlus 12",
			SystemVersion: "SDK 36",
			Country:       "Prague, Czechia",
		}).To_Authorization(),
	}
	for i := 0; i < f.others; i++ {
		list = append(list, mtproto.MakeTLAuthorization(&mtproto.Authorization{Hash: int64(i + 1)}).To_Authorization())
	}
	return mtproto.MakeTLAccountAuthorizations(&mtproto.Account_Authorizations{Authorizations: list}).To_Account_Authorizations(), nil
}

func (f *fake) AuthsessionGetRecentLangCode(_ context.Context, in *authsession.TLAuthsessionGetRecentLangCode) (*mtproto.String, error) {
	f.recentAsks++
	if in.GetExceptAuthKeyId() != newKey {
		return &mtproto.String{V: "wrong key"}, nil
	}
	return &mtproto.String{V: f.recentLang}, nil
}

func (f *fake) UserNewAuthorization(_ context.Context, in *user.TLUserNewAuthorization) (*mtproto.Bool, error) {
	f.reviews = append(f.reviews, in)
	return mtproto.BoolTrue, nil
}

func (f *fake) SyncUpdatesNotMe(_ context.Context, in *sync.TLSyncUpdatesNotMe) (*mtproto.Void, error) {
	if f.pushFails {
		return nil, errors.New("sync is down")
	}
	f.pushes = append(f.pushes, in)
	return &mtproto.Void{}, nil
}

func (f *fake) notifier() Notifier {
	return Notifier{Authsession: f, User: f, Sync: f}
}

func login(langCode string, hasPassword bool) Login {
	return Login{
		User: mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{
			User: mtproto.MakeTLUserData(&mtproto.UserData{Id: userId, FirstName: "Anna"}).To_UserData(),
		}).To_ImmutableUser(),
		PermAuthKeyId: newKey,
		Hash:          hash,
		LangCode:      langCode,
		HasPassword:   hasPassword,
		Date:          time.Date(2026, 10, 29, 16, 46, 6, 0, time.UTC),
	}
}

// message is the text of the only New login message, and that it went to
// the account without the new key.
func (f *fake) message(t *testing.T) string {
	t.Helper()
	if len(f.pushes) != 1 {
		t.Fatalf("%d pushes, want 1", len(f.pushes))
	}
	p := f.pushes[0]
	u := p.GetUpdates().GetUpdates()
	if p.GetUserId() != userId || p.GetPermAuthKeyId() != newKey || len(u) != 1 ||
		u[0].GetPredicateName() != mtproto.Predicate_updateServiceNotification || u[0].GetInboxDate().GetValue() != int32(login("", false).Date.Unix()) {
		t.Fatalf("push %v", p)
	}
	return u[0].GetMessage_STRING()
}

func TestNotify(t *testing.T) {
	f := &fake{others: 2, recentLang: "en"}
	if err := f.notifier().Notify(context.Background(), login("ru", true)); err != nil {
		t.Fatal(err)
	}
	// The language of the session that confirmed the login wins over the
	// one that connected last; the account has a password.
	text := f.message(t)
	want := "Вход с нового устройства. Anna, мы обнаружили вход в Ваш аккаунт с нового устройства 29/10/2026 в 16:46:06 UTC.\n\n" +
		"Устройство: Pluma Android, 12.9.1, OnePlus 12, SDK 36\nМесто входа: Prague, Czechia\n\n"
	if !strings.HasPrefix(text, want) || strings.Contains(text, "Two-Step") {
		t.Fatalf("message %q", text)
	}
	if f.recentAsks != 0 {
		t.Fatal("the recent language was asked for a confirmed QR login")
	}

	if len(f.reviews) != 1 {
		t.Fatalf("%d review updates", len(f.reviews))
	}
	r := f.reviews[0]
	u := r.GetUpdate()
	if r.GetUserId() != userId || r.GetExceptAuthKeyId() != newKey || u.GetPredicateName() != mtproto.Predicate_updateNewAuthorization ||
		!u.GetUnconfirmed() || u.GetHash() != hash || u.GetDate_FLAGINT32().GetValue() != int32(login("", false).Date.Unix()) ||
		u.GetDevice().GetValue() != "OnePlus 12" || u.GetLocation().GetValue() != "Prague, Czechia" {
		t.Fatalf("review update %v", r)
	}
}

func TestNotifyWithCodeInRecentLanguage(t *testing.T) {
	f := &fake{others: 1, recentLang: "en-US"}
	if err := f.notifier().Notify(context.Background(), login("", false)); err != nil {
		t.Fatal(err)
	}
	text := f.message(t)
	if !strings.HasPrefix(text, "New login. Dear Anna, we detected") ||
		!strings.HasSuffix(text, "you can enable Two-Step Verification in Privacy and Security settings.") {
		t.Fatalf("message %q", text)
	}
	if f.recentAsks != 1 {
		t.Fatalf("%d asks for the recent language", f.recentAsks)
	}
}

// A failed message does not keep the banner back.
func TestNotifyMessageFails(t *testing.T) {
	f := &fake{others: 1, pushFails: true}
	if err := f.notifier().Notify(context.Background(), login("", false)); err == nil {
		t.Fatal("a failed message was not reported")
	}
	if len(f.reviews) != 1 || !f.reviews[0].GetUpdate().GetUnconfirmed() {
		t.Fatalf("the banner went %d times", len(f.reviews))
	}
}

func TestNotifyFirstSession(t *testing.T) {
	f := &fake{}
	if err := f.notifier().Notify(context.Background(), login("", false)); err != nil {
		t.Fatal(err)
	}
	if len(f.pushes) != 0 || len(f.reviews) != 0 {
		t.Fatalf("a first session told %d pushes, %d reviews", len(f.pushes), len(f.reviews))
	}
}

func TestNotifyWithoutTheNewSession(t *testing.T) {
	f := &fake{others: 1}
	l := login("", false)
	l.PermAuthKeyId = newKey + 1 // not bound
	if err := f.notifier().Notify(context.Background(), l); err == nil {
		t.Fatal("a login without its authorization was told")
	}
	if len(f.pushes) != 0 || len(f.reviews) != 0 {
		t.Fatal("something was sent")
	}
}

func TestReviewed(t *testing.T) {
	f := &fake{}
	if err := f.notifier().Reviewed(context.Background(), userId, hash); err != nil {
		t.Fatal(err)
	}
	if len(f.reviews) != 1 {
		t.Fatalf("%d review updates", len(f.reviews))
	}
	r := f.reviews[0]
	u := r.GetUpdate()
	// Every session drops the banner: no session is left out, and the
	// update has no unconfirmed and no details.
	if r.GetUserId() != userId || r.GetExceptAuthKeyId() != 0 || u.GetHash() != hash || u.GetUnconfirmed() || u.GetDevice() != nil {
		t.Fatalf("review update %v", r)
	}
}

func TestDevice(t *testing.T) {
	a := mtproto.MakeTLAuthorization(&mtproto.Authorization{AppName: "Pluma Web", AppVersion: "12.0.48 A", DeviceModel: " Safari 26 ", SystemVersion: ""}).To_Authorization()
	if got := Device(a); got != "Pluma Web, 12.0.48 A, Safari 26" {
		t.Fatalf("Device %q", got)
	}
}
