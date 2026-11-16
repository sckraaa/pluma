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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/interface/session/internal/dao"
)

const testBusinessBot = int64(5151)

// newTestLoggedInWrapper returns a permanent-key wrapper of a logged-in
// key, not started.
func newTestLoggedInWrapper(t *testing.T) (*MainAuthWrapper, *session) {
	t.Helper()
	manager := NewMainAuthWrapperManager(&dao.Dao{AuthsessionClient: &fakeAuthsessionClient{}})
	w := newMainAuthWrapper(testVictimPerm, testBusinessBot, mtproto.AuthStateNormal, nil, 0, manager)
	w.resetAuth(mtproto.AuthKeyTypePerm, testVictimPerm)
	sess := newSession(testSessionId, w.mainAuth)
	w.mainAuth.sessions[testSessionId] = sess
	return w, sess
}

func encodeTL(t *testing.T, o mtproto.TLObject) []byte {
	t.Helper()
	x := mtproto.NewEncodeBuf(256)
	if err := o.Encode(x, 233); err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), x.GetBuf()...)
}

func inBusinessConnection(t *testing.T, connectionId string, query []byte) []byte {
	t.Helper()
	return encodeTL(t, &mtproto.TLInvokeWithBusinessConnection{
		Constructor:  mtproto.CRC32_invokeWithBusinessConnection,
		ConnectionId: connectionId,
		Query:        query,
	})
}

// A query in invokeWithBusinessConnection reaches bff with the
// connection_id in its metadata, through the wrappers around it and inside
// it, as a bot library nests them.
func TestBusinessConnectionReachesMetadata(t *testing.T) {
	send := encodeTL(t, &mtproto.TLMessagesSendMessage{
		Constructor: mtproto.CRC32_messages_sendMessage_fef48f62,
		Peer:        mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: 7}).To_InputPeer(),
		Message:     "hi",
		RandomId:    1,
	})
	inner := encodeTL(t, &mtproto.TLInvokeWithoutUpdates{Constructor: mtproto.CRC32_invokeWithoutUpdates, Query: send})
	for name, body := range map[string][]byte{
		"plain":           inBusinessConnection(t, "conn-1", send),
		"wrapper inside":  inBusinessConnection(t, "conn-1", inner),
		"wrapper outside": encodeTL(t, &mtproto.TLInvokeWithLayer{Constructor: mtproto.CRC32_invokeWithLayer, Layer: 233, Query: inBusinessConnection(t, "conn-1", send)}),
	} {
		t.Run(name, func(t *testing.T) {
			w, sess := newTestLoggedInWrapper(t)
			processEncoded(t, sess, testBindMsgId+12, body)
			if len(w.tmpRpcApiMessageList) != 1 {
				t.Fatalf("%d requests queued, want 1", len(w.tmpRpcApiMessageList))
			}
			req := w.tmpRpcApiMessageList[0]
			if _, ok := req.reqMsg.(*mtproto.TLMessagesSendMessage); !ok {
				t.Fatalf("queued %T", req.reqMsg)
			}
			if req.businessConnectionId != "conn-1" {
				t.Fatalf("business connection %q, want conn-1", req.businessConnectionId)
			}
		})
	}

	// Without the wrapper there is no connection.
	w, sess := newTestLoggedInWrapper(t)
	processEncoded(t, sess, testBindMsgId+12, send)
	if len(w.tmpRpcApiMessageList) != 1 || w.tmpRpcApiMessageList[0].businessConnectionId != "" {
		t.Fatalf("a query without the wrapper carries a business connection")
	}
}

