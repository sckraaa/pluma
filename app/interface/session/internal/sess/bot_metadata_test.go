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

package sess

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/interface/session/internal/dao"
	userclient "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

const (
	testBotUser   = int64(7001)
	testHumanUser = int64(7002)
	testGoneUser  = int64(7003)
)

// fakeUserClient answers user.getImmutableUser: testBotUser is a bot,
// testHumanUser is not, testGoneUser gets USER_ID_INVALID; err fails every read.
type fakeUserClient struct {
	userclient.UserClient

	mu    sync.Mutex
	err   error
	reads map[int64]int
}

func (f *fakeUserClient) UserGetImmutableUser(_ context.Context, in *userpb.TLUserGetImmutableUser) (*mtproto.ImmutableUser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reads == nil {
		f.reads = map[int64]int{}
	}
	f.reads[in.GetId()]++
	if f.err != nil {
		return nil, f.err
	}
	u := &mtproto.UserData{Id: in.GetId()}
	switch in.GetId() {
	case testBotUser:
		u.Bot = mtproto.MakeTLBotData(&mtproto.BotData{}).To_BotData()
	case testHumanUser:
	default:
		return nil, mtproto.ErrUserIdInvalid
	}
	return mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{User: mtproto.MakeTLUserData(u).To_UserData()}).To_ImmutableUser(), nil
}

func (f *fakeUserClient) readsOf(id int64) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads[id]
}

// newTestBotWrapper is newTestPermWrapper with biz user answering users.
func newTestBotWrapper(t *testing.T) (*MainAuthWrapper, *session, *fakeUserClient) {
	t.Helper()
	users := &fakeUserClient{}
	manager := NewMainAuthWrapperManager(&dao.Dao{AuthsessionClient: &fakeAuthsessionClient{}, UserClient: users})
	w := newMainAuthWrapper(testVictimPerm, 0, mtproto.AuthStateUnauthorized, nil, 0, manager)
	w.resetAuth(mtproto.AuthKeyTypePerm, testVictimPerm)
	sess := newSession(testSessionId, w.mainAuth)
	w.mainAuth.sessions[testSessionId] = sess
	return w, sess, users
}

func testRequest(sess *session, connectionId string) *rpcApiMessage {
	return &rpcApiMessage{
		ctx:                  context.Background(),
		sessList:             sess.sessList,
		sessionId:            testSessionId,
		reqMsgId:             testBindMsgId + 100,
		reqMsg:               &mtproto.TLMessagesGetDialogs{},
		businessConnectionId: connectionId,
	}
}

func botAuthorizationOf(userId int64) *mtproto.Auth_Authorization {
	return mtproto.MakeTLAuthAuthorization(&mtproto.Auth_Authorization{
		User: mtproto.MakeTLUser(&mtproto.User{Id: userId, Self: true, Bot: true}).To_User(),
	}).To_Auth_Authorization()
}

func wantMetadata(t *testing.T, w *MainAuthWrapper, sess *session, userId int64, isBot bool) {
	t.Helper()
	md, err := w.rpcMetadata(testRequest(sess, ""))
	if err != nil {
		t.Fatalf("rpcMetadata: %v", err)
	}
	if md.UserId != userId || md.IsBot != isBot {
		t.Fatalf("metadata of user %d, bot %v; want user %d, bot %v", md.UserId, md.IsBot, userId, isBot)
	}
}

// The requests of a bot's key carry IsBot, those of a user's key do not;
// whether the user is a bot is read once for the user the key is bound to.
func TestRpcMetadataIsBot(t *testing.T) {
	w, sess, users := newTestBotWrapper(t)

	// Not logged in: no user, no bot, nothing read.
	wantMetadata(t, w, sess, 0, false)

	// The bot logs in with its token.
	sess.onRpcResult(context.Background(), loginResult(sess, &mtproto.TLAuthImportBotAuthorization{}, botAuthorizationOf(testBotUser)))
	wantMetadata(t, w, sess, testBotUser, true)
	wantMetadata(t, w, sess, testBotUser, true)
	if n := users.readsOf(testBotUser); n != 1 {
		t.Fatalf("the bot was read %d times for two requests, want once", n)
	}

	// Through a business connection the request stays the bot's: bff runs
	// it as the account (business_connection.go) and keeps IsBot.
	md, err := w.rpcMetadata(testRequest(sess, "conn-1"))
	if err != nil || !md.IsBot || md.BusinessConnectionId != "conn-1" {
		t.Fatalf("business connection request: %v, bot %v, connection %q", err, md.GetIsBot(), md.GetBusinessConnectionId())
	}

	// The key loses its user and is bound to a user that is no bot.
	w.changeAuthState(context.Background(), mtproto.AuthStateNeedPassword, int64(0))
	wantMetadata(t, w, sess, 0, false)
	sess.onRpcResult(context.Background(), loginResult(sess, &mtproto.TLAuthCheckPassword{}, authorizationOf(testHumanUser)))
	wantMetadata(t, w, sess, testHumanUser, false)
	if n := users.readsOf(testHumanUser); n != 1 {
		t.Fatalf("the user was read %d times, want once", n)
	}
}

