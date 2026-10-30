// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package sess

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/teamgram/proto/mtproto"
	rpcmetadata "github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/pkg/pushdevices"

	"github.com/zeromicro/go-zero/core/contextx"
	"github.com/zeromicro/go-zero/core/logx"
)

// maxInvokeNesting bounds the invoke*/initConnection wrappers around one
// query. Official clients use a few, e.g. invokeAfterMsg(invokeWithLayer(
// initConnection(invokeWithoutUpdates(query)))). Each level is decoded and
// handled recursively, so without a bound a single message of nested
// wrappers costs memory quadratic in its size and can exhaust the goroutine
// stack, before any login.
const maxInvokeNesting = 10

var (
	errInvokeTooDeep = errors.New("invoke wrappers nested too deeply")
	errInvokeNoQuery = errors.New("invoke wrapper without a query")
)

// invokeChain is where a query sits in its invoke* wrappers: the nesting
// level and the buffer the enclosing wrapper was decoded from, whose nesting
// depth and allocation budget the query's decoding continues
// (mtproto.DecodeBuf.NewNestedDecodeBuf), so the decoder limits hold for the
// whole chain.
type invokeChain struct {
	level  int
	parent *mtproto.DecodeBuf
}

// decodeQuery decodes the query of a wrapper found at k.
func (k invokeChain) decodeQuery(query []byte) (mtproto.TLObject, invokeChain, error) {
	if k.level >= maxInvokeNesting {
		return nil, k, errInvokeTooDeep
	}
	if len(query) == 0 {
		return nil, k, errInvokeNoQuery
	}
	var dBuf *mtproto.DecodeBuf
	if k.parent == nil {
		dBuf = mtproto.NewDecodeBuf(query)
	} else {
		dBuf = k.parent.NewNestedDecodeBuf(query)
	}
	obj := dBuf.Object()
	if err := dBuf.GetError(); err != nil {
		return nil, k, err
	}
	if obj == nil {
		return nil, k, errInvokeNoQuery
	}
	return obj, invokeChain{level: k.level + 1, parent: dBuf}, nil
}

// decodeInvokeQuery decodes the query of the wrapper method received as
// msgId. Wrappers nested deeper than maxInvokeNesting are answered with
// 400 INPUT_REQUEST_INVALID.
func (c *session) decodeInvokeQuery(ctx context.Context, gatewayId string, msgId *inboxMsg, method string, query []byte, chain invokeChain) (mtproto.TLObject, invokeChain, bool) {
	obj, next, err := chain.decodeQuery(query)
	switch {
	case err == nil:
		return obj, next, true
	case errors.Is(err, errInvokeTooDeep):
		logx.WithContext(ctx).Errorf("%s - msg_id %d: invoke wrappers nested deeper than %d", method, msgId.msgId, maxInvokeNesting)
		c.sendRpcResultToQueue(ctx, gatewayId, msgId.msgId, mtproto.NewRpcError(mtproto.ErrInputRequestInvalid))
		msgId.state = RECEIVED | RESPONSE_GENERATED
	default:
		logx.WithContext(ctx).Errorf("%s - msg_id %d: query: %v", method, msgId.msgId, err)
	}
	return nil, chain, false
}

func (c *session) onInvokeWithLayer(ctx context.Context, gatewayId, clientIp string, msgId *inboxMsg, request *mtproto.TLInvokeWithLayer, chain invokeChain) {
	logx.WithContext(ctx).Infof("onInvokeWithLayer - request data: {sess: %s, conn_id: %s, msg_id: %d, seq_no: %d, layer: %d}",
		c,
		gatewayId,
		msgId.msgId,
		msgId.seqNo,
		request.GetLayer())

	query, next, ok := c.decodeInvokeQuery(ctx, gatewayId, msgId, "invokeWithLayer", request.GetQuery(), chain)
	if !ok {
		return
	}

	c.sessList.cb.onUpdateLayer(ctx, request.Layer)

	c.processMsg(ctx, gatewayId, clientIp, msgId, query, next)
}

