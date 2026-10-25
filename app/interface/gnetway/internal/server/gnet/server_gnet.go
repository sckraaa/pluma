// Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
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

package gnet

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/rand"
	"net"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/interface/gnetway/internal/server/gnet/codec"
	httpcodec "github.com/teamgram/teamgram-server/app/interface/gnetway/internal/server/gnet/http"
	"github.com/teamgram/teamgram-server/app/interface/gnetway/internal/server/gnet/pp"
	"github.com/teamgram/teamgram-server/app/interface/gnetway/internal/server/gnet/ws"
	"github.com/teamgram/teamgram-server/app/interface/session/session"

	"github.com/gobwas/ws/wsutil"
	"github.com/panjf2000/gnet/v2"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/timex"
	"go.opentelemetry.io/otel"
)

// OnBoot fires when the engine is ready for accepting connections.
// The parameter engine has information and various utilities.
func (s *Server) OnBoot(eng gnet.Engine) (action gnet.Action) {
	logx.Infof("gnetway server is listening")
	s.eng = eng
	return gnet.None
}

// OnShutdown fires when the engine is being shut down, it is called right after
// all event-loops and connections are closed.
func (s *Server) OnShutdown(eng gnet.Engine) {
	_ = eng
	logx.Infof("gnetway server shutdown")
}

// OnOpen fires when a new connection has been opened.
// The parameter out is the return value which is going to be sent back to the peer.
func (s *Server) OnOpen(c gnet.Conn) (out []byte, action gnet.Action) {
	logx.Debugf("onNewConn - conn(%s)", c)

	ctx := newConnContext()
	if host, _, err := net.SplitHostPort(c.RemoteAddr().String()); err == nil {
		ctx.setClientIp(host)
	} else {
		ctx.setClientIp(c.RemoteAddr().String())
	}
	ctx.ppv1 = s.c.Gnetway.IsProxyProtocolV1(c.LocalAddr().String())
	// Behind PROXY protocol the real address comes with the header.
	if !ctx.ppv1 && !s.countConn(c, ctx) {
		c.SetContext(ctx)
		return nil, gnet.Close
	}
	ctx.tcp = s.c.Gnetway.IsTcp(c.LocalAddr().String())
	ctx.websocket = s.c.Gnetway.IsWebsocket(c.LocalAddr().String())
	ctx.http = s.c.Gnetway.IsHttp(c.LocalAddr().String())
	if ctx.websocket {
		ctx.wsCodec = new(ws.WsCodec)
	}
	if ctx.http {
		ctx.httpCodec = new(httpcodec.HttpCodec)
	}
	// Until a message decrypts with a known key, the connection must not
	// outlive this deadline, whatever it sends (see markAuthenticated).
	ctx.closeDate = s.CachedNow() + unauthenticatedConnTimeout
	s.timeoutWheel.Add(c.ConnId(), ctx.closeDate)
	c.SetContext(ctx)

	proto := "tcp"
	if ctx.websocket {
		proto = "websocket"
	} else if ctx.http {
		proto = "http"
	}
	metricConnOpen.Inc(proto)

	return
}

