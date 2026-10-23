// Copyright 2024 Teamgram Authors
//  All rights reserved.
//
// Author: Benqi (wubenqi@gmail.com)
//

package gnet

import (
	"context"
	"encoding/binary"
	"errors"

	"github.com/teamgram/proto/mtproto"
	httpcodec "github.com/teamgram/teamgram-server/app/interface/gnetway/internal/server/gnet/http"
	"github.com/teamgram/teamgram-server/app/interface/session/session"

	"github.com/panjf2000/gnet/v2"
	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
)

var validPaths = map[string]bool{
	"/api":           true,
	"/apiw1":         true,
	"/apiw1_test":    true,
	"/apiw1_premium": true,
}

func (s *Server) onHttpData(ctx *connContext, c gnet.Conn) gnet.Action {
	if action := ctx.httpCodec.ReadBufferBytes(c); action != gnet.None {
		return action
	}

	body, done, err := ctx.httpCodec.ParseRequest()
	if err != nil {
		logx.Errorf("conn(%s) HTTP parse error: %v", c, err)
		_, _ = c.Write(httpcodec.FormatErrorResponse(400, "Bad Request"))
		return gnet.Close
	}
	if !done {
		return gnet.None
	}

	method := ctx.httpCodec.Method()
	path := ctx.httpCodec.Path()

	// Handle OPTIONS preflight
	if method == "OPTIONS" {
		_, _ = c.Write(httpcodec.FormatCORSResponse())
		ctx.httpCodec.Reset()
		return gnet.None
	}

	// Validate method
	if method != "POST" {
		_, _ = c.Write(httpcodec.FormatErrorResponse(405, "Method Not Allowed"))
		return gnet.Close
	}

	// Validate path
	if !validPaths[path] {
		_, _ = c.Write(httpcodec.FormatErrorResponse(404, "Not Found"))
		return gnet.Close
	}

	// Empty body
	if len(body) < 8 {
		_, _ = c.Write(httpcodec.FormatErrorResponse(400, "Bad Request"))
		return gnet.Close
	}

	authKeyId := int64(binary.LittleEndian.Uint64(body))

	if authKeyId == 0 {
		s.onHttpHandshake(ctx, c, body)
	} else {
		s.onHttpEncryptedMessage(ctx, c, authKeyId, body)
	}

	return gnet.None
}

// httpHandshakeIO answers handshake messages over HTTP: the state is shared
// by all connections (a client may send each request on a new one) and kept
// apart from the auth key cache.
func (s *Server) httpHandshakeIO(c gnet.Conn) *handshakeIO {
	return &handshakeIO{
		connId: c.ConnId(),
		states: s.httpHandshakes,
		reply: func(c gnet.Conn, answer mtproto.TLObject) {
			metricHandshake.Inc("ok")
			s.writeHttpResponse(c, httpcodec.FormatResponse(handshakePayload(answer)))
		},
		fail: func(c gnet.Conn, err error) {
			metricHandshake.Inc("error")
			logx.Errorf("conn(%d) HTTP handshake: %v", c.ConnId(), err)
			s.writeHttpResponse(c, httpcodec.FormatResponse(nil))
		},
	}
}

// writeHttpResponse answers the request in progress on c.
func (s *Server) writeHttpResponse(c gnet.Conn, resp []byte) {
	_, _ = c.Write(resp)
	if ctx, _ := c.Context().(*connContext); ctx != nil && ctx.httpCodec != nil {
		ctx.httpCodec.Reset()
	}
}

func (s *Server) onHttpHandshake(ctx *connContext, c gnet.Conn, body []byte) {
	io := s.httpHandshakeIO(c)
	out, pending, err := s.onHandshakeMessage(io, body)
	switch {
	case err != nil:
		io.fail(c, err)
	case out != nil:
		io.reply(c, out)
	case !pending:
		// msgs_ack: nothing to answer, but the request needs a response.
		s.writeHttpResponse(c, httpcodec.FormatResponse(nil))
	}
}