func (c *session) onInvokeAfterMsg(ctx context.Context, gatewayId, clientIp string, msgId *inboxMsg, request *mtproto.TLInvokeAfterMsg, chain invokeChain) {
	logx.WithContext(ctx).Infof("onInvokeAfterMsg - request data: {sess: %s, conn_id: %s, msg_id: %d, seq_no: %d}",
		c,
		gatewayId,
		msgId.msgId,
		msgId.seqNo)

	query, next, ok := c.decodeInvokeQuery(ctx, gatewayId, msgId, "invokeAfterMsg", request.GetQuery(), chain)
	if !ok {
		return
	}

	c.processMsg(ctx, gatewayId, clientIp, msgId, query, next)
}

func (c *session) onInvokeAfterMsgs(ctx context.Context, gatewayId, clientIp string, msgId *inboxMsg, request *mtproto.TLInvokeAfterMsgs, chain invokeChain) {
	logx.WithContext(ctx).Infof("onInvokeAfterMsgs - request data: {sess: %s, conn_id: %s, msg_id: %d, seq_no: %d}",
		c,
		gatewayId,
		msgId.msgId,
		msgId.seqNo)

	query, next, ok := c.decodeInvokeQuery(ctx, gatewayId, msgId, "invokeAfterMsgs", request.GetQuery(), chain)
	if !ok {
		return
	}

	/*
		if invokeAfterMsgs.GetQuery() == nil {
			logx.Errorf("invokeAfterMsgs Query is nil, query: {%v}", invokeAfterMsgs)
			return
		}

		dbuf := mtproto.NewDecodeBuf(invokeAfterMsgs.Query)
		query := dbuf.Object()
		if query == nil {
			logx.Errorf("Decode query error: %s", hex.EncodeToString(invokeAfterMsgs.Query))
			return
		}

		if len(invokeAfterMsgs.MsgIds) == 0 {
			// TODO(@benqi): invalid msgIds, ignore??

			messages[i].Object = query
		} else {
			var maxMsgId = invokeAfterMsgs.MsgIds[0]
			for j := 1; j < len(invokeAfterMsgs.MsgIds); j++ {
				if maxMsgId > invokeAfterMsgs.MsgIds[j] {
					maxMsgId = invokeAfterMsgs.MsgIds[j]
				}
			}


			var found = false
			for j := 0; j < i; j++ {
				if messages[j].MsgId == maxMsgId {
					messages[i].Object = query
					found = true
					break
				}
			}

			if !found {
				for j := i + 1; j < len(messages); j++ {
					if messages[j].MsgId == maxMsgId {
						// c.messages[i].Object = query
						messages[i].Object = query
						found = true
						messages = append(messages, messages[i])

						// set messages[i] = nil, will ignore this.
						messages[i] = nil
						break
					}
				}
			}

			if !found {
				// TODO(@benqi): backup message, wait.

				messages[i].Object = query
			}
		}
	*/

	c.processMsg(ctx, gatewayId, clientIp, msgId, query, next)
}

func (c *session) onInvokeWithoutUpdates(ctx context.Context, gatewayId, clientIp string, msgId *inboxMsg, request *mtproto.TLInvokeWithoutUpdates, chain invokeChain) {
	logx.WithContext(ctx).Infof("onInvokeWithoutUpdates - request data: {sess: %s, conn_id: %s, msg_id: %d, seq_no: %d, request: {%s}}",
		c,
		gatewayId,
		msgId.msgId,
		msgId.seqNo,
		reflect.TypeOf(request))

	query, next, ok := c.decodeInvokeQuery(ctx, gatewayId, msgId, "invokeWithoutUpdates", request.GetQuery(), chain)
	if !ok {
		return
	}

	c.processMsg(ctx, gatewayId, clientIp, msgId, query, next)
}

func (c *session) onInvokeWithMessagesRange(ctx context.Context, gatewayId, clientIp string, msgId *inboxMsg, request *mtproto.TLInvokeWithMessagesRange, chain invokeChain) {
	logx.WithContext(ctx).Infof("onInvokeWithMessagesRange - request data: {sess: %s, conn_id: %s, msg_id: %d, seq_no: %d, request: {%s}}",
		c,
		gatewayId,
		msgId.msgId,
		msgId.seqNo,
		reflect.TypeOf(request))

	query, _, ok := c.decodeInvokeQuery(ctx, gatewayId, msgId, "invokeWithMessagesRange", request.GetQuery(), chain)
	if !ok {
		return
	}

	c.onRpcRequest(ctx, gatewayId, clientIp, msgId, query, newTakeoutMetadata(0, request.GetRange()))
}

