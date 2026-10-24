package sess

import (
	"context"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/interface/session/internal/dao"
)

// The login of a user with a 2FA password (task 008): bff answers
// SESSION_PASSWORD_NEEDED without binding the key, and the key has no user
// until auth.checkPassword.

func TestCheckRpcWhilePasswordNeeded(t *testing.T) {
	cases := []struct {
		query mtproto.TLObject
		want  bool
	}{
		// The password step and the reset of the account.
		{&mtproto.TLAccountGetPassword{}, true},
		{&mtproto.TLAuthCheckPassword{}, true},
		{&mtproto.TLAuthRequestPasswordRecovery{}, true},
		{&mtproto.TLAuthRecoverPassword{}, true},
		{&mtproto.TLAuthCheckRecoveryPassword{}, true},
		{&mtproto.TLAccountDeleteAccount{}, true},
		// Starting over, and what a client asks before login.
		{&mtproto.TLAuthSendCode{}, true},
		{&mtproto.TLAuthSignIn{}, true},
		{&mtproto.TLAuthSignUp{}, true},
		{&mtproto.TLAuthCancelCode{}, true},
		{&mtproto.TLAuthExportLoginToken{}, true},
		{&mtproto.TLAuthLogOut{}, true},
		{&mtproto.TLAuthBindTempAuthKey{}, true},
		{&mtproto.TLHelpGetConfig{}, true},
		{&mtproto.TLLangpackGetLangPack{}, true},
		// Methods of a logged-in account.
		{&mtproto.TLAccountGetPasswordSettings{}, false},
		{&mtproto.TLAccountUpdatePasswordSettings{}, false},
		{&mtproto.TLAccountDeclinePasswordReset{}, false},
		{&mtproto.TLAccountConfirmPasswordEmail{}, false},
		{&mtproto.TLAuthAcceptLoginToken{}, false},
		{&mtproto.TLAuthExportAuthorization{}, false},
		{&mtproto.TLAccountRegisterPasskey{}, false},
		{&mtproto.TLAccountGetPasskeys{}, false},
		{&mtproto.TLAccountDeletePasskey{}, false},
		{&mtproto.TLMessagesGetDialogs{}, false},
		{&mtproto.TLUpdatesGetState{}, false},
		{&mtproto.TLUploadGetFile{}, false},
	}
	for _, c := range cases {
		if got := checkRpcWhilePasswordNeeded(c.query); got != c.want {
			t.Errorf("checkRpcWhilePasswordNeeded(%T) = %v, want %v", c.query, got, c.want)
		}
	}
}

const testPasswordUser = int64(4242)

// newTestPermWrapper returns a permanent-key wrapper of a key that is not
// logged in, not started.
func newTestPermWrapper(t *testing.T) (*MainAuthWrapper, *session) {
	t.Helper()
	manager := NewMainAuthWrapperManager(&dao.Dao{AuthsessionClient: &fakeAuthsessionClient{}})
	w := newMainAuthWrapper(testVictimPerm, 0, mtproto.AuthStateUnauthorized, nil, 0, manager)
	w.resetAuth(mtproto.AuthKeyTypePerm, testVictimPerm)
	sess := newSession(testSessionId, w.mainAuth)
	w.mainAuth.sessions[testSessionId] = sess
	return w, sess
}

func loginResult(sess *session, query mtproto.TLObject, result mtproto.TLObject) *rpcApiMessage {
	return &rpcApiMessage{
		ctx:       context.Background(),
		sessList:  sess.sessList,
		sessionId: testSessionId,
		reqMsgId:  testBindMsgId,
		reqMsg:    query,
		rpcResult: &mtproto.TLRpcResult{ReqMsgId: testBindMsgId, Result: result},
	}
}

func authorizationOf(userId int64) *mtproto.Auth_Authorization {
	return mtproto.MakeTLAuthAuthorization(&mtproto.Auth_Authorization{
		User: mtproto.MakeTLUser(&mtproto.User{Id: userId, Self: true}).To_User(),
	}).To_Auth_Authorization()
}

