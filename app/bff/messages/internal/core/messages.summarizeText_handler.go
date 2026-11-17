// Copyright 2025 Teamgram Authors
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
	"github.com/teamgram/proto/mtproto"
)

// MessagesSummarizeText
// messages.summarizeText#abbbd346 flags:# peer:InputPeer id:int to_lang:flags.0?string tone:flags.2?string = TextWithEntities;
//
// Refused (task 098, part D): the server has no model to summarize with,
// and an empty text would be a made-up answer. The clients do not get here
// on their own: they offer a summary only of a message with
// summary_from_language (Desktop history/history_item_helpers.cpp:986-988,
// Android messenger/TranslateController.java:143, Web
// components/chat/bubbles.ts:10653), which the server never fills. On an
// error Android closes its sheet (ui/Components/TranslateAlert2.java:384-398)
// and iOS shows a generic failure (TelegramEngine/Messages/Summarize.swift:30-44).
// Not TRANSLATIONS_DISABLED_ALT: on it Android summarizes on the device
// with a third-party translator (TranslateAlert2.java:384-385), and the
// text must not leave the server (ADR 0057).
func (c *MessagesCore) MessagesSummarizeText(in *mtproto.TLMessagesSummarizeText) (*mtproto.TextWithEntities, error) {
	c.Logger.Infof("messages.summarizeText - refused: no summarization model on this server")
	return nil, mtproto.ErrEnterpriseIsBlocked
}
