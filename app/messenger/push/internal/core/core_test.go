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
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/push/internal/provider"
	"github.com/teamgram/teamgram-server/pkg/pushcrypt"
	"github.com/teamgram/teamgram-server/pkg/pushdevices"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	me     = int64(42)
	anna   = int64(7)
	group  = int64(100)
	keyFCM = int64(501) // Android of me
	keyIOS = int64(502) // iPhone of me
	keyWeb = int64(503) // Web K of me, the one that acts
)

type fakeOnline map[int64]bool

func (f fakeOnline) OnlineAuthorizations(context.Context, int64) (map[int64]bool, error) {
	return f, nil
}

type fakeAuths struct {
	owners map[int64]int64
	active map[int64]time.Time
}

func (f *fakeAuths) UserOf(_ context.Context, k int64) (int64, error) { return f.owners[k], nil }
func (f *fakeAuths) LastActive(_ context.Context, k int64) (time.Time, error) {
	return f.active[k], nil
}

type settingsKey struct {
	peerType int32
	peerId   int64
}

type fakeSettings map[settingsKey]*mtproto.PeerNotifySettings

func (f fakeSettings) NotifySettings(_ context.Context, _ int64, t int32, id int64) (*mtproto.PeerNotifySettings, error) {
	if s, ok := f[settingsKey{t, id}]; ok {
		return s, nil
	}
	return mtproto.MakeTLPeerNotifySettings(&mtproto.PeerNotifySettings{}).To_PeerNotifySettings(), nil
}

type fakeMessages map[int32]*mtproto.Message

func (f fakeMessages) UserMessage(_ context.Context, _ int64, id int32) (*mtproto.Message, error) {
	return f[id], nil
}

type sent struct {
	provider string
	token    string
	p        string // encrypted
	apns     *provider.APNsPush
}

type fakeProviders struct {
	mu       sync.Mutex
	sent     []sent
	lastAPNs *provider.APNsPush
	outcome  map[string]provider.Outcome // by token; sent by default
}

func (f *fakeProviders) result(token string) provider.Outcome {
	if o, ok := f.outcome[token]; ok {
		return o
	}
	return provider.OutcomeSent
}

type fakeAPNs struct{ *fakeProviders }

func (f fakeAPNs) Send(_ context.Context, p *provider.APNsPush) (provider.Outcome, error) {
	var body struct {
		P string `json:"p"`
	}
	_ = json.Unmarshal(p.Body, &body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sent{provider: "apns", token: p.Token, p: body.P, apns: p})
	f.lastAPNs = p
	return f.result(p.Token), nil
}

type fakeFCM struct{ *fakeProviders }

func (f fakeFCM) Send(_ context.Context, token string, data map[string]string, _ bool) (provider.Outcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sent{provider: "fcm", token: token, p: data["p"]})
	return f.result(token), nil
}

type env struct {
	core     *Core
	devices  *pushdevices.MemStore
	auths    *fakeAuths
	settings fakeSettings
	online   fakeOnline
	messages fakeMessages
	pushes   *fakeProviders
	secrets  map[string][]byte // by token
	ids      map[string]int64  // device ids by token
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{
		devices:  pushdevices.NewMemStore(),
		auths:    &fakeAuths{owners: map[int64]int64{keyFCM: me, keyIOS: me, keyWeb: me}, active: map[int64]time.Time{}},
		settings: fakeSettings{},
		online:   fakeOnline{},
		messages: fakeMessages{},
		pushes:   &fakeProviders{outcome: map[string]provider.Outcome{}},
		secrets:  map[string][]byte{},
		ids:      map[string]int64{},
	}
	e.core = New(Deps{
		Devices: e.devices, Online: e.online, Authorizations: e.auths, Settings: e.settings, Messages: e.messages,
		APNs: fakeAPNs{e.pushes}, FCM: fakeFCM{e.pushes},
	})
	e.register(t, keyFCM, pushdevices.TokenFCM, "fcm-token", "ru", false)
	e.register(t, keyIOS, pushdevices.TokenAPNs, "apns-token", "ru", true)
	return e
}

