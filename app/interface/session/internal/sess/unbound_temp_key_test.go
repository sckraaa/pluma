package sess

import (
	"context"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/interface/session/internal/dao"
)

const (
	testTempKeyId   = int64(0x7e3f_0000_0000_0001)
	testVictimPerm  = int64(0x1c71_0000_0000_0002)
	testSessionId   = int64(0x5e55_0000_0000_0003)
	testBindMsgId   = int64(0x6000_0000_0000_0004)
	testGatewayId   = "gateway-1"
	testClientIp    = "192.0.2.1"
	testUnboundUser = int64(0)
)

// newTestUnboundWrapper returns an unbound-temp-key wrapper that is not
// started, so tests can drive it without racing its loops.
func newTestUnboundWrapper(t *testing.T, client *fakeAuthsessionClient) (*MainAuthWrapper, *session) {
	t.Helper()
	manager := NewMainAuthWrapperManager(&dao.Dao{AuthsessionClient: client})
	w := newMainAuthWrapper(testTempKeyId, testUnboundUser, mtproto.AuthStateNew, nil, 0, manager)
	w.unboundTempKey = true
	w.resetAuth(mtproto.AuthKeyTypeTemp, testTempKeyId)
	sess := newSession(testSessionId, w.tempAuth)
	w.tempAuth.sessions[testSessionId] = sess
	return w, sess
}

func rpcErrorInOutQueue(t *testing.T, sess *session, reqMsgId int64) *mtproto.TLRpcError {
	t.Helper()
	oMsg := sess.outQueue.Lookup(reqMsgId)
	if oMsg == nil {
		return nil
	}
	dBuf := mtproto.NewDecodeBuf(oMsg.msg.Body)
	obj := dBuf.Object()
	if err := dBuf.GetError(); err != nil {
		t.Fatalf("decode queued reply: %v", err)
	}
	rpcResult, ok := obj.(*mtproto.TLRpcResult)
	if !ok {
		t.Fatalf("queued reply is %T, want rpc_result", obj)
	}
	rpcErr, _ := rpcResult.Result.(*mtproto.TLRpcError)
	return rpcErr
}

func TestCheckRpcWithUnboundTempKey(t *testing.T) {
	cases := []struct {
		query mtproto.TLObject
		want  bool
	}{
		{&mtproto.TLAuthBindTempAuthKey{}, true},
		{&mtproto.TLHelpGetConfig{}, true},
		{&mtproto.TLHelpGetNearestDc{}, true},
		{&mtproto.TLAuthSendCode{}, false},
		{&mtproto.TLAuthSignIn{}, false},
		{&mtproto.TLAuthImportAuthorization{}, false},
		{&mtproto.TLUpdatesGetState{}, false},
		{&mtproto.TLMessagesGetDialogs{}, false},
		{&mtproto.TLAccountRegisterDevice{}, false},
		{&mtproto.TLLangpackGetLangPack{}, false},
	}

	for _, c := range cases {
		if got := checkRpcWithUnboundTempKey(c.query); got != c.want {
			t.Errorf("checkRpcWithUnboundTempKey(%T) = %v, want %v", c.query, got, c.want)
		}
	}
}

