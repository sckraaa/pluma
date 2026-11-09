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

package botstore

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// secretBytes is the entropy of the secret part of a token.
const secretBytes = 26

// newToken returns a token of the bot, <bot id>:<secret>, and the hash of its
// secret, which is all that is stored (ADR 0041).
func newToken(botId int64) (token, hash string, err error) {
	b := make([]byte, secretBytes)
	if _, err = rand.Read(b); err != nil {
		return "", "", fmt.Errorf("botstore: read random: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(b)
	return strconv.FormatInt(botId, 10) + ":" + secret, hashOf(secret), nil
}

// hashOf is the stored form of a secret. The secret has 208 random bits, so
// no salt or slow hash is needed.
func hashOf(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// ParseToken splits a token into the bot id and the hash of its secret; ok is
// false for anything that is not a token.
func ParseToken(token string) (botId int64, hash string, ok bool) {
	id, secret, found := strings.Cut(token, ":")
	if !found || secret == "" {
		return 0, "", false
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return 0, "", false
	}
	return n, hashOf(secret), true
}