func (e *env) register(t *testing.T, key int64, tokenType int32, token, lang string, noMuted bool) {
	t.Helper()
	secret := bytes.Repeat([]byte{byte(len(e.secrets) + 1)}, pushcrypt.SecretLength)
	d := &pushdevices.Device{UserId: me, AuthKeyId: key, TokenType: tokenType, Token: token, Secret: secret, LangCode: lang, NoMuted: noMuted}
	if err := e.devices.Register(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	e.secrets[token], e.ids[token] = secret, d.Id
}

// take returns the pushes sent since the last call, decrypted, by token.
func (e *env) take(t *testing.T) map[string]map[string]any {
	t.Helper()
	e.pushes.mu.Lock()
	defer e.pushes.mu.Unlock()
	out := map[string]map[string]any{}
	for _, s := range e.pushes.sent {
		plain, err := pushcrypt.DecodeP(e.secrets[s.token], s.p)
		if err != nil {
			t.Fatalf("push to %s does not decrypt: %v", s.token, err)
		}
		var m map[string]any
		if err = json.Unmarshal(plain, &m); err != nil {
			t.Fatal(err)
		}
		if _, dup := out[s.token]; dup {
			t.Fatalf("two pushes to %s", s.token)
		}
		out[s.token] = m
	}
	e.pushes.sent = nil
	return out
}

func (e *env) lastAPNs() *provider.APNsPush {
	e.pushes.mu.Lock()
	defer e.pushes.mu.Unlock()
	return e.pushes.lastAPNs
}

func newMessage(id int32, peer *mtproto.Peer, text string) *mtproto.Updates {
	m := mtproto.MakeTLMessage(&mtproto.Message{Id: id, PeerId: peer, FromId: mtproto.MakePeerUser(anna), Message: text}).To_Message()
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{mtproto.MakeTLUpdateNewMessage(&mtproto.Update{Message_MESSAGE: m}).To_Update()},
		Users:   []*mtproto.User{mtproto.MakeTLUser(&mtproto.User{Id: anna, FirstName: wrapperspb.String("Анна")}).To_User()},
		Chats:   []*mtproto.Chat{mtproto.MakeTLChat(&mtproto.Chat{Id: group, Title: "Дача"}).To_Chat()},
	}).To_Updates()
}

func iosAlert(m map[string]any) (string, string) {
	aps, _ := m["aps"].(map[string]any)
	alert, _ := aps["alert"].(map[string]any)
	title, _ := alert["title"].(string)
	body, _ := alert["body"].(string)
	return title, body
}

func keys(m map[string]map[string]any) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func TestMessageToEveryDevice(t *testing.T) {
	e := newEnv(t)
	e.register(t, keyWeb, pushdevices.TokenWebPush, `{"endpoint":"x"}`, "ru", false)
	e.core.Handle(context.Background(), &Event{UserId: me, Updates: newMessage(5, mtproto.MakePeerUser(anna), "привет")})
	got := e.take(t)
	if len(got) != 2 {
		t.Fatalf("pushes to %v, want fcm-token and apns-token", keys(got))
	}
	if a := got["fcm-token"]; a["loc_key"] != "MESSAGE_TEXT" || a["user_id"] != float64(me) {
		t.Fatalf("android: %v", a)
	}
	if title, body := iosAlert(got["apns-token"]); title != "Анна" || body != "привет" {
		t.Fatalf("ios: %v", got["apns-token"])
	}
	p := e.lastAPNs()
	var outer map[string]any
	_ = json.Unmarshal(p.Body, &outer)
	aps := outer["aps"].(map[string]any)
	if p.Type != provider.APNsAlert || aps["mutable-content"] != float64(1) || aps["alert"].(map[string]any)["body"] != "Новое сообщение" ||
		bytes.Contains(p.Body, []byte("привет")) || p.Expiration.IsZero() {
		t.Fatalf("APNs request: %+v %s", p, p.Body)
	}
}

func TestWhoGetsNoPush(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	// The authorization that acted, and one online.
	e.online[keyIOS] = true
	e.core.Handle(ctx, &Event{UserId: me, Except: keyFCM, Updates: newMessage(5, mtproto.MakePeerUser(anna), "x")})
	if got := e.take(t); len(got) != 0 {
		t.Fatalf("pushes to %v", keys(got))
	}
	delete(e.online, keyIOS)

	// A key that is no longer the user's: the device goes.
	e.auths.owners[keyFCM] = 0
	e.core.Handle(ctx, &Event{UserId: me, Updates: newMessage(6, mtproto.MakePeerUser(anna), "x")})
	if got := e.take(t); len(got) != 1 || got["apns-token"] == nil {
		t.Fatalf("pushes to %v, want apns-token only", keys(got))
	}
	if ds, _ := e.devices.List(ctx, me); len(ds) != 1 || ds[0].Token != "apns-token" {
		t.Fatalf("devices: %+v", ds)
	}
}

