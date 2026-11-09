//go:build integration

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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
	"github.com/teamgram/teamgram-server/pkg/logincode"
	"github.com/teamgram/teamgram-server/pkg/moderation"
	"github.com/teamgram/teamgram-server/pkg/moderation/effects"
	"github.com/teamgram/teamgram-server/pkg/phoneallowlist"
	"github.com/teamgram/teamgram-server/pkg/testinfra"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var itDB *testinfra.MySQL

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	if itDB, err = testinfra.StartMySQL(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = itDB.Terminate(ctx)
	os.Exit(code)
}

// fakeServices are the services the panel reaches.
type fakeServices struct {
	notified, deleted, revoked int
	failNotify                 bool
	userDeleted                []int64
	channelsBlocked            map[int64]bool
	chatsBlocked               map[int64]bool
	channelsDeleted            []int64
	chatsDeleted               []int64
	messaged                   []int64
	forgotten                  []int64
	failForget                 bool
	// pushes counts the calls of MsgPushUserMessage; the call number
	// downAt finds the message service down, once.
	pushes, downAt int
}

func (f *fakeServices) MsgPushUserMessage(ctx context.Context, in *msgpb.TLMsgPushUserMessage) (*mtproto.Bool, error) {
	if f.failNotify {
		return nil, fmt.Errorf("msg is down")
	}
	if f.pushes++; f.pushes == f.downAt {
		return nil, status.Error(codes.Unavailable, "msg restarts")
	}
	f.notified++
	f.messaged = append(f.messaged, in.PeerId)
	return mtproto.BoolTrue, nil
}
func (f *fakeServices) MsgDeleteMessages(ctx context.Context, in *msgpb.TLMsgDeleteMessages) (*mtproto.Messages_AffectedMessages, error) {
	f.deleted++
	return nil, nil
}
func (f *fakeServices) ChannelDeleteMessages(ctx context.Context, in *channel.TLChannelDeleteMessages) (*mtproto.Messages_AffectedMessages, error) {
	f.deleted++
	return nil, nil
}
func (f *fakeServices) AuthsessionResetAuthorization(ctx context.Context, in *authsession.TLAuthsessionResetAuthorization) (*authsession.Vector_Long, error) {
	f.revoked++
	return &authsession.Vector_Long{}, nil
}
func (f *fakeServices) AuthsessionGetRecentLangCode(ctx context.Context, in *authsession.TLAuthsessionGetRecentLangCode) (*mtproto.String, error) {
	return &mtproto.String{V: "en"}, nil
}
func (f *fakeServices) SyncUpdatesMe(ctx context.Context, in *sync.TLSyncUpdatesMe) (*mtproto.Void, error) {
	return &mtproto.Void{}, nil
}
func (f *fakeServices) DfsDownloadFile(ctx context.Context, in *dfs.TLDfsDownloadFile) (*mtproto.Upload_File, error) {
	return nil, nil
}
func (f *fakeServices) ChannelSetBlocked(ctx context.Context, in *channel.TLChannelSetBlocked) (*mtproto.Bool, error) {
	f.channelsBlocked[in.ChannelId] = mtproto.FromBool(in.Blocked)
	return mtproto.BoolTrue, nil
}
func (f *fakeServices) ChannelDeleteChannel(ctx context.Context, in *channel.TLChannelDeleteChannel) (*mtproto.Updates, error) {
	f.channelsDeleted = append(f.channelsDeleted, in.ChannelId)
	return nil, nil
}
func (f *fakeServices) ChatSetBlocked(ctx context.Context, in *chat.TLChatSetBlocked) (*mtproto.Bool, error) {
	f.chatsBlocked[in.ChatId] = mtproto.FromBool(in.Blocked)
	return mtproto.BoolTrue, nil
}
func (f *fakeServices) ChatDeleteChat(ctx context.Context, in *chat.TLChatDeleteChat) (*mtproto.MutableChat, error) {
	f.chatsDeleted = append(f.chatsDeleted, in.ChatId)
	return nil, nil
}
func (f *fakeServices) UserDeleteUser(ctx context.Context, in *user.TLUserDeleteUser) (*mtproto.Bool, error) {
	f.userDeleted = append(f.userDeleted, in.UserId)
	return mtproto.BoolTrue, nil
}
func (f *fakeServices) UserDeleteUsername(ctx context.Context, in *user.TLUserDeleteUsername) (*mtproto.Bool, error) {
	return mtproto.BoolTrue, nil
}
func (f *fakeServices) AuthsessionGetAuthorizations(ctx context.Context, in *authsession.TLAuthsessionGetAuthorizations) (*mtproto.Account_Authorizations, error) {
	return mtproto.MakeTLAccountAuthorizations(&mtproto.Account_Authorizations{Authorizations: []*mtproto.Authorization{}}).To_Account_Authorizations(), nil
}
func (f *fakeServices) Forget(ctx context.Context, userId int64) error {
	if f.failForget {
		return fmt.Errorf("redis is down")
	}
	f.forgotten = append(f.forgotten, userId)
	return nil
}

