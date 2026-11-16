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

package core

import (
	"github.com/teamgram/teamgram-server/pkg/reactions"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/langpack"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	// date = 1509066502,    2017/10/27 09:08:22
	// expires = 1509070295, 2017/10/27 10:11:35
	expiresTimeout = 3600 // 超时时间设置为3600秒

	// support user: @benqi
	// SUPPORT_USER_ID = 2
)

// HelpGetConfig
// help.getConfig#c4f9186b = Config;
func (c *ConfigurationCore) HelpGetConfig(in *mtproto.TLHelpGetConfig) (*mtproto.Config, error) {
	_ = in

	rValue, _ := proto.Clone(c.svcCtx.ClientConfig).(*mtproto.TLConfig)
	now := int32(time.Now().Unix())
	rValue.SetDate(now)
	rValue.SetExpires(now + expiresTimeout)
	c.setLangPack(rValue)
	c.setDefaultReaction(rValue)
	// Where the desktop app looks for its updates (task 054): tdesktop
	// keeps it as its prefix and asks <prefix>/current
	// (mtp_instance.cpp:935-937, update_checker.cpp:717-721 at a1e89e1f).
	if url := c.svcCtx.Config.ReleasesURL; url != "" {
		rValue.SetAutoupdateUrlPrefix(mtproto.MakeFlagsString(url + "/dl/desktop"))
	}

	return rValue.To_Config(), nil
}

// defaultReaction is Telegram's reactions_default for an account that chose
// none (help.getConfig, 2026-11-03).
const defaultReaction = "e❤"

// setDefaultReaction fills reactions_default, the quick reaction of the
// clients, with the account's own (messages.setDefaultReaction, task 053).
func (c *ConfigurationCore) setDefaultReaction(cfg *mtproto.TLConfig) {
	key := defaultReaction
	if c.MD.GetUserId() != 0 {
		own, err := reactions.Default(c.ctx, c.svcCtx.Dao.DB(), c.MD.GetUserId())
		switch {
		case err != nil:
			// The rest of config still serves the client.
			c.Logger.Errorf("help.getConfig: the default reaction of user %d: %v", c.MD.GetUserId(), err)
		case own != "":
			key = own
		}
	}
	cfg.SetReactionsDefault(reactions.FromKey(key))
}

// setLangPack fills flags.2 of config for the session's pack and language:
// suggested_lang_code, lang_pack_version and base_lang_pack_version, the
// versions langpack.getDifference answers with. The clients ask for a
// difference when their version is behind (Android
// checkUpdateForCurrentRemoteLocale, LocaleController.java:847-874; iOS
// ManagedConfigurationUpdates.swift:98-111; tdesktop
// lang_cloud_manager.cpp:271-279). Without a stored pack the flag is left
// out and the clients keep their bundled strings. The three fields share
// the flag and are written only together.
func (c *ConfigurationCore) setLangPack(cfg *mtproto.TLConfig) {
	cfg.SetSuggestedLangCode(nil)
	cfg.SetLangPackVersion(nil)
	cfg.SetBaseLangPackVersion(nil)

	pack := langpack.CanonicalPack(c.MD.GetLangpack())
	lang := langpack.CanonicalLangCode(c.MD.GetLangCode())
	v, err := c.svcCtx.Dao.Langpacks.ConfigVersions(c.ctx, pack, lang)
	if err != nil {
		// The rest of config still serves the client; its language pack
		// waits for the next help.getConfig.
		c.Logger.Errorf("help.getConfig: language pack versions of %s/%s: %v", pack, lang, err)
		return
	}
	if v == nil {
		return
	}
	cfg.SetSuggestedLangCode(wrapperspb.String(v.SuggestedLangCode))
	cfg.SetLangPackVersion(wrapperspb.Int32(v.LangPackVersion))
	cfg.SetBaseLangPackVersion(wrapperspb.Int32(v.BaseLangPackVersion))
}