// OnClose fires when a connection has been closed.
// The parameter err is the last known connection error.
func (s *Server) OnClose(c gnet.Conn, err error) (action gnet.Action) {
	defer func() {
		// The connection is going away anyway: only keep the gateway up.
		if r := recover(); r != nil {
			logRecoveredPanic(c.ConnId(), "close", r)
		}
	}()
	logx.Debugf("onConnClosed - conn(%s), err: %v", c, err)

	ctx, _ := c.Context().(*connContext)
	if ctx == nil {
		return
	}
	if ctx.limitSubject != "" {
		s.limits.closeConn(ctx.limitSubject)
		ctx.limitSubject = ""
	}

	defer func() {
		s.timeoutWheel.Remove(c.ConnId(), ctx.closeDate)
		if ctx.wsCodec != nil {
			ctx.wsCodec.Conn.Release()
			ctx.wsCodec = nil
		}

		proto := "tcp"
		if ctx.websocket {
			proto = "websocket"
		} else if ctx.http {
			proto = "http"
		}
		metricConnClose.Inc(proto)

		c.SetContext(nil)
	}()

	// HTTP connections are transient - no persistent session to close
	if ctx.http {
		return
	}

	if ctx.authKey == nil {
		return
	}

	var (
		authKey   = ctx.authKey
		sessionId = ctx.sessionId
		clientIp  = ctx.clientIp
	)
	bDeleted := s.authSessionMgr.RemoveSession(authKey.AuthKeyId(), sessionId, c.ConnId())
	if !bDeleted {
		return
	}

	// Same routing as the data of this key (an unbound temporary key has its
	// own session wrapper).
	routeKey, permAuthKeyId := authKey.sessionRoute()
	if err := s.pool.Submit(func() {
		closeCtx, span := otel.Tracer("gnetway").Start(context.Background(), "SessionCloseSession")
		defer span.End()
		err := s.svcCtx.Dao.SessionDispatcher.CloseSession(closeCtx, routeKey, &session.TLSessionCloseSession{
			Client: session.MakeTLSessionClientEvent(&session.SessionClientEvent{
				ServerId:      s.svcCtx.GatewayId,
				AuthKeyId:     authKey.AuthKeyId(),
				KeyType:       int32(authKey.AuthKeyType()),
				PermAuthKeyId: permAuthKeyId,
				SessionId:     sessionId,
				ClientIp:      clientIp,
			}).To_SessionClientEvent(),
		})
		if err != nil {
			logx.Errorf("client.SessionCloseSession - error: %v", err)
		}
	}); err != nil {
		logx.Errorf("OnClose - pool.Submit error: %v, authKeyId: %d", err, authKey.AuthKeyId())
	}

	return
}

// Connection timeouts (seconds).
const (
	// unauthenticatedConnTimeout: a connection must complete the handshake
	// and send a message under a known auth key within this time after it
	// opened; traffic before that (handshake messages, WebSocket pings, a
	// slowly sent request) does not extend it.
	unauthenticatedConnTimeout = 60
	// idle timeouts of an authenticated connection, extended by any traffic.
	tcpIdleTimeout  = 300
	httpIdleTimeout = 60
)

// extendDeadline moves the idle deadline of an authenticated connection.
func (s *Server) extendDeadline(c gnet.Conn, ctx *connContext) {
	oldCloseDate := ctx.closeDate
	if ctx.http {
		ctx.closeDate = s.CachedNow() + httpIdleTimeout + rand.Int63()%10 //nolint:gosec // deadline jitter, not a secret
	} else {
		ctx.closeDate = s.CachedNow() + tcpIdleTimeout + rand.Int63()%10 //nolint:gosec // deadline jitter, not a secret
	}
	s.timeoutWheel.Move(c.ConnId(), oldCloseDate, ctx.closeDate)
}

// markAuthenticated records that a message on c decrypted with a known auth
// key: from now on any traffic keeps the connection open.
func (s *Server) markAuthenticated(c gnet.Conn, ctx *connContext) {
	if ctx.authenticated {
		return
	}
	ctx.authenticated = true
	s.extendDeadline(c, ctx)
}

// OnTraffic fires when a local socket receives data from the peer.
func (s *Server) OnTraffic(c gnet.Conn) (action gnet.Action) {
	defer recoverConn(c, "traffic", &action)

	ctx, ok := c.Context().(*connContext)
	if !ok || ctx == nil {
		return gnet.Close
	}
	if ctx.authenticated {
		s.extendDeadline(c, ctx)
	}
	if ctx.ppv1 {
		ppv1, err := c.Peek(-1)
		if err != nil {
			logx.Errorf("conn(%s) Peek fail: %v", c, err)
			return
		}
		if len(ppv1) < len(pp.V1Identifier) {
			// Never the bytes: past the PROXY header they are the client's traffic.
			logx.Debugf("conn(%s) ppv1: %d bytes, waiting for the PROXY header", c, len(ppv1))
			return
		}

		if bytes.HasPrefix(ppv1, pp.V1Identifier) {
			logx.Debugf("conn(%s) ppv1: %d bytes buffered", c, len(ppv1))

			r := bytes.NewReader(ppv1)
			h, err := pp.ReadHeader(r)
			if err != nil {
				logx.Errorf("conn(%s) ReadHeader error: %v", c, err)
				if r.Len() > 107 {
					return gnet.Close
				} else {
					return
				}
			}
			if host, _, err := net.SplitHostPort(h.Source.String()); err == nil {
				ctx.setClientIp(host)
			} else {
				ctx.setClientIp(h.Source.String())
			}
			if !s.countConn(c, ctx) {
				return gnet.Close
			}
			_, _ = c.Discard(len(ppv1) - r.Len())
			ctx.ppv1 = false

			if r.Len() == 0 {
				return
			}
		} else {
			ctx.ppv1 = false
		}
	}

	if ctx.http {
		return s.onHttpData(ctx, c)
	} else if ctx.websocket {
		return s.onWebsocketData(ctx, c)
	} else {
		return s.onTcpData(ctx, c)
	}
}

