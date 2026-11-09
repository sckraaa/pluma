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

package core

import (
	"crypto/rand"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/secretchat"
)

// maxRandomLength is the most random bytes one request gets; the clients
// ask 256 (Android SecretChatHelper.startSecretChat).
const maxRandomLength = 1024

// MessagesGetDhConfig
// messages.getDhConfig#26cf8950 version:int random_length:int = messages.DhConfig;
func (c *SecretChatsCore) MessagesGetDhConfig(in *mtproto.TLMessagesGetDhConfig) (*mtproto.Messages_DhConfig, error) {
	if err := c.checkUser(); err != nil {
		return nil, err
	}
	if in.GetRandomLength() < 0 || in.GetRandomLength() > maxRandomLength {
		return nil, mtproto.ErrRandomLengthInvalid
	}
	random := make([]byte, in.GetRandomLength())
	if _, err := rand.Read(random); err != nil {
		c.Logger.Errorf("messages.getDhConfig - %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	if in.GetVersion() == secretchat.DHVersion {
		return mtproto.MakeTLMessagesDhConfigNotModified(&mtproto.Messages_DhConfig{Random: random}).To_Messages_DhConfig(), nil
	}
	return mtproto.MakeTLMessagesDhConfig(&mtproto.Messages_DhConfig{
		G:       secretchat.DHG,
		P:       secretchat.DHPrime,
		Version: secretchat.DHVersion,
		Random:  random,
	}).To_Messages_DhConfig(), nil
}
