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

package gnet

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/interface/gnetway/internal/config"
	sessionclient "github.com/teamgram/teamgram-server/app/interface/session/client"
	"github.com/teamgram/teamgram-server/app/interface/session/session"
	"github.com/teamgram/teamgram-server/pkg/authkeys"
	"github.com/teamgram/teamgram-server/pkg/mtserver/handshake"
	"github.com/teamgram/teamgram-server/pkg/serverkey"

	"github.com/panjf2000/gnet/v2"
	"github.com/zeromicro/go-zero/core/logx"
)

// LoadRSAKeys loads the server RSA keys listed in the config and verifies
// each of them (file permissions, key size, fingerprint), so that gnetway
// refuses to start rather than serve handshakes with a missing, exposed or
// mismatching key. Several keys may be listed to rotate without downtime.
func LoadRSAKeys(cList []config.RSAKey) ([]*serverkey.Key, error) {
	if len(cList) == 0 {
		return nil, fmt.Errorf("no server RSA key configured (RSAKey is empty)")
	}

	keys := make([]*serverkey.Key, 0, len(cList))
	seen := make(map[uint64]string, len(cList))
	for _, c := range cList {
		key, err := serverkey.Load(c.KeyFile, c.KeyFingerprint)
		if err != nil {
			return nil, err
		}
		if prev, ok := seen[key.Fingerprint]; ok {
			return nil, fmt.Errorf("server RSA key %s duplicates %s (fingerprint %d)", c.KeyFile, prev, key.Fingerprint)
		}
		seen[key.Fingerprint] = c.KeyFile
		keys = append(keys, key)
	}

	return keys, nil
}

const (
	// maxConnHandshakes bounds the handshakes in progress on one connection
	// (a client runs perm, temp and media temp key handshakes).
	maxConnHandshakes = 3
	// maxHttpHandshakes bounds the handshakes in progress over the HTTP
	// transport, whose requests may arrive on different connections.
	maxHttpHandshakes = 4096
	// httpHandshakeTTL is how long (seconds) an HTTP handshake may take.
	httpHandshakeTTL = 60
)

var errKeyExchangeLimit = errors.New("handshake: too many key exchanges from the address")

// errAuthKeyIdTaken: another key has the auth_key_id of a new key.
var errAuthKeyIdTaken = handshake.ErrKeyIdTaken

// handshakeIO is how a transport keeps handshake state and answers. reply
// and fail run on the event loop of connection connId; fail is called when a
// step computed in the worker pool fails.
type handshakeIO struct {
	connId int64
	// clientIp is the address the key exchanges count against (ipLimits).
	clientIp string
	states   *handshake.Store
	reply    func(c gnet.Conn, answer mtproto.TLObject)
	fail     func(c gnet.Conn, err error)
	// exchange runs the handshakes of io (pkg/mtserver/handshake); set by
	// bindHandshakeIO.
	exchange *handshake.Exchange
}

// bindHandshakeIO connects io to the handshake code: the steps computed in
// the worker pool answer on the event loop of the connection, keys are
// saved through the session service, and every req_DH_params counts against
// the key exchange budget of the address.
func (s *Server) bindHandshakeIO(io *handshakeIO) *handshakeIO {
	io.exchange = &handshake.Exchange{
		States: io.states,
		Keys: handshake.KeySaverFunc(func(ctx context.Context, key *handshake.AuthKey, expiresIn int32) error {
			return s.saveAuthKey(ctx, key, expiresIn)
		}),
		Now: s.CachedNow,
		Admit: func() error {
			if !s.limits.allowKeyExchange(ipSubject(io.clientIp), time.Now()) {
				metricLimitRefused.Inc("key_exchange")
				return errKeyExchangeLimit
			}
			return nil
		},
		Submit: func(f func()) error { return s.submit(io.connId, f) },
		Reply: func(answer mtproto.TLObject) {
			s.trigger(io.connId, func(c gnet.Conn) { io.reply(c, answer) })
		},
		Fail: func(err error) {
			s.trigger(io.connId, func(c gnet.Conn) { io.fail(c, err) })
		},
	}
	return io
}

// onHandshakeMessage processes an unencrypted message. req_pq is answered at
// once (the answer is returned); the DH steps run in the worker pool and
// answer through io later (pending). msgs_ack needs no answer. An error means
// the message is malformed or does not match the handshake state.
func (s *Server) onHandshakeMessage(io *handshakeIO, frame []byte) (answer mtproto.TLObject, pending bool, err error) {
	return s.handshake.Handle(io.exchange, frame)
}

// saveAuthKeyInfo stores a key created by a handshake (with its first server
// salt) through the session service and caches it. authsession answers
// boolFalse when the auth_key_id is taken (errAuthKeyIdTaken).
func (s *Server) saveAuthKeyInfo(ctx context.Context, key *handshake.AuthKey, expiresIn int32) error {
	now := int32(time.Now().Unix())

	serverSalt := mtproto.MakeTLFutureSalt(&mtproto.FutureSalt{
		ValidSince: now,
		ValidUntil: now + 30*60,
		Salt:       key.ServerSalt,
	}).To_FutureSalt()

	keyInfo := mtproto.NewAuthKeyInfo(key.AuthKeyId, key.AuthKey, key.AuthKeyType)

	// Fix by @wuyun9527, 2018-12-21
	var (
		rB *mtproto.Bool
	)
	err := s.svcCtx.Dao.ShardingSessionClient.InvokeByKey(
		strconv.FormatInt(key.AuthKeyId, 10),
		func(client sessionclient.SessionClient) (err error) {
			rB, err = client.SessionSetAuthKey(ctx, &session.TLSessionSetAuthKey{
				AuthKey:    keyInfo,
				FutureSalt: serverSalt,
				ExpiresIn:  expiresIn,
			})
			return err
		})
	if err = setAuthKeyResult(rB, err); errors.Is(err, errAuthKeyIdTaken) {
		logx.Infof("saveAuthKeyInfo - auth_key_id:%d is taken: dh_gen_retry", key.AuthKeyId)
		return err
	} else if err != nil {
		logx.Errorf("saveAuthKeyInfo not successful - auth_key_id:%d, err:%v", key.AuthKeyId, err)
		return err
	} else {
		// A temporary key expires expires_in seconds after the handshake, at
		// most authkeys.MaxTempExpiresIn (authsession stores the same
		// deadline).
		var expiresAt int64
		if expiresIn > 0 {
			expiresAt = int64(now) + int64(authkeys.TempExpiresIn(expiresIn))
		}
		s.PutAuthKey(newCheckedAuthKeyUtil(
			&mtproto.AuthKeyInfo{
				AuthKeyId:          keyInfo.AuthKeyId,
				AuthKey:            keyInfo.AuthKey,
				AuthKeyType:        keyInfo.AuthKeyType,
				PermAuthKeyId:      keyInfo.PermAuthKeyId,
				TempAuthKeyId:      keyInfo.TempAuthKeyId,
				MediaTempAuthKeyId: keyInfo.MediaTempAuthKeyId},
			int64(now),
			expiresAt))
	}
	return nil
}

// setAuthKeyResult reads the answer of session.setAuthKey: boolTrue means
// the key is stored, boolFalse that its auth_key_id is taken (authsession
// checks it); anything else is a failure to store the key.
func setAuthKeyResult(rB *mtproto.Bool, err error) error {
	switch {
	case err != nil:
		return err
	case rB == nil:
		return errors.New("session.setAuthKey: empty answer")
	case !mtproto.FromBool(rB):
		return errAuthKeyIdTaken
	}
	return nil
}