// OnTick fires immediately after the engine starts and will fire again
// following the duration specified by the delay return value.
func (s *Server) OnTick() (delay time.Duration, action gnet.Action) {
	s.tickNumber = s.tickNumber + 1
	if s.tickNumber%5 == 0 {
		logx.Statf("conn count: %d", s.eng.CountConnections())
	}
	delay = time.Second * 1
	now := time.Now().Unix()
	s.cachedNow.Store(now)

	// Use time wheel to expire only connections in the current slot
	// instead of iterating all connections.
	expiredConnIds := s.timeoutWheel.ExpireSlot(now)
	for _, connId := range expiredConnIds {
		s.trigger(connId, func(c gnet.Conn) {
			ctx, _ := c.Context().(*connContext)
			if ctx == nil {
				return
			}
			if now >= ctx.closeDate {
				logx.Errorf("close conn(%s) by timeout", c)
				metricConnTimeout.Inc()
				_ = c.Close()
			} else {
				// Connection was refreshed but still in old slot; re-add to new slot
				s.timeoutWheel.Add(connId, ctx.closeDate)
			}
		})
	}
	return
}

// computeQuickAckToken computes the Quick ACK token of a client message: the
// first 32 bits of msg_key_large = SHA256(auth_key[88:88+32] + plaintext),
// the plaintext with its padding (MTProto 2.0, x = 0), with the most
// significant bit set. Clients compute the same value when they send the
// message (Android Datacenter::createRequestsData, tdlib Transport) and match
// the acknowledgement by it, so it must not be computed over the ciphertext.
func computeQuickAckToken(authKey []byte, plaintext []byte) uint32 {
	h := sha256.New()
	h.Write(authKey[88 : 88+32])
	h.Write(plaintext)
	var sum [32]byte
	h.Sum(sum[:0])
	return binary.LittleEndian.Uint32(sum[:4]) | 0x80000000
}

// encryptedFrameMinSize is auth_key_id, msg_key and the smallest encrypted
// data: a 32-byte header and 12 bytes of padding, in 16-byte blocks.
const encryptedFrameMinSize = 8 + 16 + 48

var errEncryptedFrame = errors.New("encrypted message too short")

// splitEncryptedFrame returns msg_key and encrypted_data of an encrypted
// message. Bytes after the last whole 16-byte block (the padding of the
// padded intermediate transport) are not part of encrypted_data.
func splitEncryptedFrame(frame []byte) (msgKey, data []byte, err error) {
	if len(frame) < encryptedFrameMinSize {
		return nil, nil, errEncryptedFrame
	}
	data = frame[24:]
	return frame[8:24], data[:len(data)-len(data)%16], nil
}