// fakeCodes is the login code service: a secret that must reach the response
// and nothing else.
type fakeCodes struct{ code string }

func (f *fakeCodes) Pending(ctx context.Context, phone string) ([]logincode.PendingRequest, error) {
	return nil, nil
}
func (f *fakeCodes) Reissue(ctx context.Context, phone, requestId string) (*logincode.Reissued, error) {
	return &logincode.Reissued{Code: f.code, ExpiresAt: time.Now().Add(time.Minute)}, nil
}

type fakeOutbox struct{ replayed, discarded int }

func (f *fakeOutbox) list(ctx context.Context, states ...int8) ([]outboxRow, error) { return nil, nil }
func (f *fakeOutbox) replay(ctx context.Context, recipient int64) (int, error) {
	f.replayed++
	return 1, nil
}
func (f *fakeOutbox) discard(ctx context.Context, id int64) (outboxRow, error) {
	f.discarded++
	return outboxRow{Id: id}, nil
}

type fakeProm struct{ down bool }

func (f *fakeProm) services(ctx context.Context) ([]serviceState, error) {
	if f.down {
		return nil, fmt.Errorf("down")
	}
	return []serviceState{{Job: "bff", Instance: "bff:1", Up: true}, {Job: "msg", Instance: "msg:1", Up: false}}, nil
}
func (f *fakeProm) alerts(ctx context.Context) ([]alertState, error) { return nil, nil }
func (f *fakeProm) disk(ctx context.Context) (*diskState, error) {
	return &diskState{Available: 10, Size: 100}, nil
}

type fakeReleases struct{ withdrawn int }

func (f *fakeReleases) list(ctx context.Context) ([]platformReleases, error) {
	return []platformReleases{{Platform: "android"}}, nil
}
func (f *fakeReleases) setWithdrawn(ctx context.Context, platform string, build int32, withdrawn bool) (*platformReleases, error) {
	if withdrawn {
		f.withdrawn++
	}
	return &platformReleases{Platform: platform}, nil
}

type env struct {
	srv    *httptest.Server
	store  *store
	dao    *moderation.DAO
	fake   *fakeServices
	codes  *fakeCodes
	outbox *fakeOutbox
	prom   *fakeProm
	rel    *fakeReleases
	db     *sqlx.DB
}

func newEnv(t *testing.T) *env {
	t.Helper()
	c := itDB.Config()
	db := sqlx.NewMySQL(&c)
	st := &store{db: db}
	dao := moderation.New(db)
	fake := &fakeServices{channelsBlocked: map[int64]bool{}, chatsBlocked: map[int64]bool{}}
	codes := &fakeCodes{code: "54321"}
	box := &fakeOutbox{}
	prom := &fakeProm{}
	rel := &fakeReleases{}
	cfg := &config{Listen: "127.0.0.1:0", Origin: testOrigin}
	runner := &effects.Runner{DAO: dao, Msg: fake, Channel: fake, Authsession: fake, Sync: fake}
	d := deps{dao: dao, runner: runner, dfs: fake, channel: fake, chat: fake, user: fake, auth: fake, msg: fake,
		codes: codes, twofa: fake, allowlist: phoneallowlist.New(db), outbox: box, prom: prom, releases: rel}
	srv := httptest.NewServer(newServer(cfg, st, d).routes())
	t.Cleanup(srv.Close)
	return &env{srv: srv, store: st, dao: dao, fake: fake, codes: codes, outbox: box, prom: prom, rel: rel, db: db}
}

// client talks to the panel as a person of the tailnet would.
type client struct {
	t      *testing.T
	e      *env
	login  string
	cookie *http.Cookie
	key    *softKey
	count  uint32
}

func (e *env) person(t *testing.T, login string) *client {
	t.Helper()
	if err := e.store.setAdmin(context.Background(), login, false, "test"); err != nil {
		t.Fatal(err)
	}
	return &client{t: t, e: e, login: login, key: newSoftKey(t)}
}

func (c *client) do(method, path string, body any) (int, []byte) {
	c.t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		c.t.Fatal(err)
	}
	req, err := http.NewRequest(method, c.e.srv.URL+path, bytes.NewReader(data))
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set(identityHeader, c.login)
	req.Header.Set("Origin", testOrigin)
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	for _, ck := range resp.Cookies() {
		if ck.Name == sessionCookie {
			c.cookie = ck
		}
	}
	return resp.StatusCode, out
}

func (c *client) begin(path string, body any) map[string]any {
	c.t.Helper()
	code, out := c.do("POST", path, body)
	if code != http.StatusOK {
		c.t.Fatalf("%s: %d %s", path, code, out)
	}
	var v map[string]any
	if err := json.Unmarshal(out, &v); err != nil {
		c.t.Fatal(err)
	}
	return v
}

