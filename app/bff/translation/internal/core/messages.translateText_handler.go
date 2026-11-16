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
	"errors"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/translate"
)

const methodTranslateText = "messages.translateText"

// MessagesTranslateText
// messages.translateText#a5eec345 flags:# peer:flags.0?InputPeer id:flags.0?Vector<int> text:flags.1?Vector<TextWithEntities> to_lang:string tone:flags.2?string = messages.TranslatedText;
//
// The translation of messages (peer and id) or of texts (text): a message,
// the visible messages of a chat, a transcription, a poll (ADR 0064, p. 8).
// What the answer must be for the clients:
//
//   - exactly one item for each one asked, in its order, with its entities:
//     the clients match by position (texts.go, messageTexts);
//   - an error that a client waits out, never 500: what is not ready within
//     the wait, an engine that is away, a user over the limit, a service
//     that failed on the way are FLOOD_WAIT_X, which Android, tdesktop and
//     Web K repeat by themselves after X (jni/tgnet/ConnectionsManager.cpp:
//     1372-1420, mtproto/mtp_instance.cpp:1583-1625,
//     lib/appManagers/apiManager.ts:841-870), and a 500 they would repeat
//     without end;
//   - never TRANSLATIONS_DISABLED_ALT: on it Android and iOS translate on
//     the device with a third-party service (TranslateController.java:
//     1168-1183, Translate.swift:60), and the text must not leave the
//     server (ADR 0057).
//
// tone is not read: no client sends it (Android TranslateAlert2.java:417,
// tdesktop translate_mtproto_provider.cpp:108), and the model has no tones.
// The fields of older layers (msg_id, text as a string, from_lang) are not
// sent by the clients of layers 228 and 233 and are not read either.
// translations_disabled of a chat does not forbid its translation: it only
// hides the bar (messages.togglePeerTranslations).
func (c *TranslationCore) MessagesTranslateText(in *mtproto.TLMessagesTranslateText) (*mtproto.Messages_TranslatedText, error) {
	if c.MD.IsBot {
		// core.telegram.org/method/messages.translateText: users only.
		return nil, refuse(methodTranslateText, translate.ResultInvalid, mtproto.ErrBotMethodInvalid)
	}
	if c.svcCtx.Translator == nil {
		// No engine: the clients are told translations_*_enabled =
		// disabled and offer nothing (ADR 0064, p. 9); tdesktop, which
		// does not read the keys, gets the answer it got before.
		return nil, refuse(methodTranslateText, translate.ResultOff, mtproto.ErrEnterpriseIsBlocked)
	}
	byMessages := in.GetPeer() != nil
	if !byMessages && len(in.GetText_FLAGVECTORTEXTWITHENTITIES()) == 0 {
		return nil, refuse(methodTranslateText, translate.ResultInvalid, mtproto.ErrInputTextEmpty)
	}
	if byMessages && len(in.GetId()) == 0 {
		return nil, refuse(methodTranslateText, translate.ResultInvalid, mtproto.ErrMsgIdInvalid)
	}
	lang, err := c.svcCtx.Profile.Lang(in.GetToLang())
	if err != nil {
		return nil, refuse(methodTranslateText, translate.ResultInvalid, err)
	}

	var items []*mtproto.TextWithEntities
	if byMessages {
		items, err = c.messageTexts(in.GetPeer(), in.GetId())
	} else {
		items, err = c.clientTexts(in.GetText_FLAGVECTORTEXTWITHENTITIES())
	}
	switch {
	case errors.Is(err, errUnreachable):
		return nil, refuse(methodTranslateText, translate.ResultInvalid, mtproto.ErrPeerIdInvalid)
	case err != nil && clientError(err):
		return nil, refuse(methodTranslateText, translate.ResultInvalid, err)
	case err != nil:
		return nil, c.readFailed(methodTranslateText, "read the texts", err)
	}

	// One item is one message a person asked for and waits for (the
	// translation sheet of Android, of tdesktop); a batch is the bar of a
	// chat, which goes after them (ADR 0064, p. 6).
	prio := translate.PriorityChat
	if len(items) == 1 {
		prio = translate.PriorityManual
	}
	out, err := c.svcCtx.Translator.Translate(c.ctx, translate.Ask{
		Method:   methodTranslateText,
		UserId:   c.MD.UserId,
		Lang:     lang,
		Priority: prio,
		Items:    items,
	})
	if err != nil {
		// The scheduler's errors are the client's answers, counted there.
		return nil, err
	}
	return mtproto.MakeTLMessagesTranslateResult(&mtproto.Messages_TranslatedText{Result: out}).To_Messages_TranslatedText(), nil
}
