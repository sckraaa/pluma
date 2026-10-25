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
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/teamgram/marmota/pkg/cache"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/interface/gnetway/internal/config"
	"github.com/teamgram/teamgram-server/app/interface/gnetway/internal/server/gnet/codec"
	"github.com/teamgram/teamgram-server/app/interface/gnetway/internal/svc"
	"github.com/teamgram/teamgram-server/pkg/serverkey"

	"github.com/panjf2000/gnet/v2"
	"github.com/panjf2000/gnet/v2/pkg/logging"
	"github.com/panjf2000/gnet/v2/pkg/pool/goroutine"
	"github.com/zeromicro/go-zero/core/logx"
)

var (
	//etcdPrefix is a etcd globe key prefix
	endpoints string
)

type Server struct {
	gnet.BuiltinEventEngine
	eng            gnet.Engine
	pool           *goroutine.Pool
	cache          *cache.LRUCache
	c              *config.Config
	handshake      *handshake
	authSessionMgr *authSessionManager
	svcCtx         *svc.ServiceContext
	tickNumber     int64
	cachedNow      atomic.Int64 // cached unix timestamp, updated every OnTick
	timeoutWheel   *timeoutWheel

	// httpHandshakes keeps handshakes over the HTTP transport, apart from
	// the auth key cache so that unauthenticated requests cannot evict keys.
	httpHandshakes *handshakeStore
	// saveAuthKey stores a key created by a handshake (saveAuthKeyInfo);
	// errAuthKeyIdTaken when another key has its auth_key_id.
	saveAuthKey func(st *HandshakeStateCtx, key *mtproto.AuthKeyInfo) error
	// queryKey reads an auth key from authsession (fetchAuthKey); known is
	// the copy the gateway had, if any.
	queryKey func(authKeyId int64, known *authKeyUtil) (*authKeyUtil, error)
	// triggerFn, when set (tests), replaces eng.Trigger.
	triggerFn func(connId int64, cb func(c gnet.Conn))
	// limits bounds the connections and key exchanges of one address.
	limits *ipLimits
}

// CachedNow returns the cached unix timestamp (updated every OnTick interval).
// Falls back to time.Now() if not yet initialized.
func (s *Server) CachedNow() int64 {
	if n := s.cachedNow.Load(); n > 0 {
		return n
	}
	return time.Now().Unix()
}

// New starts the MTProto listeners. keys are the server RSA keys loaded and
// verified by LoadRSAKeys.
func New(svcCtx *svc.ServiceContext, c config.Config, keys []*serverkey.Key) (*Server, error) {
	var (
		s   = new(Server)
		err error
	)

	s.handshake, err = newHandshake(keys)
	if err != nil {
		return nil, err
	}

	s.authSessionMgr = NewAuthSessionManager()
	s.timeoutWheel = newTimeoutWheel()
	s.httpHandshakes = newHandshakeStore(maxHttpHandshakes, httpHandshakeTTL)
	s.saveAuthKey = s.saveAuthKeyInfo
	s.queryKey = s.fetchAuthKey
	s.limits = newIPLimits(c.Gnetway.MaxConnsPerIP, c.Gnetway.KeyExchangesPerIPMinute)

	cacheSizeMB := c.Gnetway.AuthKeyCacheM
	if cacheSizeMB <= 0 {
		cacheSizeMB = 10
	}
	s.cache = cache.NewLRUCache(int64(cacheSizeMB) * 1024 * 1024)

	s.pool = goroutine.Default()

	s.c = &c
	s.svcCtx = svcCtx

	go func() {
		s.Serve()
	}()
	return s, nil
}

// classifyCodecError maps codec errors to a stable reason label for metrics.
func classifyCodecError(err error) string {
	switch {
	case errors.Is(err, codec.ErrProtoBadMagic):
		return "bad_magic"
	case errors.Is(err, codec.ErrProtoBadLength):
		return "bad_len"
	case errors.Is(err, codec.ErrProtoBadCRC):
		return "bad_crc"
	case errors.Is(err, codec.ErrProtoBadSeq):
		return "bad_seq"
	case errors.Is(err, codec.ErrProtoDecrypt):
		return "decrypt"
	case errors.Is(err, codec.ErrTransportNotSupported):
		return "transport_unsupported"
	case errors.Is(err, codec.ErrUnexpectedEOF):
		return "unexpected_eof"
	default:
		return "other"
	}
}

func (s *Server) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	logx.Debugf("stop engine... error: %v", s.eng.Stop(ctx))
}

func (s *Server) Serve() {
	// addrs := strings.Join(s.c.Gnetway.ToAddresses(), ",")
	logx.Debugf("addrs: %s", s.c.Gnetway.ToAddresses())

	err := gnet.Rotate(
		s,
		s.c.Gnetway.ToAddresses(),
		gnet.WithMulticore(s.c.Gnetway.Multicore),
		gnet.WithSocketRecvBuffer(s.c.Gnetway.ReceiveBuf),
		gnet.WithSocketSendBuffer(s.c.Gnetway.SendBuf),
		gnet.WithLockOSThread(true),
		gnet.WithReuseAddr(true),
		gnet.WithTicker(true),
		gnet.WithLogLevel(logging.DebugLevel),
		gnet.WithLogger(NewLogger()))
	if err != nil {
		logx.Error(err)
		panic(err)
	}
}
