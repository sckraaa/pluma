// Copyright (c) 2026 The Teamgram Authors (https://teamgram.net).
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

package core

import (
	"github.com/teamgram/proto/mtproto"
)

// MessagesTranslateRichMessage
// messages.translateRichMessage#1a542004 flags:# peer:flags.0?InputPeer id:flags.0?Vector<int> text:flags.1?Vector<InputRichMessage> to_lang:string tone:flags.2?string = messages.TranslatedRichMessage;
//
// Refused (ADR 0064, p. 8): the method translates message.rich_message, the
// blocks of a rich message, and Pluma keeps no rich messages
// (messages.saveDraft drops the field, sendMessage does not read it). The
// clients ask it only for a message that has one: Android
// messenger/TranslateController.java:1506, iOS TelegramCore/.../Messages/
// Translate.swift:330-332, tdesktop history_view_translate_tracker.cpp:373
// (item->richPage()); every other message goes through
// messages.translateText (app/bff/translation). Its translation by the
// same model comes with the task that starts to keep rich messages.
// ERR_ENTERPRISE_IS_BLOCKED (400) is not repeated by the clients; never
// TRANSLATIONS_DISABLED_ALT, on which Android and iOS translate on the
// device with a third-party service.
func (c *MessagesCore) MessagesTranslateRichMessage(in *mtproto.TLMessagesTranslateRichMessage) (*mtproto.Messages_TranslatedRichMessage, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