func TestUnboundTempKeyWrapperRejectsOtherMethods(t *testing.T) {
	// The rest of a container sent with an unbound key (here: next to a forged
	// auth.bindTempAuthKey naming someone else's perm key) must not run.
	for _, query := range []mtproto.TLObject{
		&mtproto.TLMessagesGetDialogs{},
		&mtproto.TLUpdatesGetState{},
		&mtproto.TLAuthSendCode{},
		&mtproto.TLAccountRegisterDevice{TokenType: 7, Token: "1"},
	} {
		client := &fakeAuthsessionClient{}
		w, sess := newTestUnboundWrapper(t, client)
		msg := &inboxMsg{msgId: testBindMsgId + 4}

		if sess.onRpcRequest(context.Background(), testGatewayId, testClientIp, msg, query, nil) {
			t.Fatalf("%T: request accepted on an unbound temp key", query)
		}
		if len(w.tmpRpcApiMessageList) != 0 {
			t.Fatalf("%T: %d requests queued for execution", query, len(w.tmpRpcApiMessageList))
		}
		rpcErr := rpcErrorInOutQueue(t, sess, msg.msgId)
		if rpcErr == nil || rpcErr.Code() != 401 || rpcErr.Message() != "AUTH_KEY_PERM_EMPTY" {
			t.Fatalf("%T: reply = %v, want 401 AUTH_KEY_PERM_EMPTY", query, rpcErr)
		}
		if w.mainUpdatesSession != nil || w.androidPushSession != nil || w.pushSessionId != 0 {
			t.Fatalf("%T: wrapper state changed by a rejected request", query)
		}
	}
}

func TestUnboundTempKeyWrapperAllowsBindAndConfig(t *testing.T) {
	for _, query := range []mtproto.TLObject{
		&mtproto.TLAuthBindTempAuthKey{PermAuthKeyId: testVictimPerm},
		&mtproto.TLHelpGetConfig{},
		&mtproto.TLHelpGetNearestDc{},
	} {
		client := &fakeAuthsessionClient{}
		w, sess := newTestUnboundWrapper(t, client)
		msg := &inboxMsg{msgId: testBindMsgId}

		if !sess.onRpcRequest(context.Background(), testGatewayId, testClientIp, msg, query, nil) {
			t.Fatalf("%T: request rejected on an unbound temp key", query)
		}
		if len(w.tmpRpcApiMessageList) != 1 {
			t.Fatalf("%T: %d requests queued, want 1", query, len(w.tmpRpcApiMessageList))
		}
		if client.getAuthStateDataCalls != 0 {
			t.Fatalf("%T: auth state fetched for an unbound temp key", query)
		}
	}
}

func TestUnboundTempKeyWrapperHasNoPermKeyOrUser(t *testing.T) {
	client := &fakeAuthsessionClient{}
	w, _ := newTestUnboundWrapper(t, client)

	if got := w.permAuthKeyId(); got != 0 {
		t.Fatalf("permAuthKeyId() = %d, want 0", got)
	}
	if w.AuthUserId != 0 {
		t.Fatalf("AuthUserId = %d, want 0", w.AuthUserId)
	}

	w.onUpdateLayer(context.Background(), 228)
	w.onUpdateInitConnection(context.Background(), testClientIp, &mtproto.TLInitConnection{ApiId: 4, DeviceModel: "x"})
	if client.setLayerCalls != 0 || client.setInitConnectionCalls != 0 {
		t.Fatalf("client info persisted for an unbound temp key: setLayer %d, setInitConnection %d",
			client.setLayerCalls, client.setInitConnectionCalls)
	}
	if w.Layer() != 228 {
		t.Fatalf("Layer() = %d, want 228 (kept in memory)", w.Layer())
	}

	permWrapper := newMainAuthWrapper(testVictimPerm, 42, mtproto.AuthStateNormal, nil, 0, w.cb)
	if got := permWrapper.permAuthKeyId(); got != testVictimPerm {
		t.Fatalf("perm wrapper permAuthKeyId() = %d, want %d", got, testVictimPerm)
	}
}

func TestNewUnboundTempAuthWrapper(t *testing.T) {
	manager := NewMainAuthWrapperManager(&dao.Dao{AuthsessionClient: &fakeAuthsessionClient{}})
	w := NewUnboundTempAuthWrapper(testTempKeyId, manager)
	defer w.Stop()

	if !w.unboundTempKey || w.authKeyId != testTempKeyId || w.AuthUserId != 0 || w.permAuthKeyId() != 0 {
		t.Fatalf("unexpected wrapper: unbound %v, authKeyId %d, user %d, perm %d",
			w.unboundTempKey, w.authKeyId, w.AuthUserId, w.permAuthKeyId())
	}
}

