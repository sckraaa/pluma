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

package translate

import (
	"errors"
	"time"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/grpc/status"
)

// The errors of messages.translateText a client is answered with (ADR 0064,
// section 8; core.telegram.org/method/messages.translateText). The others of
// the method are ready in mtproto: ErrInputTextEmpty, ErrMsgIdInvalid,
// ErrPeerIdInvalid, ErrBotMethodInvalid.
var (
	// ErrInputTextTooLong: an item over MaxItemLength, or a request over
	// its count or its total length (ADR 0064, section 7). iOS has a case
	// of its own for it (TelegramCore/.../Messages/Translate.swift:48-64).
	ErrInputTextTooLong = status.Error(mtproto.ErrBadRequest, "INPUT_TEXT_TOO_LONG")
	// ErrToLangInvalid: to_lang is not a language of the profile. Android
	// shows it as a toast (TranslateController.java:1184-1196), iOS as
	// .invalidLanguage (Translate.swift:48-64).
	ErrToLangInvalid = mtproto.ErrToLangInvalid
)

// ErrBadAnswer: the engine answered, but its answer is not a translation of
// the item: empty, longer than MaxOutputFactor times the source, or cut by
// max_tokens. It is a failure of this item, not of the engine: the item is
// tried again (ADR 0064, section 6).
var ErrBadAnswer = errors.New("translate: the answer of the engine cannot be used")

// The bounds of the X of FLOOD_WAIT_X the translation answers with (brief
// 101, "Пределы и ошибки"): not under 3 seconds, so that a retry finds
// progress, and not over 30, under the 60 up to which Web K retries by
// itself (lib/appManagers/apiManager.ts:841-870).
const (
	MinFloodWait = 3 * time.Second
	MaxFloodWait = 30 * time.Second
)

// FloodWait returns FLOOD_WAIT_X for a wait of about d, X rounded up to a
// second and kept within [MinFloodWait, MaxFloodWait]. Android, tdesktop and
// Web K repeat the request after X by themselves; 500 is never answered
// instead, because Android and tdesktop would repeat it without end (ADR
// 0064, "Клиенты", "Повторы транспорта").
func FloodWait(d time.Duration) error {
	d = min(max(d, MinFloodWait), MaxFloodWait)
	return mtproto.NewErrFloodWaitX(int32((d + time.Second - 1) / time.Second))
}