func (c *session) onInvokeWithTakeout(ctx context.Context, gatewayId, clientIp string, msgId *inboxMsg, request *mtproto.TLInvokeWithTakeout, chain invokeChain) {
	logx.WithContext(ctx).Infof("onInvokeWithTakeout - request data: {sess: %s, conn_id: %s, msg_id: %d, seq_no: %d, request: {%s}}",
		c,
		gatewayId,
		msgId.msgId,
		msgId.seqNo,
		reflect.TypeOf(request))

	query, _, ok := c.decodeInvokeQuery(ctx, gatewayId, msgId, "invokeWithTakeout", request.GetQuery(), chain)
	if !ok {
		return
	}

	query, takeout, err := newTakeoutGuard().ValidateWrappedQuery(request.GetTakeoutId(), query)
	if err != nil {
		if rpcErr, ok := err.(*mtproto.TLRpcError); ok {
			c.sendRpcResultToQueue(ctx, gatewayId, msgId.msgId, rpcErr)
			msgId.state = RECEIVED | RESPONSE_GENERATED
			return
		}

		c.sendRpcResultToQueue(ctx, gatewayId, msgId.msgId, mtproto.NewRpcError(mtproto.ErrInternalServerError))
		msgId.state = RECEIVED | RESPONSE_GENERATED
		return
	}

	c.onRpcRequest(ctx, gatewayId, clientIp, msgId, query, takeout)
}

func (c *session) onInvokeWithBusinessConnection(ctx context.Context, gatewayId, clientIp string, msgId *inboxMsg, request *mtproto.TLInvokeWithBusinessConnection, chain invokeChain) {
	logx.WithContext(ctx).Infof("onInvokeWithBusinessConnection - request data: {sess: %s, conn_id: %s, msg_id: %d, seq_no: %d, request: {%s}}",
		c,
		gatewayId,
		msgId.msgId,
		msgId.seqNo,
		reflect.TypeOf(request))

	query, next, ok := c.decodeInvokeQuery(ctx, gatewayId, msgId, "invokeWithBusinessConnection", request.GetQuery(), chain)
	if !ok {
		return
	}

	c.processMsg(ctx, gatewayId, clientIp, msgId, query, next)
}

func (c *session) onInvokeWithGooglePlayIntegrity(ctx context.Context, gatewayId, clientIp string, msgId *inboxMsg, request *mtproto.TLInvokeWithGooglePlayIntegrity, chain invokeChain) {
	logx.WithContext(ctx).Infof("onInvokeWithGooglePlayIntegrity - request data: {sess: %s, conn_id: %s, msg_id: %d, seq_no: %d, request: {%s}}",
		c,
		gatewayId,
		msgId.msgId,
		msgId.seqNo,
		reflect.TypeOf(request))

	query, next, ok := c.decodeInvokeQuery(ctx, gatewayId, msgId, "invokeWithGooglePlayIntegrity", request.GetQuery(), chain)
	if !ok {
		return
	}

	c.processMsg(ctx, gatewayId, clientIp, msgId, query, next)
}

// onInvokeWithApnsSecret
func (c *session) onInvokeWithApnsSecret(ctx context.Context, gatewayId, clientIp string, msgId *inboxMsg, request *mtproto.TLInvokeWithApnsSecret, chain invokeChain) {
	logx.WithContext(ctx).Infof("onInvokeWithApnsSecret - request data: {sess: %s, conn_id: %s, msg_id: %d, seq_no: %d, request: {%s}}",
		c,
		gatewayId,
		msgId.msgId,
		msgId.seqNo,
		reflect.TypeOf(request))

	query, next, ok := c.decodeInvokeQuery(ctx, gatewayId, msgId, "invokeWithApnsSecret", request.GetQuery(), chain)
	if !ok {
		return
	}

	c.processMsg(ctx, gatewayId, clientIp, msgId, query, next)
}