// A key restored after a restart of the session (its user from
// authsession) is asked about its user too.
func TestRpcMetadataIsBotAfterRestart(t *testing.T) {
	users := &fakeUserClient{}
	manager := NewMainAuthWrapperManager(&dao.Dao{AuthsessionClient: &fakeAuthsessionClient{}, UserClient: users})
	w := newMainAuthWrapper(testVictimPerm, testBotUser, mtproto.AuthStateNormal, nil, 0, manager)
	w.resetAuth(mtproto.AuthKeyTypePerm, testVictimPerm)
	sess := newSession(testSessionId, w.mainAuth)
	w.mainAuth.sessions[testSessionId] = sess
	wantMetadata(t, w, sess, testBotUser, true)
}

// A failed read is the request's error and is not kept: the next request
// reads again. USER_ID_INVALID, which biz also answers when it fails to
// load the user, is such a failure and never makes a bot a user.
func TestRpcMetadataIsBotReadFails(t *testing.T) {
	w, sess, users := newTestBotWrapper(t)
	sess.onRpcResult(context.Background(), loginResult(sess, &mtproto.TLAuthImportBotAuthorization{}, botAuthorizationOf(testBotUser)))

	users.err = errors.New("biz unavailable")
	if md, err := w.rpcMetadata(testRequest(sess, "")); err == nil {
		t.Fatalf("a failed read gave metadata of user %d, bot %v", md.UserId, md.IsBot)
	}
	users.err = nil
	wantMetadata(t, w, sess, testBotUser, true)
	if n := users.readsOf(testBotUser); n != 2 {
		t.Fatalf("the bot was read %d times, want again after the failure", n)
	}

	w2, sess2, users2 := newTestBotWrapper(t)
	sess2.onRpcResult(context.Background(), loginResult(sess2, &mtproto.TLAuthSignIn{}, authorizationOf(testGoneUser)))
	for i := 0; i < 2; i++ {
		if md, err := w2.rpcMetadata(testRequest(sess2, "")); err == nil {
			t.Fatalf("USER_ID_INVALID gave metadata of user %d, bot %v", md.UserId, md.IsBot)
		}
	}
	if n := users2.readsOf(testGoneUser); n != 2 {
		t.Fatalf("the user was read %d times, want again after USER_ID_INVALID", n)
	}
}

// A request whose metadata cannot be made is answered with a temporary
// error, which the clients repeat, and never runs as a user's.
func TestRpcRunLoopAnswersFailedBotRead(t *testing.T) {
	w, sess, users := newTestBotWrapper(t)
	sess.onRpcResult(context.Background(), loginResult(sess, &mtproto.TLAuthImportBotAuthorization{}, botAuthorizationOf(testBotUser)))
	users.err = errors.New("biz unavailable")

	w.finish.Add(1)
	go w.rpcRunLoop()
	defer w.rpcQueue.Close()

	w.sendToRpcQueue(context.Background(), []*rpcApiMessage{testRequest(sess, "")})
	select {
	case v := <-w.rpcDataChan:
		r := v.(*rpcApiMessage).rpcResult
		if r == nil {
			t.Fatal("no result")
		}
		rpcErr, _ := r.Result.(*mtproto.TLRpcError)
		if rpcErr == nil || rpcErr.Code() != 500 || rpcErr.Message() != "INTERNAL_SERVER_ERROR" {
			t.Fatalf("answer %T %v, want 500 INTERNAL_SERVER_ERROR", r.Result, rpcErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no answer")
	}
}