func (c *client) assertion(challenge string) *assertionBody {
	c.t.Helper()
	c.count++
	client := clientDataFor(c.t, "webauthn.get", challenge, testOrigin)
	auth := authDataFor(testRP, flagUserPresent|flagUserVerified, c.count, nil)
	return &assertionBody{
		CredentialId:      b64(c.key.id),
		ClientDataJSON:    b64(client),
		AuthenticatorData: b64(auth),
		Signature:         b64(c.key.sign(c.t, auth, client)),
		Challenge:         challenge,
	}
}

func (c *client) register() {
	c.t.Helper()
	begun := c.begin("/api/passkey/register/begin", map[string]any{})
	challenge := begun["challengeHex"].(string)
	client := clientDataFor(c.t, "webauthn.create", challenge, testOrigin)
	att := c.key.attestation(c.t, flagUserPresent|flagUserVerified|flagAttested, 0)
	code, out := c.do("POST", "/api/passkey/register/finish", map[string]any{"challenge": challenge, "clientDataJSON": b64(client), "attestationObject": b64(att)})
	if code != http.StatusOK {
		c.t.Fatalf("register: %d %s", code, out)
	}
}

func (c *client) loginWithPasskey() {
	c.t.Helper()
	begun := c.begin("/api/login/begin", map[string]any{})
	code, out := c.do("POST", "/api/login/finish", c.assertion(begun["challengeHex"].(string)))
	if code != http.StatusOK {
		c.t.Fatalf("login: %d %s", code, out)
	}
}

// signedAction sends a dangerous action with its step-up.
func (c *client) signedAction(path string, payload any) (int, []byte) {
	c.t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		c.t.Fatal(err)
	}
	begun := c.begin("/api/stepup/begin", map[string]any{"path": path, "payload": string(data)})
	return c.do("POST", path, map[string]any{"payload": string(data), "assertion": c.assertion(begun["challengeHex"].(string))})
}

var uniqueUser = time.Now().UnixMilli()

func (e *env) report(t *testing.T, target int64) int64 {
	t.Helper()
	uniqueUser++
	reporter := uniqueUser
	if err := e.dao.AddReport(context.Background(), &moderation.Report{
		ReporterUserId: reporter, Kind: moderation.KindMessage, PeerType: mtproto.PEER_USER, PeerId: target, TargetUserId: target, Reason: "spam",
	}, []int32{1}, &moderation.Snapshot{Messages: []moderation.SnapshotMessage{{Id: 1, Text: "text"}}}); err != nil {
		t.Fatal(err)
	}
	rows, err := e.dao.ReportsOfTarget(context.Background(), target, 1)
	if err != nil || len(rows) != 1 {
		t.Fatalf("report: %v %v", rows, err)
	}
	return rows[0].Id
}