func (s *Server) onEncryptedMessage(c gnet.Conn, ctx *connContext, authKey *authKeyUtil, needAck bool, mmsg []byte) error {
	since := timex.Now()
	defer func() {
		metricMsgProcess.ObserveFloat(timex.Since(since).Seconds()*1000, "encrypted")
	}()

	msgKey, encryptedData, err := splitEncryptedFrame(mmsg)
	if err != nil {
		logx.Errorf("conn(%s) encrypted message(%d bytes): %v", c, len(mmsg), err)
		return err
	}
	mtpRwaData, err := authKey.AesIgeDecrypt(msgKey, encryptedData)
	if err != nil {
		// Only ids and lengths: the message is ciphertext under someone's key
		// and anyone can send garbage with a known auth_key_id (audit #9).
		logx.Errorf("conn(%d) auth_key_id %d: encrypted message of %d bytes does not decrypt: %v",
			c.ConnId(), authKey.AuthKeyId(), len(mmsg), err)
		return err
	}
	s.markAuthenticated(c, ctx)

	// Send Quick ACK if requested by client.
	// The token MUST go through ctx.codec.EncodeQuickAck so that the CTR cipher
	// state is advanced correctly on obfuscated transports. Bypassing the codec
	// here would desynchronise the client's CTR counter and corrupt all subsequent
	// messages (causing decryptServerResponse failures on the Android client).
	if needAck {
		ackToken := computeQuickAckToken(authKey.AuthKey(), mtpRwaData)
		if ackData := ctx.codec.EncodeQuickAck(ackToken); len(ackData) > 0 {
			if ctx.websocket {
				_ = wsutil.WriteServerBinary(c, ackData)
			} else {
				_, _ = c.Write(ackData)
			}
		}
		metricQuickAck.Inc()
	}

	var (
		salt      = int64(binary.LittleEndian.Uint64(mtpRwaData))
		sessionId = int64(binary.LittleEndian.Uint64(mtpRwaData[8:]))
	)

	var (
		isNew    = ctx.sessionId != sessionId
		clientIp = ctx.clientIp
		connId   = c.ConnId()
	)
	if isNew {
		ctx.sessionId = sessionId
	} else {
		// check sessionId??
	}

	var connType int32
	if isNew && s.authSessionMgr.AddNewSession(authKey, sessionId, connId) {
		connType = 1 // signal session side to create session
	}

	// The session wrapper is chosen from the key data authsession returned,
	// never from the message: until authsession has verified
	// auth.bindTempAuthKey, a temporary key's messages go to its own
	// restricted wrapper, whatever perm_auth_key_id they name.
	routeKey, data := newSessionClientData(s.svcCtx.GatewayId, authKey, connType, clientIp, sessionId, salt, mtpRwaData[16:])

	if err := s.submit(connId, func() {
		defer func() {
			logx.WithDuration(timex.Since(since)).Infof("onEncryptedMessage: %s", c)
		}()

		ctx2, span := otel.Tracer("gnetway").Start(context.Background(), "SessionSendDataToSession")
		defer span.End()
		err := s.svcCtx.Dao.SessionDispatcher.SendData(ctx2, routeKey, &session.TLSessionSendDataToSession{
			Data: data,
		})
		if err != nil {
			logx.Errorf("session.sendDataToSession - error: %v", err)
		}
		// authsession no longer knows the key (destroy_auth_key,
		// auth.dropTempAuthKeys, the removal of an expired key, task 008):
		// it goes at once, not at the next recheck, and the client gets
		// -404 and creates a new key.
		if errors.Is(err, mtproto.ErrAuthKeyUnregistered) {
			s.RemoveAuthKey(authKey.AuthKeyId())
			s.trigger(connId, func(c gnet.Conn) {
				writeAuthKeyNotFound(c)
				_ = c.Close()
			})
		}
	}); err != nil {
		logx.Errorf("onEncryptedMessage - pool.Submit error: %v, connId: %d", err, connId)
	}

	return nil
}

// newSessionClientData builds session.sendDataToSession data for a decrypted
// message and returns the key to dispatch it by (authKeyUtil.sessionRoute).
func newSessionClientData(gatewayId string, authKey *authKeyUtil, connType int32, clientIp string, sessionId, salt int64, payload []byte) (int64, *session.SessionClientData) {
	routeKey, permAuthKeyId := authKey.sessionRoute()
	return routeKey, &session.SessionClientData{
		ServerId:      gatewayId,
		ConnType:      connType,
		AuthKeyId:     authKey.AuthKeyId(),
		KeyType:       int32(authKey.AuthKeyType()),
		PermAuthKeyId: permAuthKeyId,
		SessionId:     sessionId,
		Salt:          salt,
		Payload:       payload,
		ClientIp:      clientIp,
	}
}

// writeAuthKeyNotFound sends transport error -404: the server does not know
// the auth key (or a temporary key expired). Clients drop the key and create
// a new one.
func writeAuthKeyNotFound(c gnet.Conn) {
	out := &mtproto.MTPRawMessage{
		Payload: make([]byte, 4),
	}
	code := int32(-404)
	binary.LittleEndian.PutUint32(out.Payload, uint32(code))
	_ = UnThreadSafeWrite(c, out)
}

func (s *Server) GetConnCounts() int {
	return s.eng.CountConnections()
}