func TestSessionPasswordNeededHasNoUser(t *testing.T) {
	for _, login := range []mtproto.TLObject{
		&mtproto.TLAuthSignIn{},
		&mtproto.TLAuthSendCode{},
		&mtproto.TLAuthExportLoginToken{},
	} {
		w, sess := newTestPermWrapper(t)
		rpcErr := mtproto.NewRpcError(mtproto.ErrSessionPasswordNeeded)
		sess.onRpcResult(context.Background(), loginResult(sess, login, rpcErr))
		if w.state != mtproto.AuthStateNeedPassword || w.AuthUserId != 0 {
			t.Fatalf("%T: state %d, user %d after SESSION_PASSWORD_NEEDED, want %d without a user",
				login, w.state, w.AuthUserId, mtproto.AuthStateNeedPassword)
		}
		if rpcErr.Code() != 401 || rpcErr.Message() != "SESSION_PASSWORD_NEEDED" {
			t.Fatalf("%T: the client gets %d %s", login, rpcErr.Code(), rpcErr.Message())
		}

		// Methods of the account are refused, the password step runs (as a
		// key without a user: bff finds the waiting login by the key).
		for i, q := range []mtproto.TLObject{&mtproto.TLMessagesGetDialogs{}, &mtproto.TLAuthAcceptLoginToken{}, &mtproto.TLAccountUpdatePasswordSettings{}} {
			msg := &inboxMsg{msgId: testBindMsgId + 8 + int64(i)*4}
			if sess.onRpcRequest(context.Background(), testGatewayId, testClientIp, msg, q, nil) {
				t.Fatalf("%T accepted while the password is needed", q)
			}
			if e := rpcErrorInOutQueue(t, sess, msg.msgId); e == nil || e.Code() != 401 || e.Message() != "SESSION_PASSWORD_NEEDED" {
				t.Fatalf("%T: reply %v, want 401 SESSION_PASSWORD_NEEDED", q, e)
			}
		}
		msg := &inboxMsg{msgId: testBindMsgId + 64}
		if !sess.onRpcRequest(context.Background(), testGatewayId, testClientIp, msg, &mtproto.TLAccountGetPassword{}, nil) {
			t.Fatal("account.getPassword refused while the password is needed")
		}
		if len(w.tmpRpcApiMessageList) != 1 || w.AuthUserId != 0 {
			t.Fatalf("queued %d, user %d", len(w.tmpRpcApiMessageList), w.AuthUserId)
		}

		// The right password: bff bound the key.
		sess.onRpcResult(context.Background(), loginResult(sess, &mtproto.TLAuthCheckPassword{}, authorizationOf(testPasswordUser)))
		if w.state != mtproto.AuthStateNormal || w.AuthUserId != testPasswordUser {
			t.Fatalf("%T: state %d, user %d after auth.checkPassword", login, w.state, w.AuthUserId)
		}
		// A late SESSION_PASSWORD_NEEDED does not log the key out of it.
		sess.onRpcResult(context.Background(), loginResult(sess, login, mtproto.NewRpcError(mtproto.ErrSessionPasswordNeeded)))
		if w.state != mtproto.AuthStateNormal || w.AuthUserId != testPasswordUser {
			t.Fatalf("%T: state %d, user %d after a late SESSION_PASSWORD_NEEDED", login, w.state, w.AuthUserId)
		}
	}
}

func TestCheckPasswordAfterSessionRestart(t *testing.T) {
	// The wrapper did not see SESSION_PASSWORD_NEEDED (session restarted):
	// the result of auth.checkPassword still logs the key in.
	w, sess := newTestPermWrapper(t)
	sess.onRpcResult(context.Background(), loginResult(sess, &mtproto.TLAuthCheckPassword{}, authorizationOf(testPasswordUser)))
	if w.state != mtproto.AuthStateNormal || w.AuthUserId != testPasswordUser {
		t.Fatalf("state %d, user %d", w.state, w.AuthUserId)
	}
}

func TestPasswordLoginExpired(t *testing.T) {
	w, sess := newTestPermWrapper(t)
	sess.onRpcResult(context.Background(), loginResult(sess, &mtproto.TLAuthSignIn{}, mtproto.NewRpcError(mtproto.ErrSessionPasswordNeeded)))
	// A wrong password keeps the wait.
	sess.onRpcResult(context.Background(), loginResult(sess, &mtproto.TLAuthCheckPassword{}, mtproto.NewRpcError(mtproto.ErrPasswordHashInvalid)))
	if w.state != mtproto.AuthStateNeedPassword {
		t.Fatalf("state %d after a wrong password", w.state)
	}
	// The login waited too long: bff forgot it, the key starts over.
	sess.onRpcResult(context.Background(), loginResult(sess, &mtproto.TLAuthCheckPassword{}, mtproto.NewRpcError(mtproto.ErrAuthKeyUnregistered)))
	if w.state != mtproto.AuthStateUnauthorized || w.AuthUserId != 0 {
		t.Fatalf("state %d, user %d after the login expired", w.state, w.AuthUserId)
	}
}

func TestLegacyPasswordNeededErrorIsNotParsed(t *testing.T) {
	// The upstream hack SESSION_PASSWORD_NEEDED_<user id> is gone: such an
	// error names no user to the session.
	w, sess := newTestPermWrapper(t)
	rpcErr := mtproto.NewRpcError(mtproto.ErrSessionPasswordNeeded)
	rpcErr.SetErrorMessage("SESSION_PASSWORD_NEEDED_42")
	sess.onRpcResult(context.Background(), loginResult(sess, &mtproto.TLAuthSignIn{}, rpcErr))
	if w.state != mtproto.AuthStateUnauthorized || w.AuthUserId != 0 {
		t.Fatalf("state %d, user %d", w.state, w.AuthUserId)
	}
}