// A method a connected bot may not call through its connection, and an
// empty connection_id, are refused before any service runs them as the bot.
func TestBusinessConnectionRefused(t *testing.T) {
	history := encodeTL(t, &mtproto.TLMessagesGetHistory{
		Constructor: mtproto.CRC32_messages_getHistory,
		Peer:        mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: 7}).To_InputPeer(),
	})
	upload := encodeTL(t, &mtproto.TLMessagesUploadMedia{
		Constructor: mtproto.CRC32_messages_uploadMedia_14967978,
		Peer:        mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: 7}).To_InputPeer(),
		Media:       mtproto.MakeTLInputMediaEmpty(nil).To_InputMedia(),
	})
	send := encodeTL(t, &mtproto.TLMessagesSendMessage{
		Constructor: mtproto.CRC32_messages_sendMessage_fef48f62,
		Peer:        mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: 7}).To_InputPeer(),
		Message:     "hi",
		RandomId:    1,
	})
	for _, tc := range []struct {
		name string
		body []byte
		want string
	}{
		{"messages.getHistory", inBusinessConnection(t, "conn-1", history), "BUSINESS_CONNECTION_NOT_ALLOWED"},
		{"messages.uploadMedia wrapped", inBusinessConnection(t, "conn-1", upload), "BUSINESS_CONNECTION_NOT_ALLOWED"},
		{"invokeWithLayer around getHistory", inBusinessConnection(t, "conn-1",
			encodeTL(t, &mtproto.TLInvokeWithLayer{Constructor: mtproto.CRC32_invokeWithLayer, Layer: 233, Query: history})), "BUSINESS_CONNECTION_NOT_ALLOWED"},
		{"empty connection_id", inBusinessConnection(t, "", send), "BUSINESS_CONNECTION_INVALID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, sess := newTestLoggedInWrapper(t)
			msgId := testBindMsgId + 16
			processEncoded(t, sess, msgId, tc.body)
			if len(w.tmpRpcApiMessageList) != 0 {
				t.Fatalf("%d requests queued", len(w.tmpRpcApiMessageList))
			}
			rpcErr := rpcErrorInOutQueue(t, sess, msgId)
			if rpcErr == nil || rpcErr.Code() != 400 || rpcErr.Message() != tc.want {
				t.Fatalf("reply = %v, want 400 %s", rpcErr, tc.want)
			}
		})
	}
}

// A refused query changes nothing in the session: updates.getState through
// a connection does not make it the main updates session or stop its
// pushes.
func TestBusinessConnectionRefusedKeepsSync(t *testing.T) {
	getState := encodeTL(t, &mtproto.TLUpdatesGetState{Constructor: mtproto.CRC32_updates_getState})
	w, sess := newTestLoggedInWrapper(t)
	sess.canSync = true
	msgId := testBindMsgId + 16
	processEncoded(t, sess, msgId, inBusinessConnection(t, "conn-1", getState))
	if len(w.tmpRpcApiMessageList) != 0 {
		t.Fatalf("%d requests queued", len(w.tmpRpcApiMessageList))
	}
	if rpcErr := rpcErrorInOutQueue(t, sess, msgId); rpcErr == nil || rpcErr.Message() != "BUSINESS_CONNECTION_NOT_ALLOWED" {
		t.Fatalf("reply = %v, want BUSINESS_CONNECTION_NOT_ALLOWED", rpcErr)
	}
	if !sess.canSync || w.mainUpdatesSession != nil {
		t.Fatal("a refused updates.getState changed the session's updates")
	}
}

func TestBusinessConnectionMethods(t *testing.T) {
	for _, q := range []mtproto.TLObject{
		&mtproto.TLAccountUpdateProfile{}, &mtproto.TLMessagesDeleteMessages{}, &mtproto.TLMessagesEditMessage{},
		&mtproto.TLMessagesReadHistory{}, &mtproto.TLMessagesSendMedia{}, &mtproto.TLMessagesSendMessage{},
		&mtproto.TLMessagesSendMultiMedia{}, &mtproto.TLMessagesSetTyping{}, &mtproto.TLMessagesUpdatePinnedMessage{},
	} {
		if !businessConnectionMethod(q) {
			t.Errorf("%T is refused through a business connection", q)
		}
	}
	for _, q := range []mtproto.TLObject{
		&mtproto.TLMessagesGetHistory{}, &mtproto.TLMessagesUploadMedia{}, &mtproto.TLMessagesForwardMessages{},
		&mtproto.TLAccountUpdateUsername{}, &mtproto.TLUpdatesGetDifference{}, &mtproto.TLAccountSetGlobalPrivacySettings{},
	} {
		if businessConnectionMethod(q) {
			t.Errorf("%T is allowed through a business connection", q)
		}
	}
}