func TestMuted(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	future := wrapperspb.Int32(int32(time.Now().Add(time.Hour).Unix()))
	e.settings[settingsKey{mtproto.PEER_USER, anna}] = mtproto.MakeTLPeerNotifySettings(&mtproto.PeerNotifySettings{MuteUntil: future}).To_PeerNotifySettings()

	e.core.Handle(ctx, &Event{UserId: me, Updates: newMessage(5, mtproto.MakePeerUser(anna), "x")})
	got := e.take(t)
	// Android (no_muted off) gets the silent MESSAGE_MUTED, iOS (no_muted) nothing.
	if len(got) != 1 || got["fcm-token"]["loc_key"] != "MESSAGE_MUTED" {
		t.Fatalf("muted: %v", got)
	}
	// Not again within 15 seconds.
	e.core.Handle(ctx, &Event{UserId: me, Updates: newMessage(6, mtproto.MakePeerUser(anna), "x")})
	if got = e.take(t); len(got) != 0 {
		t.Fatalf("second muted: %v", got)
	}
	e.core.muted.now = func() time.Time { return time.Now().Add(MutedEvery) }
	e.core.Handle(ctx, &Event{UserId: me, Updates: newMessage(7, mtproto.MakePeerUser(anna), "x")})
	if got = e.take(t); len(got) != 1 {
		t.Fatalf("muted after 15 s: %v", got)
	}

	// A mention gets through.
	ups := newMessage(8, mtproto.MakePeerUser(anna), "@me")
	ups.Updates[0].Message_MESSAGE.Mentioned = true
	e.core.Handle(ctx, &Event{UserId: me, Updates: ups})
	if got = e.take(t); got["fcm-token"]["loc_key"] != "MESSAGE_TEXT" || got["apns-token"] == nil {
		t.Fatalf("mention in a muted chat: %v", got)
	}

	// Groups muted by default, one unmuted.
	e.settings[settingsKey{mtproto.PEER_CHATS, 0}] = mtproto.MakeTLPeerNotifySettings(&mtproto.PeerNotifySettings{MuteUntil: future}).To_PeerNotifySettings()
	e.settings[settingsKey{mtproto.PEER_CHAT, group}] = mtproto.MakeTLPeerNotifySettings(&mtproto.PeerNotifySettings{MuteUntil: wrapperspb.Int32(0)}).To_PeerNotifySettings()
	e.core.Handle(ctx, &Event{UserId: me, Updates: newMessage(9, mtproto.MakePeerChat(group), "x")})
	if got = e.take(t); got["fcm-token"]["loc_key"] != "CHAT_MESSAGE_TEXT" {
		t.Fatalf("unmuted group: %v", got)
	}
}

func TestPreviewsSilentAndLock(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.settings[settingsKey{mtproto.PEER_USERS, 0}] = mtproto.MakeTLPeerNotifySettings(&mtproto.PeerNotifySettings{
		ShowPreviews: mtproto.BoolFalse, Silent: mtproto.BoolTrue,
	}).To_PeerNotifySettings()
	e.core.Handle(ctx, &Event{UserId: me, Updates: newMessage(5, mtproto.MakePeerUser(anna), "секрет")})
	got := e.take(t)
	custom, _ := got["fcm-token"]["custom"].(map[string]any)
	if got["fcm-token"]["loc_key"] != "MESSAGE_NOTEXT" || custom["silent"] != float64(1) {
		t.Fatalf("android without previews: %v", got["fcm-token"])
	}
	if title, body := iosAlert(got["apns-token"]); title != "Анна" || body != "Новое сообщение" || got["apns-token"]["silent"] != "1" {
		t.Fatalf("ios without previews: %v", got["apns-token"])
	}

	// The iPhone locked past its period: no name either.
	delete(e.settings, settingsKey{mtproto.PEER_USERS, 0})
	ds, _ := e.devices.List(ctx, me)
	for _, d := range ds {
		if d.TokenType == pushdevices.TokenAPNs {
			_ = e.devices.SetLockedPeriod(ctx, me, d.AuthKeyId, 60)
		}
	}
	e.auths.active[keyIOS] = time.Now().Add(-2 * time.Minute)
	e.core.Handle(ctx, &Event{UserId: me, Updates: newMessage(6, mtproto.MakePeerUser(anna), "секрет")})
	if title, body := iosAlert(e.take(t)["apns-token"]); title != "" || body != "Новое сообщение" {
		t.Fatalf("locked: %q %q", title, body)
	}
	e.auths.active[keyIOS] = time.Now()
	e.core.Handle(ctx, &Event{UserId: me, Updates: newMessage(7, mtproto.MakePeerUser(anna), "секрет")})
	if _, body := iosAlert(e.take(t)["apns-token"]); body != "секрет" {
		t.Fatalf("active within the period: %q", body)
	}
}