func TestDoRpcRequestBindTempAuthKeyForwardsConnectionMetadata(t *testing.T) {
	client := &fakeAuthsessionClient{}
	w, _ := newTestUnboundWrapper(t, client)

	request := &rpcApiMessage{
		ctx:       context.Background(),
		sessList:  w.tempAuth,
		sessionId: testSessionId,
		clientIp:  testClientIp,
		reqMsgId:  testBindMsgId,
		reqMsg: &mtproto.TLAuthBindTempAuthKey{
			PermAuthKeyId:    testVictimPerm,
			Nonce:            7,
			ExpiresAt:        1_800_000_000,
			EncryptedMessage: []byte{1, 2, 3},
		},
	}
	md := &metadata.RpcMetadata{
		ServerId:      "session-1",
		ClientAddr:    testClientIp,
		AuthId:        request.sessList.authId,
		SessionId:     request.sessionId,
		ClientMsgId:   request.reqMsgId,
		UserId:        w.AuthUserId,
		PermAuthKeyId: w.permAuthKeyId(),
	}

	doRpcRequest(context.Background(), w.cb.Dao, md, request)

	if client.bindTempAuthKeyCalls != 1 {
		t.Fatalf("authsession.bindTempAuthKey calls = %d, want 1", client.bindTempAuthKeyCalls)
	}
	got := client.bindTempAuthKeyMD
	if got == nil {
		t.Fatal("authsession.bindTempAuthKey called without rpc metadata")
	}
	if got.AuthId != testTempKeyId || got.SessionId != testSessionId || got.ClientMsgId != testBindMsgId {
		t.Fatalf("metadata = {auth_id: %d, session_id: %d, client_msg_id: %d}, want {%d, %d, %d}",
			got.AuthId, got.SessionId, got.ClientMsgId, testTempKeyId, testSessionId, testBindMsgId)
	}
	if got.PermAuthKeyId != 0 || got.UserId != 0 {
		t.Fatalf("metadata attributes the unbound key: perm %d, user %d", got.PermAuthKeyId, got.UserId)
	}
	if in := client.bindTempAuthKeyIn; in.PermAuthKeyId != testVictimPerm || in.Nonce != 7 || in.ExpiresAt != 1_800_000_000 {
		t.Fatalf("forwarded request = %v", in)
	}
	if request.rpcResult == nil || request.rpcResult.ReqMsgId != testBindMsgId {
		t.Fatalf("rpc result = %v", request.rpcResult)
	}
}

func TestFailedBindTempAuthKeyKeepsWrapper(t *testing.T) {
	client := &fakeAuthsessionClient{bindTempAuthKeyErr: mtproto.ErrEncryptedMessageInvalid}
	manager := NewMainAuthWrapperManager(&dao.Dao{AuthsessionClient: client})
	w := newMainAuthWrapper(testVictimPerm, 42, mtproto.AuthStateNormal, nil, 0, manager)
	manager.AllocMainAuthWrapper(testVictimPerm, func(int64) *MainAuthWrapper { return w })
	w.running.Set(true)

	w.resetAuth(mtproto.AuthKeyTypeTemp, testTempKeyId)
	sess := newSession(testSessionId, w.tempAuth)
	w.tempAuth.sessions[testSessionId] = sess

	request := &rpcApiMessage{
		ctx:       context.Background(),
		sessList:  w.tempAuth,
		sessionId: testSessionId,
		reqMsgId:  testBindMsgId,
		reqMsg:    &mtproto.TLAuthBindTempAuthKey{PermAuthKeyId: testVictimPerm},
	}
	doRpcRequest(context.Background(), manager.Dao, &metadata.RpcMetadata{AuthId: testTempKeyId}, request)
	if rpcErr, ok := request.TryGetRpcResultError(); !ok || rpcErr.Message() != "ENCRYPTED_MESSAGE_INVALID" {
		t.Fatalf("rpc result = %v, want ENCRYPTED_MESSAGE_INVALID", request.rpcResult)
	}

	sess.onRpcResult(context.Background(), request)

	if manager.GetMainAuthWrapper(testVictimPerm) != w {
		t.Fatal("wrapper removed after a failed auth.bindTempAuthKey")
	}
	if w.state != mtproto.AuthStateNormal || w.AuthUserId != 42 || !w.running.True() {
		t.Fatalf("wrapper changed after a failed auth.bindTempAuthKey: state %d, user %d, running %v",
			w.state, w.AuthUserId, w.running.True())
	}
}

