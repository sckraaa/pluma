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

// Package core is the part of the secret chat methods the calls need: the
// clients take the Diffie-Hellman group of a call from messages.getDhConfig
// before anything else (task 068).
package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	// dhVersion numbers the group below. A client without one sends 0
	// (Telegram-iOS SecretChatEncryptionConfig.swift:9, tweb
	// appCallsManager.ts:165, tdesktop's and Android's empty cache) and must
	// get the group: iOS and tdesktop treat dhConfigNotModified on version 0
	// as a failure (SecretChatEncryptionConfig.swift:30,
	// calls_instance.cpp:555).
	dhVersion = 1
	dhG       = 3
	// maxRandomLength bounds the random of one request; the clients ask for
	// 256 bytes or none.
	maxRandomLength = 1024
)

// dhPrime is the 2048-bit safe prime of the auth key exchange
// (gnetway handshake.go), the one tdesktop knows as good without testing it
// (mtproto_dh_utils.cpp:107). Android, iOS and tweb test it themselves.
var dhPrime = mustHex("" +
	"c71caeb9c6b1c9048e6c522f70f13f73980d40238e3e21c14934d037563d930f" +
	"48198a0aa7c14058229493d22530f4dbfa336f6e0ac925139543aed44cce7c37" +
	"20fd51f69458705ac68cd4fe6b6b13abdc9746512969328454f18faf8c595f64" +
	"2477fe96bb2a941d5bcd1d4ac8cc49880708fa9b378e3c4f3a9060bee67cf9a4" +
	"a4a695811051907e162753b56b0f6b410dba74d8a84b2a14b3144e0ef1284754" +
	"fd17ed950d5965b4b9dd46582db1178d169c6bc465b0d6ff9ca3928fef5b9ae4" +
	"e418fc15e83ebea0f87fa9ff5eed70050ded2849f47bf959d956850ce929851f" +
	"0d8115f635b105ee2e4e15d04b2454bf6f4fadf034b10403119cd8e3b92fcc5b")

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

type SecretChatsCore struct {
	ctx context.Context
	logx.Logger
	MD *metadata.RpcMetadata
}

func New(ctx context.Context) *SecretChatsCore {
	return &SecretChatsCore{
		ctx:    ctx,
		Logger: logx.WithContext(ctx),
		MD:     metadata.RpcMetadataFromIncoming(ctx),
	}
}

// MessagesGetDhConfig answers the group and random_length random bytes
// (core.telegram.org/method/messages.getDhConfig). tdesktop fails a call
// whose random is not exactly the length it asked for
// (calls_instance.cpp:533).
func (c *SecretChatsCore) MessagesGetDhConfig(in *mtproto.TLMessagesGetDhConfig) (*mtproto.Messages_DhConfig, error) {
	n := in.GetRandomLength()
	if n < 0 || n > maxRandomLength {
		return nil, mtproto.ErrRandomLengthInvalid
	}
	random := make([]byte, n)
	if _, err := rand.Read(random); err != nil {
		c.Logger.Errorf("messages.getDhConfig - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	if in.GetVersion() == dhVersion {
		return mtproto.MakeTLMessagesDhConfigNotModified(&mtproto.Messages_DhConfig{Random: random}).To_Messages_DhConfig(), nil
	}
	return mtproto.MakeTLMessagesDhConfig(&mtproto.Messages_DhConfig{
		G:       dhG,
		P:       dhPrime,
		Version: dhVersion,
		Random:  random,
	}).To_Messages_DhConfig(), nil
}