// onInvokeWithReCaptcha
func (c *session) onInvokeWithReCaptcha(ctx context.Context, gatewayId, clientIp string, msgId *inboxMsg, request *mtproto.TLInvokeWithReCaptcha, chain invokeChain) {
	logx.WithContext(ctx).Infof("onInvokeWithReCaptcha - request data: {sess: %s, conn_id: %s, msg_id: %d, seq_no: %d, request: {%s}}",
		c,
		gatewayId,
		msgId.msgId,
		msgId.seqNo,
		reflect.TypeOf(request))

	query, next, ok := c.decodeInvokeQuery(ctx, gatewayId, msgId, "invokeWithReCaptcha", request.GetQuery(), chain)
	if !ok {
		return
	}

	c.processMsg(ctx, gatewayId, clientIp, msgId, query, next)
}

func (c *session) onInitConnection(ctx context.Context, gatewayId, clientIp string, msgId *inboxMsg, request *mtproto.TLInitConnection, chain invokeChain) {
	logx.WithContext(ctx).Infof("onInitConnection - request data: {sess: %s, conn_id: %s, msg_id: %d, seq_no: %d, request: {%s}}",
		c,
		gatewayId,
		msgId.msgId,
		msgId.msgId,
		reflect.TypeOf(request))

	c.sessList.cb.onUpdateInitConnection(ctx, clientIp, request)

	query, next, ok := c.decodeInvokeQuery(ctx, gatewayId, msgId, "initConnection", request.GetQuery(), chain)
	if !ok {
		return
	}

	c.processMsg(ctx, gatewayId, clientIp, msgId, query, next)
}