// After auth.bindTempAuthKey succeeds, the client acknowledges to the
// permanent key's wrapper: the wrapper of the unbound key drops what it sent
// and sends the binding's answer once (task 008; it sent them again every
// waitMsgAcksTimeout). A permanent key's wrapper keeps waiting for acks.
func TestBoundTempKeyWrapperDoesNotResend(t *testing.T) {
	w, sess := newTestUnboundWrapper(t, &fakeAuthsessionClient{})
	sent := sess.outQueue.AddNotifyMsg(1, true, &mtproto.TLMessageRawData{MsgId: 4001, Seqno: 1})
	sent.sent = 1_800_000_000
	sess.inQueue.AddMsgId(testBindMsgId)

	request := &rpcApiMessage{
		ctx:       context.Background(),
		sessList:  w.tempAuth,
		sessionId: testSessionId,
		reqMsgId:  testBindMsgId,
		reqMsg:    &mtproto.TLAuthBindTempAuthKey{PermAuthKeyId: testVictimPerm},
	}
	doRpcRequest(context.Background(), w.cb.Dao, &metadata.RpcMetadata{AuthId: testTempKeyId}, request)
	if _, failed := request.TryGetRpcResultError(); failed {
		t.Fatalf("binding failed: %v", request.rpcResult)
	}
	sess.onRpcResult(context.Background(), request)

	if sess.outQueue.Lookup(sent.msgId) != nil {
		t.Fatal("a message sent before the binding is still waiting for an ack")
	}
	if sess.outQueue.oMsgs.Len() != 1 {
		t.Fatalf("%d messages queued, want the binding's answer", sess.outQueue.oMsgs.Len())
	}
	var pendings []*outboxMsg
	for e := sess.outQueue.oMsgs.Front(); e != nil; e = e.Next() {
		pendings = append(pendings, e.Value.(*outboxMsg))
	}
	sess.updatePendingStates(context.Background(), pendings, 1_800_000_001)
	if sess.outQueue.oMsgs.Len() != 0 {
		t.Fatal("the binding's answer waits for an ack that will not come here")
	}

	// A permanent key's wrapper: the same states keep its messages.
	manager := NewMainAuthWrapperManager(&dao.Dao{AuthsessionClient: &fakeAuthsessionClient{}})
	perm := newMainAuthWrapper(testVictimPerm, 42, mtproto.AuthStateNormal, nil, 0, manager)
	perm.resetAuth(mtproto.AuthKeyTypeTemp, testTempKeyId)
	permSess := newSession(testSessionId, perm.tempAuth)
	perm.tempAuth.sessions[testSessionId] = permSess
	perm.tempAuth.changeAuthState(mtproto.AuthStatePermBound)
	m := permSess.outQueue.AddNotifyMsg(2, true, &mtproto.TLMessageRawData{MsgId: 4005, Seqno: 1})
	permSess.updatePendingStates(context.Background(), []*outboxMsg{m}, 1_800_000_001)
	if permSess.outQueue.Lookup(m.msgId) == nil {
		t.Fatal("a permanent key's wrapper dropped a message waiting for its ack")
	}
}