func TestPanelLoginAndDecision(t *testing.T) {
	e := newEnv(t)
	mod := e.person(t, "mod@example")

	// Before the passkey, only the ceremonies are open.
	if code, _ := mod.do("GET", "/api/reports", nil); code != http.StatusUnauthorized {
		t.Fatalf("without a session: %d", code)
	}
	mod.register()
	mod.loginWithPasskey()
	if code, out := mod.do("GET", "/api/reports", nil); code != http.StatusOK {
		t.Fatalf("queue: %d %s", code, out)
	}

	uniqueUser++
	target := uniqueUser
	id := e.report(t, target)

	path := fmt.Sprintf("/api/reports/%d/decide", id)
	body := `{"action":"ban","reason":"spam"}`
	// A dangerous action without the passkey is refused.
	if code, _ := mod.do("POST", path, map[string]any{"payload": body}); code != http.StatusUnauthorized {
		t.Fatalf("ban without confirmation: %d", code)
	}
	// An assertion made for another action is refused.
	begun := mod.begin("/api/stepup/begin", map[string]any{"path": path, "payload": `{"action":"dismiss"}`})
	if code, _ := mod.do("POST", path, map[string]any{"payload": body, "assertion": mod.assertion(begun["challengeHex"].(string))}); code != http.StatusForbidden {
		t.Fatalf("ban with another action's assertion: %d", code)
	}

	code, out := mod.signedAction(path, map[string]any{"action": "ban", "reason": "spam"})
	if code != http.StatusOK {
		t.Fatalf("ban: %d %s", code, out)
	}
	if e.fake.notified != 1 || e.fake.revoked != 1 {
		t.Errorf("steps of the ban: notified %d revoked %d", e.fake.notified, e.fake.revoked)
	}
	r, err := e.dao.Restriction(context.Background(), target)
	if err != nil || r == nil || r.Kind != moderation.RestrictBanned {
		t.Errorf("restriction after the ban: %+v %v", r, err)
	}
	if code, _ := mod.signedAction(path, map[string]any{"action": "ban"}); code != http.StatusConflict {
		t.Errorf("a decided report: %d", code)
	}

	// Reading a report is journaled.
	if code, _ := mod.do("GET", fmt.Sprintf("/api/reports/%d", id), nil); code != http.StatusOK {
		t.Fatalf("report: %d", code)
	}
	entries, err := e.dao.AuditLog(context.Background(), 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	var views int
	for _, a := range entries {
		if a.Action == "view_report" && a.Target == fmt.Sprintf("report:%d", id) {
			views++
		}
	}
	if views != 1 {
		t.Errorf("views in the journal: %d", views)
	}
}

// A failed step leaves the report for the retry, without repeating what was
// done.
func TestPanelRestrictionFailureIsResumable(t *testing.T) {
	e := newEnv(t)
	mod := e.person(t, "mod2@example")
	mod.register()
	mod.loginWithPasskey()
	uniqueUser++
	target := uniqueUser
	id := e.report(t, target)

	e.fake.failNotify = true
	path := fmt.Sprintf("/api/reports/%d/decide", id)
	if code, out := mod.signedAction(path, map[string]any{"action": "restrict", "reason": "x"}); code != http.StatusOK {
		t.Fatalf("restrict: %d %s", code, out)
	}
	report, err := e.dao.Report(context.Background(), id)
	if err != nil || report.EffectsStage != 0 || report.EffectsError == "" || report.EffectsAttempts != 1 {
		t.Fatalf("after a failed step: %+v %v", report, err)
	}
	// The restriction itself is in force already.
	if r, err := e.dao.Restriction(context.Background(), target); err != nil || r == nil {
		t.Fatalf("the restriction must be in force: %+v %v", r, err)
	}

	e.fake.failNotify = false
	runner := &effects.Runner{DAO: e.dao, Msg: e.fake, Channel: e.fake, Authsession: e.fake, Sync: e.fake}
	if err = runner.Run(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if e.fake.notified != 1 {
		t.Errorf("notified %d times", e.fake.notified)
	}
	if report, err = e.dao.Report(context.Background(), id); err != nil || report.EffectsStage != 1 || report.EffectsError != "" {
		t.Fatalf("after the retry: %+v %v", report, err)
	}
}

// The identity header is believed only from loopback and only for a person
// of the panel: no roles, whoever enters is the owner.
func TestPanelIdentity(t *testing.T) {
	e := newEnv(t)
	owner := e.person(t, "owner@example")
	owner.register()
	owner.loginWithPasskey()

	// Not a person of the panel.
	stranger := &client{t: t, e: e, login: "stranger@example"}
	if code, _ := stranger.do("GET", "/api/me", nil); code != http.StatusForbidden {
		t.Errorf("a stranger: %d", code)
	}

	// The same header from outside the machine is not believed.
	s := newServer(&config{Origin: testOrigin}, e.store, deps{dao: e.dao})
	req := httptest.NewRequest("GET", "/api/me", nil)
	req.RemoteAddr = "100.64.0.1:40000"
	req.Header.Set(identityHeader, "owner@example")
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("a remote peer with the header: %d", rec.Code)
	}

	// A write from another origin.
	req2, err := http.NewRequest("POST", e.srv.URL+"/api/logout", bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	req2.Header.Set(identityHeader, "owner@example")
	req2.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("another origin: %d", resp.StatusCode)
	}

	// Disabling a person ends the sessions.
	if err := e.store.setAdmin(context.Background(), "owner@example", true, "test"); err != nil {
		t.Fatal(err)
	}
	if code, _ := owner.do("GET", "/api/me", nil); code != http.StatusForbidden {
		t.Errorf("a disabled person: %d", code)
	}
}

func unique(prefix string) string {
	uniqueUser++
	return fmt.Sprintf("%s%d", prefix, uniqueUser)
}

func (e *env) makeUser(t *testing.T, phone, first, username string) int64 {
	t.Helper()
	uniqueUser++
	id := uniqueUser
	if _, err := e.db.Exec(context.Background(),
		"INSERT INTO users (id, user_type, access_hash, first_name, last_name, username, phone, country_code) VALUES (?, 2, 1, ?, 'Testov', ?, ?, 'RU')",
		id, first, username, phone); err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *env) makeChannel(t *testing.T, creator int64, title string) int64 {
	t.Helper()
	uniqueUser++
	id := uniqueUser
	if _, err := e.db.Exec(context.Background(),
		"INSERT INTO channels (id, access_hash, creator_user_id, title, broadcast, participants_count, `date`) VALUES (?, 1, ?, ?, 1, 2, ?)",
		id, creator, title, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *env) makeChat(t *testing.T, creator int64, title string) int64 {
	t.Helper()
	uniqueUser++
	id := uniqueUser
	if _, err := e.db.Exec(context.Background(),
		"INSERT INTO chats (id, creator_user_id, access_hash, random_id, participant_count, title, `date`) VALUES (?, ?, 1, 1, 2, ?, ?)",
		id, creator, title, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	return id
}

func decodeJSON(t *testing.T, data []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("%s: %v", data, err)
	}
}

func (e *env) journalHas(t *testing.T, action, target string) bool {
	t.Helper()
	entries, err := e.dao.AuditLogFiltered(context.Background(), moderation.AuditFilter{Action: action, Target: target, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	return len(entries) > 0
}

// Every destructive action needs the passkey and an assertion made for it;
// without one nothing happens.
func TestPanelDestructiveActionsNeedPasskey(t *testing.T) {
	e := newEnv(t)
	owner := e.person(t, "pk-owner@example")
	owner.register()
	owner.loginWithPasskey()
	id := e.makeUser(t, unique("7700"), "Ann", "")
	ch := e.makeChannel(t, id, "Chan")

	for _, tc := range []struct {
		path    string
		payload string
	}{
		{fmt.Sprintf("/api/users/%d/terminate", id), `{"reason":""}`},
		{fmt.Sprintf("/api/users/%d/restrict", id), `{"action":"ban"}`},
		{fmt.Sprintf("/api/users/%d/code", id), `{}`},
		{fmt.Sprintf("/api/users/%d/message", id), `{"text":"hello"}`},
		{fmt.Sprintf("/api/users/%d/delete", id), fmt.Sprintf(`{"confirm":"%d"}`, id)},
		{fmt.Sprintf("/api/peers/channel/%d/block", ch), `{"reason":""}`},
		{fmt.Sprintf("/api/peers/channel/%d/unblock", ch), `{"reason":""}`},
		{fmt.Sprintf("/api/peers/channel/%d/delete", ch), fmt.Sprintf(`{"confirm":"%d"}`, ch)},
		{"/api/registration/mode", `{"open":true}`},
		{"/api/registration/phones/remove", `{"phone":"+15550001111"}`},
		{"/api/registration/code", `{"phone":"+15550001111"}`},
		{"/api/broadcasts", `{"text":"x","audience":"all"}`},
		{"/api/broadcasts/1/cancel", `{}`},
		{"/api/releases/android/1/withdraw", `{}`},
		{"/api/releases/android/1/restore", `{}`},
		{"/api/system/outbox/replay", `{"recipient":1}`},
		{"/api/system/outbox/discard", `{"id":1}`},
		{"/api/passkeys/remove", `{"id":"AA"}`},
		{"/api/admins", `{"login":"x@example","disabled":false}`},
	} {
		if code, out := owner.do("POST", tc.path, map[string]any{"payload": tc.payload}); code != http.StatusUnauthorized {
			t.Errorf("%s without the passkey: %d %s", tc.path, code, out)
		}
	}
	// Without a session nothing is open at all.
	fresh := &client{t: t, e: e, login: "pk-owner@example"}
	if code, _ := fresh.do("GET", "/api/users", nil); code != http.StatusUnauthorized {
		t.Errorf("users without a session: %d", code)
	}
}

// The deletion of an account: only after a block, with the id typed again.
func TestPanelUserDeletionRules(t *testing.T) {
	e := newEnv(t)
	owner := e.person(t, "del-owner@example")
	owner.register()
	owner.loginWithPasskey()
	id := e.makeUser(t, unique("7701"), "Bob", "bobby")
	path := fmt.Sprintf("/api/users/%d/delete", id)
	body := map[string]any{"confirm": fmt.Sprint(id)}

	// Not blocked yet.
	if code, _ := owner.signedAction(path, body); code != http.StatusConflict {
		t.Fatalf("delete before the block: %d", code)
	}
	if len(e.fake.userDeleted) != 0 {
		t.Fatal("the account was deleted")
	}
	// Blocked, but the wrong confirmation.
	if code, out := owner.signedAction(fmt.Sprintf("/api/users/%d/restrict", id), map[string]any{"action": "ban", "reason": "x"}); code != http.StatusOK {
		t.Fatalf("ban: %d %s", code, out)
	}
	if code, _ := owner.signedAction(path, map[string]any{"confirm": "12"}); code != http.StatusBadRequest {
		t.Fatalf("delete with a wrong confirmation: %d", code)
	}
	// A step that fails is an error, the account stays to be deleted again.
	e.fake.failForget = true
	if code, _ := owner.signedAction(path, body); code != http.StatusBadGateway {
		t.Fatalf("delete with a failing step: %d", code)
	}
	if len(e.fake.userDeleted) != 0 {
		t.Fatal("the account was deleted before its last step")
	}
	e.fake.failForget = false
	// Blocked and confirmed.
	if code, out := owner.signedAction(path, body); code != http.StatusOK {
		t.Fatalf("delete: %d %s", code, out)
	}
	if len(e.fake.userDeleted) != 1 || e.fake.userDeleted[0] != id || len(e.fake.forgotten) != 1 {
		t.Fatalf("delete steps: deleted %v forgotten %v", e.fake.userDeleted, e.fake.forgotten)
	}
	if !e.journalHas(t, "delete_user", fmt.Sprintf("user:%d", id)) {
		t.Error("the deletion is not in the journal")
	}

	// The service account is not a person.
	if code, _ := owner.signedAction("/api/users/777000/restrict", map[string]any{"action": "ban"}); code != http.StatusBadRequest {
		t.Errorf("ban the service account: %d", code)
	}
}

func TestPanelPeerBlockAndDelete(t *testing.T) {
	e := newEnv(t)
	owner := e.person(t, "peer-owner@example")
	owner.register()
	owner.loginWithPasskey()
	creator := e.makeUser(t, unique("7702"), "Cat", "")

	for _, tc := range []struct {
		kind string
		id   int64
	}{
		{peerChannel, e.makeChannel(t, creator, "Chan one")},
		{peerChat, e.makeChat(t, creator, "Group one")},
	} {
		base := fmt.Sprintf("/api/peers/%s/%d", tc.kind, tc.id)
		// Deleting before a block is refused.
		if code, _ := owner.signedAction(base+"/delete", map[string]any{"confirm": fmt.Sprint(tc.id)}); code != http.StatusConflict {
			t.Fatalf("%s delete before block: %d", tc.kind, code)
		}
		if code, out := owner.signedAction(base+"/block", map[string]any{"reason": "spam"}); code != http.StatusOK {
			t.Fatalf("%s block: %d %s", tc.kind, code, out)
		}
		if tc.kind == peerChannel {
			if !e.fake.channelsBlocked[tc.id] {
				t.Fatal("the service did not get the block")
			}
			// The service would have set this; the fake does not.
			if _, err := e.db.Exec(context.Background(), "UPDATE channels SET blocked = 1 WHERE id = ?", tc.id); err != nil {
				t.Fatal(err)
			}
		} else {
			if !e.fake.chatsBlocked[tc.id] {
				t.Fatal("the service did not get the block")
			}
			if _, err := e.db.Exec(context.Background(), "UPDATE chats SET blocked = 1, deactivated = 1 WHERE id = ?", tc.id); err != nil {
				t.Fatal(err)
			}
		}
		if code, _ := owner.signedAction(base+"/delete", map[string]any{"confirm": "x"}); code != http.StatusBadRequest {
			t.Fatalf("%s delete with a wrong confirmation: %d", tc.kind, code)
		}
		if code, out := owner.signedAction(base+"/delete", map[string]any{"confirm": fmt.Sprint(tc.id)}); code != http.StatusOK {
			t.Fatalf("%s delete: %d %s", tc.kind, code, out)
		}
		if !e.journalHas(t, "delete_peer", peerTarget(tc.kind, tc.id)) || !e.journalHas(t, "block_peer", peerTarget(tc.kind, tc.id)) {
			t.Errorf("%s: the journal has no block and delete", tc.kind)
		}
		if code, out := owner.do("GET", base, nil); code != http.StatusOK {
			t.Fatalf("%s card: %d %s", tc.kind, code, out)
		}
	}
	if len(e.fake.channelsDeleted) != 1 || len(e.fake.chatsDeleted) != 1 {
		t.Errorf("deleted: %v %v", e.fake.channelsDeleted, e.fake.chatsDeleted)
	}
}

// The login code reaches the owner in the response, never the journal.
func TestPanelLoginCodeIsNotJournaled(t *testing.T) {
	e := newEnv(t)
	owner := e.person(t, "code-owner@example")
	owner.register()
	owner.loginWithPasskey()
	phone := unique("7703")
	id := e.makeUser(t, phone, "Dan", "")
	code, out := owner.signedAction(fmt.Sprintf("/api/users/%d/code", id), map[string]any{})
	if code != http.StatusOK || !strings.Contains(string(out), e.codes.code) {
		t.Fatalf("code: %d %s", code, out)
	}
	entries, err := e.dao.AuditLogFiltered(context.Background(), moderation.AuditFilter{Action: "login_code", Limit: 10})
	if err != nil || len(entries) == 0 {
		t.Fatalf("journal: %v %v", entries, err)
	}
	for _, a := range entries {
		if strings.Contains(a.Detail, e.codes.code) || strings.Contains(a.Target, e.codes.code) || strings.Contains(a.Detail, phone) {
			t.Fatalf("the journal holds the code or the phone: %+v", a)
		}
	}
}

// The users and the channels are found by what the owner types.
func TestPanelSearch(t *testing.T) {
	e := newEnv(t)
	owner := e.person(t, "search-owner@example")
	owner.register()
	owner.loginWithPasskey()
	phone := unique("7704")
	uname := unique("searchy")
	id := e.makeUser(t, phone, "Евгений", uname)
	ch := e.makeChannel(t, id, "Новости района")

	var res struct {
		Users []struct {
			Id int64 `json:"id"`
		} `json:"users"`
	}
	for _, q := range []string{phone, fmt.Sprint(id), "Евген", "@" + uname} {
		code, out := owner.do("GET", "/api/users?q="+url.QueryEscape(q), nil)
		if code != http.StatusOK {
			t.Fatalf("search %q: %d", q, code)
		}
		decodeJSON(t, out, &res)
		found := false
		for _, u := range res.Users {
			found = found || u.Id == id
		}
		if !found {
			t.Errorf("search %q: not found", q)
		}
	}
	// A wildcard of LIKE is not a wildcard.
	if code, out := owner.do("GET", "/api/users?q="+url.QueryEscape("%"), nil); code != http.StatusOK {
		t.Fatalf("search %%: %d", code)
	} else if decodeJSON(t, out, &res); len(res.Users) != 0 {
		t.Errorf("%%: %d users", len(res.Users))
	}
	var peers struct {
		Peers []struct {
			Id int64 `json:"id"`
		} `json:"peers"`
	}
	for _, q := range []string{"Новости", fmt.Sprint(ch)} {
		code, out := owner.do("GET", "/api/peers?q="+url.QueryEscape(q), nil)
		if code != http.StatusOK {
			t.Fatalf("peers %q: %d %s", q, code, out)
		}
		decodeJSON(t, out, &peers)
		if len(peers.Peers) == 0 || peers.Peers[0].Id != ch {
			t.Errorf("peers %q: %+v", q, peers)
		}
	}
	// A card's read is journaled.
	if code, out := owner.do("GET", fmt.Sprintf("/api/users/%d", id), nil); code != http.StatusOK {
		t.Fatalf("card: %d %s", code, out)
	}
	if !e.journalHas(t, "view_user", fmt.Sprintf("user:%d", id)) {
		t.Error("a card read is not journaled")
	}
}

// Registration: the mode and the list, read back by the login flow.
func TestPanelRegistration(t *testing.T) {
	e := newEnv(t)
	owner := e.person(t, "reg-owner@example")
	owner.register()
	owner.loginWithPasskey()
	ctx := context.Background()
	list := phoneallowlist.New(e.db)
	uniqueUser++
	phone := fmt.Sprintf("1201555%04d", 100+uniqueUser%100) // a number the validator takes
	defer func() { _ = list.SetRegistrationOpen(ctx, false, "test") }()

	if allowed, err := list.Contains(ctx, phone); err != nil || allowed {
		t.Fatalf("by default: %v %v", allowed, err)
	}
	if code, out := owner.signedAction("/api/registration/mode", map[string]any{"open": true}); code != http.StatusOK {
		t.Fatalf("open: %d %s", code, out)
	}
	if allowed, err := list.Contains(ctx, phone); err != nil || !allowed {
		t.Fatalf("open: %v %v", allowed, err)
	}
	if code, out := owner.signedAction("/api/registration/mode", map[string]any{"open": false}); code != http.StatusOK {
		t.Fatalf("list: %d %s", code, out)
	}
	if allowed, err := list.Contains(ctx, phone); err != nil || allowed {
		t.Fatalf("back to the list: %v %v", allowed, err)
	}
	// Add through the list; adding needs no passkey, removing does.
	if code, out := owner.do("POST", "/api/registration/phones", map[string]any{"payload": fmt.Sprintf(`{"phone":"+%s","note":"n"}`, phone)}); code != http.StatusOK {
		t.Fatalf("add: %d %s", code, out)
	}
	if allowed, err := list.Contains(ctx, phone); err != nil || !allowed {
		t.Fatalf("after add: %v %v", allowed, err)
	}
	if code, out := owner.signedAction("/api/registration/phones/remove", map[string]any{"phone": "+" + phone}); code != http.StatusOK {
		t.Fatalf("remove: %d %s", code, out)
	}
	if allowed, _ := list.Contains(ctx, phone); allowed {
		t.Error("the phone is still allowed")
	}
	// The journal has only the last digits.
	entries, err := e.dao.AuditLogFiltered(ctx, moderation.AuditFilter{Action: "allowlist_remove", Limit: 3})
	if err != nil || len(entries) == 0 || strings.Contains(entries[0].Detail, phone) {
		t.Fatalf("journal: %+v %v", entries, err)
	}
}

// A broadcast goes out one by one and resumes from its cursor.
func TestPanelBroadcast(t *testing.T) {
	e := newEnv(t)
	owner := e.person(t, "bc-owner@example")
	owner.register()
	owner.loginWithPasskey()
	var ids []int64
	for i := 0; i < 3; i++ {
		ids = append(ids, e.makeUser(t, unique("7705"), "U", ""))
	}
	idsText := strings.Trim(strings.ReplaceAll(fmt.Sprint(ids), " ", ","), "[]")
	body := map[string]any{"text": "hello all", "audience": "ids", "ids": idsText}
	code, out := owner.signedAction("/api/broadcasts", body)
	if code != http.StatusOK {
		t.Fatalf("start: %d %s", code, out)
	}
	if !e.journalHas(t, "broadcast_start", "broadcast:") {
		t.Error("not journaled")
	}

	// The message service goes down at the second recipient: nobody is lost
	// and the run goes on from there when the lease ends.
	ctx := context.Background()
	e.fake.downAt = 2
	if err := sendNext(ctx, e.store, e.fake); err == nil {
		t.Fatal("the run went on with the message service down")
	}
	if len(e.fake.messaged) != 1 {
		t.Fatalf("sent to %v before the service went down", e.fake.messaged)
	}
	if b, err := e.store.claimBroadcast(ctx); err != nil || b != nil {
		t.Fatalf("taken again under its lease: %+v %v", b, err)
	}
	if _, err := e.db.Exec(ctx, "UPDATE admin_broadcasts SET claim_until = 0 WHERE state = ?", broadcastRunning); err != nil {
		t.Fatal(err)
	}
	if err := sendNext(ctx, e.store, e.fake); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(e.fake.messaged, ids) {
		t.Fatalf("sent to %v, want %v", e.fake.messaged, ids)
	}
	rows, err := e.store.broadcasts(ctx, 5)
	if err != nil || len(rows) == 0 || rows[0].State != broadcastFinished || rows[0].Sent != 3 {
		t.Fatalf("broadcasts: %+v %v", rows, err)
	}
}

func TestPanelSystemAndReleases(t *testing.T) {
	e := newEnv(t)
	owner := e.person(t, "sys-owner@example")
	owner.register()
	owner.loginWithPasskey()

	code, out := owner.do("GET", "/api/system", nil)
	if code != http.StatusOK || !strings.Contains(string(out), `"up":false`) {
		t.Fatalf("system: %d %s", code, out)
	}
	if code, out = owner.do("GET", "/api/overview", nil); code != http.StatusOK || !strings.Contains(string(out), "counters") {
		t.Fatalf("overview: %d %s", code, out)
	}
	if code, out = owner.do("GET", "/api/releases", nil); code != http.StatusOK || !strings.Contains(string(out), "android") {
		t.Fatalf("releases: %d %s", code, out)
	}
	if code, out = owner.signedAction("/api/releases/android/7/withdraw", map[string]any{}); code != http.StatusOK || e.rel.withdrawn != 1 {
		t.Fatalf("withdraw: %d %s", code, out)
	}
	if !e.journalHas(t, "release_withdraw", "release:android:7") {
		t.Error("the rollback is not journaled")
	}
	if code, _ := owner.signedAction("/api/system/outbox/discard", map[string]any{"id": 9}); code != http.StatusOK || e.outbox.discarded != 1 {
		t.Errorf("discard: %d %d", code, e.outbox.discarded)
	}
	// Prometheus down: the rest of the page still answers.
	e.prom.down = true
	if code, out := owner.do("GET", "/api/system", nil); code != http.StatusOK || !strings.Contains(string(out), "services_error") {
		t.Errorf("system without prometheus: %d %s", code, out)
	}
}

// Access: a passkey is removed only while another stays, and the owner
// cannot lock himself out.
func TestPanelAccess(t *testing.T) {
	e := newEnv(t)
	owner := e.person(t, "acc-owner@example")
	owner.register()
	owner.loginWithPasskey()
	var res struct {
		Passkeys []passkeyRow `json:"passkeys"`
	}
	code, out := owner.do("GET", "/api/passkeys", nil)
	if code != http.StatusOK {
		t.Fatalf("passkeys: %d", code)
	}
	decodeJSON(t, out, &res)
	if len(res.Passkeys) != 1 {
		t.Fatalf("passkeys: %+v", res)
	}
	if code, _ := owner.signedAction("/api/passkeys/remove", map[string]any{"id": res.Passkeys[0].Id}); code != http.StatusConflict {
		t.Errorf("remove the last passkey: %d", code)
	}
	if code, _ := owner.signedAction("/api/admins", map[string]any{"login": "acc-owner@example", "disabled": true}); code != http.StatusBadRequest {
		t.Errorf("lock himself out: %d", code)
	}
	if code, out := owner.signedAction("/api/admins", map[string]any{"login": "other@example", "disabled": false}); code != http.StatusOK {
		t.Errorf("add a login: %d %s", code, out)
	}
}