// connHandshakeIO returns how handshake messages of connection c are kept
// and answered.
func (s *Server) connHandshakeIO(c gnet.Conn, ctx *connContext) *handshakeIO {
	if ctx.handshakes == nil {
		ctx.handshakes = &handshakeIO{
			connId:   c.ConnId(),
			clientIp: ctx.clientIp,
			states:   newHandshakeStore(maxConnHandshakes, 0),
			reply: func(c gnet.Conn, answer mtproto.TLObject) {
				metricHandshake.Inc("ok")
				_ = UnThreadSafeWrite(c, &mtproto.MTPRawMessage{Payload: handshakePayload(answer)})
			},
			fail: func(c gnet.Conn, err error) {
				metricHandshake.Inc("error")
				logx.Errorf("conn(%d) handshake: %v", c.ConnId(), err)
				_ = c.Close()
			},
		}
	}
	return ctx.handshakes
}

// mtprotoFrameMinSize: every message starts with auth_key_id.
const mtprotoFrameMinSize = 8

func (s *Server) onMTPRawMessage(ctx *connContext, c gnet.Conn, needAck bool, msg2 []byte) gnet.Action {
	if len(msg2) < mtprotoFrameMinSize {
		logx.Errorf("conn(%d) frame of %d bytes", c.ConnId(), len(msg2))
		return gnet.Close
	}
	authKeyId := int64(binary.LittleEndian.Uint64(msg2))

	if authKeyId == 0 {
		out, _, err := s.onHandshakeMessage(s.connHandshakeIO(c, ctx), msg2)
		if err != nil {
			logx.Errorf("conn(%d) handshake: %v", c.ConnId(), err)
			metricHandshake.Inc("error")
			return gnet.Close
		} else if out != nil {
			metricHandshake.Inc("ok")
			_ = UnThreadSafeWrite(c, &mtproto.MTPRawMessage{Payload: handshakePayload(out)})
		}
	} else {
		if ctx.keyQuery != nil {
			// Messages wait, in order, behind the key read in progress.
			return s.queryAuthKey(c, ctx, authKeyId, nil, needAck, msg2)
		}

		now := s.CachedNow()
		authKey := ctx.getAuthKey()
		if authKey == nil {
			authKey = s.GetAuthKey(authKeyId)
			if authKey != nil {
				ctx.putAuthKey(authKey)
			}
		} else if authKey.AuthKeyId() != authKeyId {
			logx.Errorf("conn(%s) getAuthKey - error: invalid key id %d ", c, authKeyId)
			return gnet.Close
		}

		if authKey != nil && authKey.expired(now) {
			logx.Infof("conn(%s) temp auth key %d expired", c, authKeyId)
			s.RemoveAuthKey(authKeyId)
			writeAuthKeyNotFound(c)
			return gnet.Close
		}

		if authKey != nil && !authKey.needsRecheck(now) {
			err := s.onEncryptedMessage(c, ctx, authKey, needAck, msg2)
			if err != nil {
				logx.Errorf("conn(%s) onEncryptedMessage - error: %v ", c, err)
				return gnet.Close
			}
			return gnet.None
		}

		known := s.knownAuthKey(authKeyId, authKey)
		if known != nil && known.unboundReadsExhausted() {
			logx.Errorf("conn(%d) temp auth key %d still unbound after %d reads", c.ConnId(), authKeyId, maxUnboundTempKeyReads)
			writeAuthKeyNotFound(c)
			return gnet.Close
		}

		// Read the key (again) from authsession: it is not cached, it is a
		// temporary key due for a recheck, or an unbound temporary key whose
		// binding only authsession can confirm.
		return s.queryAuthKey(c, ctx, authKeyId, known, needAck, msg2)
	}

	return gnet.None
}

// Messages that wait on a connection while its auth key is read from
// authsession; beyond these limits the connection is closed.
const (
	maxPendingFrames     = 32
	maxPendingFrameBytes = codec.MAX_MTPRORO_FRAME_SIZE
)

// pendingKeyQuery is a read of the auth key of a connection from authsession
// in progress, with the messages waiting for it (in order). There is at most
// one per connection, so a stream of messages under unknown key ids costs one
// query at a time, not one per message.
type pendingKeyQuery struct {
	authKeyId int64
	frames    []pendingFrame
	bytes     int
}

type pendingFrame struct {
	needAck bool
	frame   []byte
}