func TestInvalidTokens(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.pushes.outcome["fcm-token"] = provider.OutcomeTokenInvalid
	e.pushes.outcome["apns-token"] = provider.OutcomeTokenInvalid
	e.core.Handle(ctx, &Event{UserId: me, Updates: newMessage(5, mtproto.MakePeerUser(anna), "x")})
	_ = e.take(t)
	if ds, _ := e.devices.List(ctx, me); len(ds) != 0 {
		t.Fatalf("devices after invalid tokens: %+v", ds)
	}
	// FCM: gone. APNs: told on the next registration.
	err := e.devices.Register(ctx, &pushdevices.Device{UserId: me, AuthKeyId: keyIOS, TokenType: pushdevices.TokenAPNs, Token: "apns-token", Secret: e.secrets["apns-token"]})
	if err != pushdevices.ErrTokenInvalidated {
		t.Fatalf("APNs token registered again: %v", err)
	}
	if err = e.devices.Register(ctx, &pushdevices.Device{UserId: me, AuthKeyId: keyFCM, TokenType: pushdevices.TokenFCM, Token: "fcm-token", Secret: e.secrets["fcm-token"]}); err != nil {
		t.Fatalf("FCM token registered again: %v", err)
	}
}

func TestRevoke(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	reset := mtproto.MakeTLUpdateAccountResetAuthorization(&mtproto.Updates{UserId: me, AuthKeyId: keyFCM}).To_Updates()
	e.core.Handle(ctx, &Event{UserId: me, Only: keyFCM, Updates: reset})
	got := e.take(t)
	if len(got) != 1 || got["fcm-token"]["loc_key"] != "SESSION_REVOKE" {
		t.Fatalf("revoke: %v", got)
	}
	if ds, _ := e.devices.List(ctx, me); len(ds) != 1 || ds[0].AuthKeyId != keyIOS {
		t.Fatalf("devices after revoke: %+v", ds)
	}
	// Other pushes to one authorization are not notifications.
	e.core.Handle(ctx, &Event{UserId: me, Only: keyIOS, Updates: newMessage(5, mtproto.MakePeerUser(anna), "x")})
	if got = e.take(t); len(got) != 0 {
		t.Fatalf("updatesMe: %v", got)
	}
}

func TestPinnedAndReadHistory(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.messages[3] = mtproto.MakeTLMessage(&mtproto.Message{Id: 3, Message: "встреча в 7"}).To_Message()
	pin := mtproto.MakeTLMessageService(&mtproto.Message{
		Id: 12, PeerId: mtproto.MakePeerUser(anna), FromId: mtproto.MakePeerUser(anna),
		Action:  mtproto.MakeTLMessageActionPinMessage(nil).To_MessageAction(),
		ReplyTo: mtproto.MakeTLMessageReplyHeader(&mtproto.MessageReplyHeader{ReplyToMsgId: 3}).To_MessageReplyHeader(),
	}).To_Message()
	ups := mtproto.MakeTLUpdates(&mtproto.Updates{Updates: []*mtproto.Update{mtproto.MakeTLUpdateNewMessage(&mtproto.Update{Message_MESSAGE: pin}).To_Update()}}).To_Updates()
	e.core.Handle(ctx, &Event{UserId: me, Updates: ups})
	got := e.take(t)
	if got["fcm-token"]["loc_key"] != "PINNED_TEXT" {
		t.Fatalf("pinned: %v", got["fcm-token"])
	}

	read := mtproto.MakeTLUpdateReadHistoryInbox(&mtproto.Update{Peer_PEER: mtproto.MakePeerUser(anna), MaxId: 12}).To_Update()
	e.core.Handle(ctx, &Event{UserId: me, Except: keyFCM, Updates: mtproto.MakeTLUpdates(&mtproto.Updates{Updates: []*mtproto.Update{read}}).To_Updates()})
	got = e.take(t)
	if len(got) != 1 || got["apns-token"]["loc-key"] != "READ_HISTORY" || got["apns-token"]["max_id"] != "12" {
		t.Fatalf("read history: %v", got)
	}
	if p := e.lastAPNs(); p.Type != provider.APNsBackground || bytes.Contains(p.Body, []byte(`"alert"`)) {
		t.Fatalf("read history APNs request: %+v %s", p, p.Body)
	}
}
