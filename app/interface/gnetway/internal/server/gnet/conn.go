// Copyright 2022 Teamgram Authors
//  All rights reserved.
//
// Author: Benqi (wubenqi@gmail.com)
//

package gnet

import (
	"fmt"

	"github.com/teamgram/teamgram-server/app/interface/gnetway/internal/server/gnet/codec"
	httpcodec "github.com/teamgram/teamgram-server/app/interface/gnetway/internal/server/gnet/http"
	"github.com/teamgram/teamgram-server/app/interface/gnetway/internal/server/gnet/ws"

	"github.com/zeromicro/go-zero/core/logx"
)

type HandshakeStateCtx struct {
	State         int32  `json:"state,omitempty"`
	ResState      int32  `json:"res_state,omitempty"`
	Nonce         []byte `json:"nonce,omitempty"`
	ServerNonce   []byte `json:"server_nonce,omitempty"`
	NewNonce      []byte `json:"new_nonce,omitempty"`
	A             []byte `json:"a,omitempty"`
	HandshakeType int    `json:"handshake_type"`
	ExpiresIn     int32  `json:"expires_in,omitempty"`

	// Pq of resPQ and its factors P < Q, which req_DH_params must repeat.
	Pq uint64 `json:"pq,omitempty"`
	P  uint64 `json:"p,omitempty"`
	Q  uint64 `json:"q,omitempty"`
	// RetryId is the retry_id client_DH_inner_data must carry: 0 at the
	// first attempt, after dh_gen_retry the auth_key_aux_hash of the key
	// that collided.
	RetryId int64 `json:"retry_id,omitempty"`
}

// Format prints the state without NewNonce and A: with them anyone reading
// a log could recompute the auth key (audit finding #8). It applies to every
// verb, so neither %v nor %+v nor %#v can print them.
func (m HandshakeStateCtx) Format(f fmt.State, _ rune) {
	fmt.Fprintf(f, "{state: %d, res_state: %d, handshake_type: %d, expires_in: %d}",
		m.State, m.ResState, m.HandshakeType, m.ExpiresIn)
}

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
