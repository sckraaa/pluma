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
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/translate"
)

const methodComposeMessageWithAI = "messages.composeMessageWithAI"

// MessagesComposeMessageWithAI
// messages.composeMessageWithAI#daecc589 flags:# proofread:flags.0?true emojify:flags.3?true text:TextWithEntities translate_to_lang:flags.1?string tone:flags.2?InputAiComposeTone = messages.ComposedMessageWithAI;
//
// Only the translation form is served (ADR 0064, p. 8): translate_to_lang
// with no proofread, emojify or tone. It is how iOS translates a message by
// hand: with translations_manual_enabled = enabled its "Translate" opens the
// text processing screen in its translate mode
// (TelegramUI/Sources/ChatController.swift:4590-4624), which asks this
// method with translate_to_lang (TextProcessingTranslateContentComponent.swift:
// 240-282, TelegramCore/.../Messages/Translate.swift:856-919). The answer is
// result_text without diff_text.
//
// Every other form (proofreading, emojis, a tone: the AI editor) needs a
// general language model, which Pluma does not have: ERR_ENTERPRISE_IS_BLOCKED
// (400), which the clients do not repeat. iOS shows any error of the method
// as a generic failure except AICOMPOSE_FLOOD_PREMIUM, which offers Premium
// (Translate.swift:919-926) and is never answered.
func (c *TranslationCore) MessagesComposeMessageWithAI(in *mtproto.TLMessagesComposeMessageWithAI) (*mtproto.Messages_ComposedMessageWithAI, error) {
	if c.MD.IsBot {
		return nil, refuse(methodComposeMessageWithAI, translate.ResultInvalid, mtproto.ErrBotMethodInvalid)
	}
	if in.GetTranslateToLang() == nil || in.GetProofread() || in.GetEmojify() || in.GetTone() != nil || in.GetChangeTone() != nil {
		return nil, refuse(methodComposeMessageWithAI, translate.ResultInvalid, mtproto.ErrEnterpriseIsBlocked)
	}
	if c.svcCtx.Translator == nil {
		return nil, refuse(methodComposeMessageWithAI, translate.ResultOff, mtproto.ErrEnterpriseIsBlocked)
	}
	lang, err := c.svcCtx.Profile.Lang(in.GetTranslateToLang().GetValue())
	if err != nil {
		return nil, refuse(methodComposeMessageWithAI, translate.ResultInvalid, err)
	}
	// The screen sends an empty text when it has none
	// (Translate.swift:905-912); it is an empty item, as in translateText.
	text := in.GetText()
	if text == nil {
		text = emptyText()
	}
	items, err := c.clientTexts([]*mtproto.TextWithEntities{text})
	switch {
	case err != nil && clientError(err):
		return nil, refuse(methodComposeMessageWithAI, translate.ResultInvalid, err)
	case err != nil:
		return nil, c.readFailed(methodComposeMessageWithAI, "resolve the mentions", err)
	}
	out, err := c.svcCtx.Translator.Translate(c.ctx, translate.Ask{
		Method: methodComposeMessageWithAI,
		UserId: c.MD.UserId,
		Lang:   lang,
		// A person looks at the screen and waits for it.
		Priority: translate.PriorityManual,
		Items:    items,
	})
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesComposedMessageWithAI(&mtproto.Messages_ComposedMessageWithAI{ResultText: out[0]}).To_Messages_ComposedMessageWithAI(), nil
}