func (s *Server) queryAuthKey(c gnet.Conn, ctx *connContext, authKeyId int64, known *authKeyUtil, needAck bool, frame []byte) gnet.Action {
	if len(frame) < encryptedFrameMinSize {
		logx.Errorf("conn(%d) encrypted message of %d bytes", c.ConnId(), len(frame))
		return gnet.Close
	}
	frame = append([]byte(nil), frame...)

	if q := ctx.keyQuery; q != nil {
		if q.authKeyId != authKeyId || len(q.frames) >= maxPendingFrames || q.bytes+len(frame) > maxPendingFrameBytes {
			logx.Errorf("conn(%d) too many messages while its auth key is read", c.ConnId())
			return gnet.Close
		}
		q.frames = append(q.frames, pendingFrame{needAck: needAck, frame: frame})
		q.bytes += len(frame)
		return gnet.None
	}

	q := &pendingKeyQuery{
		authKeyId: authKeyId,
		frames:    []pendingFrame{{needAck: needAck, frame: frame}},
		bytes:     len(frame),
	}
	ctx.keyQuery = q

	connId := c.ConnId()
	err := s.submit(connId, func() {
		key, err := s.queryKey(authKeyId, known)
		s.trigger(connId, func(c gnet.Conn) {
			ctx, _ := c.Context().(*connContext)
			if ctx == nil || ctx.keyQuery != q {
				return
			}
			ctx.keyQuery = nil
			if err != nil {
				if errors.Is(err, mtproto.ErrAuthKeyUnregistered) {
					s.RemoveAuthKey(authKeyId)
					writeAuthKeyNotFound(c)
				}
				logx.Errorf("conn(%d) sessionQueryAuthKey - error: %v", c.ConnId(), err)
				_ = c.Close()
				return
			}
			if key.unboundReadsExhausted() {
				// Kept in the cache: later messages get -404 without a read.
				logx.Errorf("conn(%d) temp auth key %d still unbound after %d reads", c.ConnId(), authKeyId, maxUnboundTempKeyReads)
				writeAuthKeyNotFound(c)
				_ = c.Close()
				return
			}
			ctx.putAuthKey(key)
			for _, f := range q.frames {
				if err := s.onEncryptedMessage(c, ctx, key, f.needAck, f.frame); err != nil {
					logx.Errorf("conn(%s) onEncryptedMessage - error: %v ", c, err)
					_ = c.Close()
					return
				}
			}
		})
	})
	if err != nil {
		ctx.keyQuery = nil
		logx.Errorf("conn(%d) queryAuthKey - pool.Submit error: %v", connId, err)
		return gnet.Close
	}
	return gnet.None
}

// knownAuthKey returns the gateway's latest copy of a key: the cached one,
// else the one the connection holds (connKey, may be nil).
func (s *Server) knownAuthKey(authKeyId int64, connKey *authKeyUtil) *authKeyUtil {
	if k := s.GetAuthKey(authKeyId); k != nil {
		return k
	}
	return connKey
}

// fetchAuthKey reads an auth key from authsession and caches it. known is the
// copy the gateway had, if any (checkQueriedAuthKey).
func (s *Server) fetchAuthKey(authKeyId int64, known *authKeyUtil) (*authKeyUtil, error) {
	queryCtx, span := otel.Tracer("gnetway").Start(context.Background(), "SessionQueryAuthKey")
	defer span.End()
	keyInfo, err := s.svcCtx.Dao.SessionDispatcher.QueryAuthKey(queryCtx, authKeyId, &session.TLSessionQueryAuthKey{
		AuthKeyId: authKeyId,
	})
	if err != nil {
		return nil, err
	}

	key, err := checkQueriedAuthKey(authKeyId, keyInfo, s.CachedNow(), known)
	if err != nil {
		return nil, err
	}
	s.PutAuthKey(key)
	return key, nil
}

func UnThreadSafeWrite(c gnet.Conn, msg interface{}) error {
	ctx := c.Context().(*connContext)

	if ctx.codec == nil {
		logx.Errorf("conn(%s) c.Write(data) - error: ctx.codec == nil ", c)
		return nil
	}

	if msg == nil {
		logx.Errorf("conn(%s) c.Write(data) - error: msg == ni ", c)
		return nil
	}

	data, err := ctx.codec.Encode(c, msg)
	if err != nil {
		logx.Errorf("conn(%s) ctx.codec.Encode(c, msg) - error: %v ", c, err)
		return err
	}

	if ctx.websocket {
		// This is the echo server
		err = wsutil.WriteServerBinary(c, data)
		if err != nil {
			logx.Errorf("conn[%v] [err=%v]", c.RemoteAddr().String(), err.Error())
			return err
		}
	} else {
		_, err = c.Write(data)
		if err != nil {
			logx.Errorf("conn(%s) c.Write(data) - error: %v ", c, err)
		}
	}

	return nil
}