func (c *session) onRpcRequest(ctx context.Context, gatewayId, clientIp string, msgId *inboxMsg, query mtproto.TLObject, takeout *rpcmetadata.Takeout) bool {
	logx.WithContext(ctx).Infof("onRpcRequest - request data: {sess: %s, gatewayId: %s, msg_id: %d, seq_no: %d, request: {%s}}",
		c,
		gatewayId,
		msgId.msgId,
		msgId.seqNo,
		reflect.TypeOf(query))

	if c.sessList.cb.unboundTempKey && !checkRpcWithUnboundTempKey(query) {
		c.sendRpcResultToQueue(ctx, gatewayId, msgId.msgId, mtproto.NewRpcError(mtproto.ErrAuthKeyPermEmpty))
		msgId.state = RECEIVED | RESPONSE_GENERATED
		return false
	}

	// TODO(@benqi): sync AuthUserId??
	//requestMessage := &mtproto.TLMessage2{
	//	MsgId:  msgId.msgId,
	//	Seqno:  msgId.seqNo,
	//	Object: object,
	//}

	switch query.(type) {
	case *mtproto.TLAccountRegisterDevice:
		registerDevice, _ := query.(*mtproto.TLAccountRegisterDevice)
		// Android's MTProto push session (ADR 0024). The token is the
		// session id as an unsigned decimal: ParseInt rejected every id
		// with the high bit set and bound session 0 instead.
		if registerDevice.TokenType == pushdevices.TokenMTProto {
			if pushSessionId, ok := pushdevices.PushSessionId(registerDevice.GetToken()); ok {
				c.sessList.cb.onBindPushSessionId(ctx, c.sessList, pushSessionId)
			}
		}
	case *mtproto.TLUpdatesGetState:
		c.sessList.cb.onSetMainUpdatesSession(ctx, c)
		c.canSync = false
	case *mtproto.TLUpdatesGetDifference:
		c.sessList.cb.onSetMainUpdatesSession(ctx, c)
		c.canSync = false
		//
	case *mtproto.TLUpdatesGetChannelDifference:
		c.sessList.cb.onSetMainUpdatesSession(ctx, c)
		c.canSync = false
		//case *mtproto.TLAuthBindTempAuthKey:
		//	res, err := c.AuthSessionRpcClient.AuthBindTempAuthKey(context.Background(), query.(*mtproto.TLAuthBindTempAuthKey))
		//	if err != nil {
		//		logx.WithContext(ctx).Errorf("bindTempAuthKey error - %v", err)
		//		err = mtproto.ErrInternalServerError
		//		c.sendRpcResultToQueue(gatewayId, msgId.msgId, &mtproto.RpcError{
		//			ErrorCode:    500,
		//			ErrorMessage: "INTERNAL_SERVER_ERROR",
		//		})
		//	} else {
		//		c.sendRpcResultToQueue(gatewayId, msgId.msgId, res)
		//	}
		//	msgId.state = RECEIVED | RESPONSE_GENERATED
		//	return false

		//case *mtproto.TLAuthSignIn:
		//	if !c.isGeneric {
		//		c.isGeneric = true
		//		c.cb.setOnline()
		//	}
		//case *mtproto.TLAuthSignUp:
		//	if !c.isGeneric {
		//		c.isGeneric = true
		//		c.cb.setOnline()
		//	}
	case *mtproto.TLAccountUpdateStatus:
		c.sessList.cb.onSetMainUpdatesSession(ctx, c)
	case *mtproto.TLUsersGetUsers:
		// logx.WithContext(ctx).Infof("user.getUsers: %s", query)
	}

	if shouldRefreshAuthState(c.sessList.cb.state) {
		c.sessList.cb.refreshAuthStateIfNeeded(ctx)
	}

	switch c.sessList.cb.state {
	case mtproto.AuthStateNormal:
		// state is ok
	case mtproto.AuthStateNeedPassword:
		if !checkRpcWhilePasswordNeeded(query) {
			c.sendRpcResultToQueue(ctx, gatewayId, msgId.msgId, mtproto.NewRpcError(mtproto.ErrSessionPasswordNeeded))
			msgId.state = RECEIVED | RESPONSE_GENERATED
			return false
		}
	default:
		if !checkRpcWithoutLogin(query) {
			c.sendRpcResultToQueue(ctx, gatewayId, msgId.msgId, mtproto.NewRpcError(rpcErrorForAuthState(c.sessList.cb.state)))
			msgId.state = RECEIVED | RESPONSE_GENERATED
			return false
		}
	}

	// A key's requests are limited (task 008): a runaway or hostile client
	// waits instead of loading the services.
	if wait := c.sessList.cb.rpcFloodWait(time.Now()); wait > 0 {
		c.sendRpcResultToQueue(ctx, gatewayId, msgId.msgId, mtproto.NewRpcError(mtproto.NewErrFloodWaitX(wait)))
		msgId.state = RECEIVED | RESPONSE_GENERATED
		return false
	}

	msgId.state = RECEIVED | RPC_PROCESSING

	c.sessList.cb.tmpRpcApiMessageList = append(
		c.sessList.cb.tmpRpcApiMessageList,
		&rpcApiMessage{
			ctx:       contextx.ValueOnlyFrom(ctx),
			sessList:  c.sessList,
			sessionId: c.sessionId,
			clientIp:  clientIp,
			reqMsgId:  msgId.msgId,
			reqMsg:    query,
			takeout:   takeout,
		})

	return true
}

// onLoginError moves the key to AuthStateNeedPassword when a login
// (auth.signIn, auth.sendCode with a future auth token, auth.exportLoginToken)
// answers SESSION_PASSWORD_NEEDED: bff did not bind the key, its login waits
// for auth.checkPassword (pkg/twofa). The state carries no user, so the
// methods allowed meanwhile run as a key that is not logged in. It never
// overwrites Normal/Logout/Deleted, should results arrive out of order
// (e.g. a delayed error after a successful auth.checkPassword).
func (c *session) onLoginError(ctx context.Context, rpcErr *mtproto.TLRpcError) {
	if !isSessionPasswordNeeded(rpcErr) {
		return
	}
	switch c.sessList.cb.state {
	case mtproto.AuthStateNormal, mtproto.AuthStateLogout, mtproto.AuthStateDeleted, mtproto.AuthStateUnknown:
		return
	}
	c.sessList.cb.changeAuthState(ctx, mtproto.AuthStateNeedPassword, int64(0))
}

func isSessionPasswordNeeded(rpcErr *mtproto.TLRpcError) bool {
	return rpcErr != nil && rpcErr.GetErrorCode() == int32(mtproto.ErrUnauthorized) && rpcErr.Message() == "SESSION_PASSWORD_NEEDED"
}

