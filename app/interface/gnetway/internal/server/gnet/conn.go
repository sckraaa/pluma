// Copyright 2022 Teamgram Authors
//  All rights reserved.
//
// Author: Benqi (wubenqi@gmail.com)
//

package gnet

import (
	httpcodec "github.com/teamgram/teamgram-server/app/interface/gnetway/internal/server/gnet/http"
	"github.com/teamgram/teamgram-server/app/interface/gnetway/internal/server/gnet/ws"
	"github.com/teamgram/teamgram-server/pkg/mtserver/codec"
	"github.com/teamgram/teamgram-server/pkg/mtserver/handshake"

	"github.com/zeromicro/go-zero/core/logx"
)

// HandshakeStateCtx is the state of a key exchange in progress
// (pkg/mtserver/handshake).
type HandshakeStateCtx = handshake.HandshakeStateCtx

type connContext struct {
	codec     codec.Codec
	authKey   *authKeyUtil
	sessionId int64
	// handshakes in progress on this connection; see handshakeIO.
	handshakes *handshakeIO
	clientIp   string
	// limitSubject is the address the connection counts against
	// (ipLimits.openConn), once its real address is known.
	limitSubject string
	tcp          bool
	websocket    bool
	http         bool
	wsCodec      *ws.WsCodec
	httpCodec    *httpcodec.HttpCodec
	logx.Logger
	newSession bool
	nextSeqNo  int32
	closeDate  int64
	ppv1       bool
	// authenticated: a message on this connection decrypted with a known
	// auth key. Until then the connection gets no more time than it was
	// given when it opened, whatever it sends.
	authenticated bool
	// keyQuery: the auth key of this connection is being read from
	// authsession (queryAuthKey).
	keyQuery *pendingKeyQuery
}

func newConnContext() *connContext {
	return &connContext{
		codec:    nil,
		clientIp: "",
	}
}

func (ctx *connContext) generateMessageSeqNo(increment bool) int32 {
	value := ctx.nextSeqNo
	if increment {
		ctx.nextSeqNo++
		return value*2 + 1
	} else {
		return value * 2
	}
}

func (ctx *connContext) setClientIp(ip string) {
	ctx.clientIp = ip
}

func (ctx *connContext) getAuthKey() *authKeyUtil {
	return ctx.authKey
}

func (ctx *connContext) putAuthKey(k *authKeyUtil) {
	ctx.authKey = k
}