func (s *Server) onHttpEncryptedMessage(ctx *connContext, c gnet.Conn, authKeyId int64, body []byte) {
	if len(body) < encryptedFrameMinSize {
		logx.Errorf("conn(%d) HTTP encrypted message of %d bytes", c.ConnId(), len(body))
		_, _ = c.Write(httpcodec.FormatErrorResponse(400, "Bad Request"))
		ctx.httpCodec.Reset()
		return
	}

	now := s.CachedNow()
	authKey := ctx.getAuthKey()
	if authKey == nil {
		authKey = s.GetAuthKey(authKeyId)
		if authKey != nil {
			ctx.putAuthKey(authKey)
		}
	} else if authKey.AuthKeyId() != authKeyId {
		logx.Errorf("conn(%s) HTTP authKeyId mismatch", c)
		_, _ = c.Write(httpcodec.FormatErrorResponse(400, "Bad Request"))
		ctx.httpCodec.Reset()
		return
	}

	if authKey != nil && authKey.expired(now) {
		s.RemoveAuthKey(authKeyId)
		s.writeHttpAuthKeyNotFound(ctx, c)
		return
	}

	if authKey != nil && !authKey.needsRecheck(now) {
		s.doHttpEncryptedMessage(ctx, c, authKey, body)
	} else {
		known := s.knownAuthKey(authKeyId, authKey)
		if known != nil && known.unboundReadsExhausted() {
			logx.Errorf("conn(%d) HTTP temp auth key %d still unbound after %d reads", c.ConnId(), authKeyId, maxUnboundTempKeyReads)
			s.writeHttpAuthKeyNotFound(ctx, c)
			return
		}

		// Not cached, due for a recheck, or an unbound temporary key whose
		// binding only authsession can confirm: read it from authsession.
		bodyClone := make([]byte, len(body))
		copy(bodyClone, body)
		connId := c.ConnId()

		if err := s.submit(connId, func() {
			ak, err := s.queryKey(authKeyId, known)
			if err != nil {
				logx.Errorf("conn(%d) HTTP sessionQueryAuthKey error: %v", connId, err)
				unregistered := errors.Is(err, mtproto.ErrAuthKeyUnregistered)
				if unregistered {
					s.RemoveAuthKey(authKeyId)
				}
				s.trigger(connId, func(c gnet.Conn) {
					ctx2, _ := c.Context().(*connContext)
					if ctx2 == nil {
						return
					}
					if unregistered {
						s.writeHttpAuthKeyNotFound(ctx2, c)
						return
					}
					// authsession or session failed (restart, timeout): the
					// key may be fine, so no -404, which makes clients drop
					// it. The client retries the request.
					s.writeHttpResponse(c, httpcodec.FormatErrorResponse(502, "Bad Gateway"))
				})
				return
			}

			s.trigger(connId, func(c gnet.Conn) {
				ctx2, _ := c.Context().(*connContext)
				if ctx2 == nil {
					return
				}
				if ak.unboundReadsExhausted() {
					logx.Errorf("conn(%d) HTTP temp auth key %d still unbound after %d reads", c.ConnId(), authKeyId, maxUnboundTempKeyReads)
					s.writeHttpAuthKeyNotFound(ctx2, c)
					return
				}
				ctx2.putAuthKey(ak)
				s.doHttpEncryptedMessage(ctx2, c, ak, bodyClone)
			})
		}); err != nil {
			logx.Errorf("conn(%s) HTTP pool.Submit error: %v", c, err)
			_, _ = c.Write(httpcodec.FormatErrorResponse(502, "Bad Gateway"))
			ctx.httpCodec.Reset()
		}
	}
}

func (s *Server) doHttpEncryptedMessage(ctx *connContext, c gnet.Conn, authKey *authKeyUtil, body []byte) {
	msgKey, encryptedData, err := splitEncryptedFrame(body)
	if err == nil {
		var mtpRwaData []byte
		mtpRwaData, err = authKey.AesIgeDecrypt(msgKey, encryptedData)
		if err == nil {
			s.markAuthenticated(c, ctx)
			s.sendHttpEncryptedMessage(ctx, c, authKey, mtpRwaData)
			return
		}
	}
	logx.Errorf("conn(%s) HTTP decrypt error: %v", c, err)
	_, _ = c.Write(httpcodec.FormatErrorResponse(400, "Bad Request"))
	ctx.httpCodec.Reset()
}

func (s *Server) sendHttpEncryptedMessage(ctx *connContext, c gnet.Conn, authKey *authKeyUtil, mtpRwaData []byte) {
	salt := int64(binary.LittleEndian.Uint64(mtpRwaData))
	sessionId := int64(binary.LittleEndian.Uint64(mtpRwaData[8:]))
	clientIp := ctx.clientIp
	connId := c.ConnId()

	payload := make([]byte, len(mtpRwaData[16:]))
	copy(payload, mtpRwaData[16:])

	if err := s.submit(connId, func() {
		sendCtx, span := otel.Tracer("gnetway").Start(context.Background(), "HttpSessionSendHttpData")
		defer span.End()

		// Routed like TCP: by the key data from authsession, never by the
		// message (an unbound temporary key gets its own session wrapper).
		routeKey, data := newSessionClientData(s.svcCtx.GatewayId, authKey, session.TRANSPORT_HTTP, clientIp, sessionId, salt, payload)
		rV, err := s.svcCtx.Dao.SessionDispatcher.SendHttpData(sendCtx, routeKey, &session.TLSessionSendHttpDataToSession{
			Client: data,
		})
		if err != nil {
			logx.Errorf("conn(%d) HTTP SendHttpData error: %v", connId, err)
			s.trigger(connId, func(c gnet.Conn) {
				_, _ = c.Write(httpcodec.FormatErrorResponse(502, "Bad Gateway"))
				if ctx2, _ := c.Context().(*connContext); ctx2 != nil {
					ctx2.httpCodec.Reset()
				}
			})
			return
		}

		msgKey, mtpRawData, _ := authKey.AesIgeEncrypt(rV.Payload)
		resp := func() []byte {
			x := mtproto.GetEncodeBuf()
			defer mtproto.PutEncodeBuf(x)
			x.Long(authKey.AuthKeyId())
			x.Bytes(msgKey)
			x.Bytes(mtpRawData)
			return append([]byte(nil), x.GetBuf()...)
		}()

		s.trigger(connId, func(c gnet.Conn) {
			_, _ = c.Write(httpcodec.FormatResponse(resp))
			if ctx2, _ := c.Context().(*connContext); ctx2 != nil {
				ctx2.httpCodec.Reset()
			}
		})
	}); err != nil {
		logx.Errorf("conn(%s) HTTP pool.Submit error: %v", c, err)
		_, _ = c.Write(httpcodec.FormatErrorResponse(502, "Bad Gateway"))
		ctx.httpCodec.Reset()
	}
}

// writeHttpAuthKeyNotFound answers with transport error -404 (unknown or
// expired auth key), as the TCP transports do.
func (s *Server) writeHttpAuthKeyNotFound(ctx *connContext, c gnet.Conn) {
	var (
		errPayload = make([]byte, 4)
		code       = int32(-404)
	)
	binary.LittleEndian.PutUint32(errPayload, uint32(code))
	_, _ = c.Write(httpcodec.FormatResponse(errPayload))
	ctx.httpCodec.Reset()
}