// onLoggedIn moves the key to AuthStateNormal for the user of a successful
// login result (auth.authorization).
func (c *session) onLoggedIn(ctx context.Context, rpcResult *rpcApiMessage) {
	if rpcResult.rpcResult == nil || rpcResult.rpcResult.Result == nil {
		return
	}
	authAuthorization, _ := rpcResult.rpcResult.Result.(*mtproto.Auth_Authorization)
	if authAuthorization != nil && authAuthorization.GetPredicateName() == mtproto.Predicate_auth_authorization && authAuthorization.GetUser() != nil {
		c.sessList.cb.changeAuthState(ctx, mtproto.AuthStateNormal, authAuthorization.GetUser().GetId())
	}
}

func (c *session) onRpcResult(ctx context.Context, rpcResult *rpcApiMessage) {
	rpcErr, _ := rpcResult.TryGetRpcResultError()
	if rpcErr != nil && rpcErr.GetErrorCode() == int32(mtproto.ErrNotReturnClient) {
		logx.WithContext(ctx).Debugf("recv NOTRETURN_CLIENT")
		c.pendingQueue.Add(rpcResult.reqMsgId)
		return
	}

	hasCanSync := false
	switch request := rpcResult.reqMsg.(type) {
	case *mtproto.TLAuthBindTempAuthKey:
		// A failed binding changes no state: the client retries or creates a
		// new temporary key (https://core.telegram.org/api/pfs), and the
		// wrapper is never another key's to tear down.
		_ = request
		if rpcErr == nil {
			c.sessList.changeAuthState(mtproto.AuthStatePermBound)
			// In the wrapper of the key while it was unbound: what it sent
			// before is acknowledged to the permanent key's wrapper now;
			// the binding's answer, still to be sent, goes once
			// (updatePendingStates).
			if c.sessList.cb.unboundTempKey {
				for _, sess := range c.sessList.sessions {
					sess.outQueue.RemoveSent()
				}
			}
		}
	case *mtproto.TLAuthLogOut:
		c.sessList.cb.changeAuthState(ctx, mtproto.AuthStateLogout, 0)
	case *mtproto.TLAuthSendCode:
		if rpcErr == nil && rpcResult.rpcResult != nil && rpcResult.rpcResult.Result != nil {
			authSentCode, _ := rpcResult.rpcResult.Result.(*mtproto.Auth_SentCode)
			if authSentCode != nil && authSentCode.GetPredicateName() == mtproto.Predicate_auth_sentCodeSuccess {
				if auth := authSentCode.GetAuthorization(); auth != nil && auth.GetUser() != nil {
					c.sessList.cb.changeAuthState(ctx, mtproto.AuthStateNormal, auth.GetUser().GetId())
				}
			}
		} else {
			c.onLoginError(ctx, rpcErr)
		}
	case *mtproto.TLAuthExportLoginToken:
		if rpcErr == nil && rpcResult.rpcResult != nil && rpcResult.rpcResult.Result != nil {
			authLoginToken, _ := rpcResult.rpcResult.Result.(*mtproto.Auth_LoginToken)
			if authLoginToken != nil && authLoginToken.GetPredicateName() == mtproto.Predicate_auth_loginTokenSuccess {
				if auth := authLoginToken.GetAuthorization(); auth != nil && auth.GetUser() != nil {
					c.sessList.cb.changeAuthState(ctx, mtproto.AuthStateNormal, auth.GetUser().GetId())
				}
			}
		} else {
			c.onLoginError(ctx, rpcErr)
		}
	case *mtproto.TLAuthSignIn:
		if rpcErr == nil {
			c.onLoggedIn(ctx, rpcResult)
		} else {
			c.onLoginError(ctx, rpcErr)
		}
	case *mtproto.TLAuthSignUp:
		if rpcErr == nil && rpcResult.rpcResult != nil && rpcResult.rpcResult.Result != nil {
			authAuthorization, _ := rpcResult.rpcResult.Result.(*mtproto.Auth_Authorization)
			if authAuthorization != nil && authAuthorization.GetPredicateName() == mtproto.Predicate_auth_authorization && authAuthorization.GetUser() != nil {
				c.sessList.cb.changeAuthState(ctx, mtproto.AuthStateNormal, authAuthorization.GetUser().GetId())
			}
		}
	case *mtproto.TLAuthFinishPasskeyLogin:
		if rpcErr == nil && rpcResult.rpcResult != nil && rpcResult.rpcResult.Result != nil {
			authAuthorization, _ := rpcResult.rpcResult.Result.(*mtproto.Auth_Authorization)
			if authAuthorization != nil && authAuthorization.GetPredicateName() == mtproto.Predicate_auth_authorization && authAuthorization.GetUser() != nil {
				c.sessList.cb.changeAuthState(ctx, mtproto.AuthStateNormal, authAuthorization.GetUser().GetId())
			}
		}
	case *mtproto.TLAuthImportAuthorization:
		if rpcErr == nil && c.sessList.cb.state == mtproto.AuthStateNeedPassword && rpcResult.rpcResult != nil && rpcResult.rpcResult.Result != nil {
			authAuthorization, _ := rpcResult.rpcResult.Result.(*mtproto.Auth_Authorization)
			if authAuthorization != nil && authAuthorization.GetPredicateName() == mtproto.Predicate_auth_authorization && authAuthorization.GetUser() != nil {
				c.sessList.cb.changeAuthState(ctx, mtproto.AuthStateNormal, authAuthorization.GetUser().GetId())
			}
		}
	case *mtproto.TLAuthCheckPassword:
		// bff binds the key before it answers auth.authorization, also when
		// this wrapper missed SESSION_PASSWORD_NEEDED (a restart of session).
		// A login that waited too long (AUTH_KEY_UNREGISTERED) starts over.
		switch {
		case rpcErr == nil:
			c.onLoggedIn(ctx, rpcResult)
		case c.sessList.cb.state == mtproto.AuthStateNeedPassword && rpcErr.Message() == "AUTH_KEY_UNREGISTERED":
			c.sessList.cb.changeAuthState(ctx, mtproto.AuthStateUnauthorized, int64(0))
		}
	case *mtproto.TLAuthRecoverPassword:
		if rpcErr == nil && c.sessList.cb.state == mtproto.AuthStateNeedPassword && rpcResult.rpcResult != nil && rpcResult.rpcResult.Result != nil {
			authAuthorization, _ := rpcResult.rpcResult.Result.(*mtproto.Auth_Authorization)
			if authAuthorization != nil && authAuthorization.GetPredicateName() == mtproto.Predicate_auth_authorization && authAuthorization.GetUser() != nil {
				c.sessList.cb.changeAuthState(ctx, mtproto.AuthStateNormal, authAuthorization.GetUser().GetId())
			}
		}
	case *mtproto.TLUpdatesGetState:
		hasCanSync = true
	case *mtproto.TLUpdatesGetDifference:
		hasCanSync = true
	case *mtproto.TLUpdatesGetChannelDifference:
		hasCanSync = true
	default:
	}

	// MoveRpcResult() clears rpcResult.rpcResult; duplicate delivery would get nil — guard to avoid sendRpcResult(nil).
	rpcResultData := rpcResult.MoveRpcResult()
	if rpcResultData != nil {
		c.sendRpcResult(ctx, rpcResultData)
	}
	if hasCanSync {
		c.canSync = true
		gatewayId := c.getGatewayId()
		c.sendQueueToGateway(ctx, gatewayId)
	}
}

func (c *session) sendRpcResult(ctx context.Context, rpcResult *mtproto.TLRpcResult) {
	if rpcResult == nil {
		return
	}
	msgId := c.inQueue.Lookup(rpcResult.ReqMsgId)
	if msgId == nil {
		logx.WithContext(ctx).Errorf("not found msgId, maybe removed: %d", rpcResult.ReqMsgId)
		return
	}

	gatewayId := c.getGatewayId()
	c.sendRpcResultToQueue(ctx, gatewayId, msgId.msgId, rpcResult.Result)
	msgId.state = RECEIVED | ACKNOWLEDGED

	if gatewayId == "" {
		logx.WithContext(ctx).Errorf("gatewayId is empty, send delay...")
	} else {
		c.sendQueueToGateway(ctx, gatewayId)
	}
}
