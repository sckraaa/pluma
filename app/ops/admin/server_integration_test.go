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
	"os"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
	"github.com/teamgram/teamgram-server/pkg/moderation"
	"github.com/teamgram/teamgram-server/pkg/moderation/effects"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
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

// fakeServices are the services a decision reaches.
type fakeServices struct {
	notified, deleted, revoked int
	failNotify                 bool
}

func (f *fakeServices) MsgPushUserMessage(ctx context.Context, in *msgpb.TLMsgPushUserMessage) (*mtproto.Bool, error) {
	if f.failNotify {
		return nil, fmt.Errorf("msg is down")
	}
	f.notified++
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

type env struct {
	srv   *httptest.Server
	store *store
	dao   *moderation.DAO
	fake  *fakeServices
}

func newEnv(t *testing.T) *env {
	t.Helper()
	c := itDB.Config()
	db := sqlx.NewMySQL(&c)
	st := &store{db: db}
	dao := moderation.New(db)
	fake := &fakeServices{}
	cfg := &config{Listen: "127.0.0.1:0", Origin: testOrigin}
	runner := &effects.Runner{DAO: dao, Msg: fake, Channel: fake, Authsession: fake, Sync: fake}
	srv := httptest.NewServer(newServer(cfg, st, dao, runner, fake).routes())
	t.Cleanup(srv.Close)
	return &env{srv: srv, store: st, dao: dao, fake: fake}
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

func (e *env) person(t *testing.T, login string, role int8) *client {
	t.Helper()
	if err := e.store.setAdmin(context.Background(), login, role, false, "test"); err != nil {
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
	mod := e.person(t, "mod@example", roleModerator)

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
	mod := e.person(t, "mod2@example", roleModerator)
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
// of the panel; roles decide what is open.
func TestPanelIdentityAndRoles(t *testing.T) {
	e := newEnv(t)
	observer := e.person(t, "obs@example", roleObserver)
	observer.register()
	observer.loginWithPasskey()
	if code, _ := observer.do("POST", "/api/reports/1/decide", map[string]any{"payload": `{"action":"dismiss"}`}); code != http.StatusForbidden {
		t.Errorf("an observer decided: %d", code)
	}

	// Not a person of the panel.
	stranger := &client{t: t, e: e, login: "stranger@example"}
	if code, _ := stranger.do("GET", "/api/me", nil); code != http.StatusForbidden {
		t.Errorf("a stranger: %d", code)
	}

	// The same header from outside the machine is not believed.
	s := newServer(&config{Origin: testOrigin}, e.store, e.dao, nil, e.fake)
	req := httptest.NewRequest("GET", "/api/me", nil)
	req.RemoteAddr = "100.64.0.1:40000"
	req.Header.Set(identityHeader, "obs@example")
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
	req2.Header.Set(identityHeader, "obs@example")
	req2.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("another origin: %d", resp.StatusCode)
	}
}
