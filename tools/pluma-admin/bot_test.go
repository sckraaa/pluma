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

package main

import (
	"errors"
	"testing"

	"github.com/teamgram/teamgram-server/pkg/conf"
	"github.com/teamgram/teamgram-server/pkg/inlinebots"
)

// bot token and bot set leave the built-in inline bots alone: no token signs
// in as them and their flags are the registry's.
func TestBuiltinBotsAreNotEditable(t *testing.T) {
	for _, b := range inlinebots.All() {
		if err := checkEditable(b.Id); !errors.Is(err, errBuiltinBot) {
			t.Errorf("@%s: %v", b.Username, err)
		}
	}
	if err := checkEditable(136907713); err != nil {
		t.Errorf("a bot of an owner: %v", err)
	}
}

// TestLoadsBotConfig: bot create and bot token find the key the keys of a
// token are sealed under in both bff configs (task 106).
func TestLoadsBotConfig(t *testing.T) {
	for _, name := range []string{
		"MYSQL_PASSWORD", "REDIS_PASSWORD", "LOGIN_REVIEW_PHONE", "LOGIN_REVIEW_CODE",
		"LOGIN_CODES_SENDER_URL", "LOGIN_CODES_SENDER_TOKEN", "TELEGRAM_API_ID", "TELEGRAM_API_HASH",
		"TELEGRAM_BOT_TOKEN", "PLUMA_RELEASES_URL", "PLUMA_IOS_UPDATE_SECRET", "CALLS_ICE_SERVERS",
		"CALLS_TURN_SECRET", "CALLS_QUALITY", "STT_URL", "STT_TOKEN", "STT_MODEL", "STT_LANGUAGE",
		"STT_TIMEOUT", "TRANSLATE_URL", "TRANSLATE_TOKEN", "TRANSLATE_MODEL", "TRANSLATE_PROFILE", "TRANSLATE_SLOTS", "TRANSLATE_CTX", "MAIL_API_KEY", "MAIL_FROM", "WEBFILE_HMAC_KEY", "WEBAPP_SIGNING_KEY",
		"STATS_GRAPH_TOKEN_KEY",
		"MAPS_URL", "MAPS_STYLES", "MAPS_TIMEOUT", "MAPS_CONCURRENCY", "MAPS_CACHE_TTL", "MAPS_PER_USER_PER_MINUTE",
		"SEARXNG_URL", "INLINE_IMPORT_DAILY_BYTES", "GROUPCALLS_SFU_ENDPOINT",
		"CDN_NODE_CONTROL", "CDN_DC_ID", "CDN_DC_ADDRESS", "CDN_DC_SECRET", "CDN_MIN_FILE_SIZE",
		"CDN_PUSH_RATE_MBIT", "CDN_PUSH_CONCURRENCY", "CDN_MONTHLY_EGRESS_LIMIT_GB", "CDN_REUPLOAD_WAIT",
	} {
		t.Setenv(name, "")
	}
	t.Setenv("LOGIN_CODE_HMAC_KEY", testHmacKey)
	t.Setenv("PUSH_SECRET_ENCRYPTION_KEY", testHmacKey)
	key := testHmacKey + testHmacKey // 64 hex digits
	t.Setenv("BOT_SECRET_ENCRYPTION_KEY", key)
	for _, f := range []string{"../../teamgramd/etc/bff.yaml", "../../teamgramd/etc2/bff.yaml"} {
		var c botConfig
		if err := conf.Load(f, &c); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if c.BotSecrets.EncryptionKey != key {
			t.Errorf("%s: the bot secret encryption key is not read", f)
		}
		if s, err := c.BotSecrets.Sealer(); s == nil || err != nil {
			t.Errorf("%s: sealer: %v", f, err)
		}
	}
}
