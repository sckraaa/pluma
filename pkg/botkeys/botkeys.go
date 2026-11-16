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

// Package botkeys keeps the two keys of a bot that sign what it is given by
// the server (task 106, ADR 0068, p. 3): the key of the initData of its mini
// apps and the key of the data of a login on its site. Both are derived from
// the bot's token, which the server sees in full only when it issues one
// (pluma-admin bot create|token) and when the bot logs in with it
// (auth.importBotAuthorization); the server keeps only the hash of the token
// (ADR 0043), so the keys are derived there and stored sealed under
// BOT_SECRET_ENCRYPTION_KEY.
package botkeys

import (
	"crypto/hmac"
	"crypto/sha256"
	"strings"
)

// KeySize is the length of each key.
const KeySize = sha256.Size

// Keys are the keys of one token.
type Keys struct {
	// WebApp signs the initData of the bot's mini apps:
	// HMAC_SHA256(key = "WebAppData", message = token)
	// (core.telegram.org/bots/webapps, "Validating data received via the
	// Mini App").
	WebApp []byte
	// Login signs the data of a login on the bot's site: SHA256(token)
	// (core.telegram.org/widgets/login-legacy, "Checking authorization").
	Login []byte
}

// Derive returns the keys of token, the whole <bot id>:<secret>.
func Derive(token string) Keys {
	mac := hmac.New(sha256.New, []byte("WebAppData"))
	mac.Write([]byte(token))
	login := sha256.Sum256([]byte(token))
	return Keys{WebApp: mac.Sum(nil), Login: login[:]}
}

// Config is the key the bot keys are sealed under.
type Config struct {
	// EncryptionKey is BOT_SECRET_ENCRYPTION_KEY, 64 hex digits (openssl
	// rand -hex 32). Empty: no keys are stored or read, and the mini apps
	// and the login of bots are off.
	EncryptionKey string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// Sealer returns the Sealer of the config: nil, without an error, when no
// key is set; an error for a key that is not 64 hex digits, which should
// stop the service.
func (c Config) Sealer() (*Sealer, error) {
	if strings.TrimSpace(c.EncryptionKey) == "" {
		return nil, nil
	}
	return NewSealer(c.EncryptionKey)
}
