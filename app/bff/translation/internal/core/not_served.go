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
)

// The tones of the AI editor (aicompose.*) are not served (ADR 0064, "Что
// не делается"): a tone rewrites a text with a general language model,
// which Pluma does not have, and the AI editor is removed from the clients
// (task 079). They share the gRPC service RPCAiComposeTone with
// messages.composeMessageWithAI, so they are answered here with
// ERR_ENTERPRISE_IS_BLOCKED (400), which the clients do not repeat, rather
// than by the routing's fallback. iOS refreshes its styles with
// aicompose.getTones and keeps none on an error (TelegramCore/.../Messages/
// Translate.swift:1264-1267), so the style picker of its translation screen
// is empty (TextProcessingScreen.swift:2305, TelegramEngineMessages.swift:
// 2037-2041).

// AicomposeCreateTone is not served: aicompose.createTone.
func (c *TranslationCore) AicomposeCreateTone(in *mtproto.TLAicomposeCreateTone) (*mtproto.AiComposeTone, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AicomposeUpdateTone is not served: aicompose.updateTone.
func (c *TranslationCore) AicomposeUpdateTone(in *mtproto.TLAicomposeUpdateTone) (*mtproto.AiComposeTone, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AicomposeSaveTone is not served: aicompose.saveTone.
func (c *TranslationCore) AicomposeSaveTone(in *mtproto.TLAicomposeSaveTone) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AicomposeDeleteTone is not served: aicompose.deleteTone.
func (c *TranslationCore) AicomposeDeleteTone(in *mtproto.TLAicomposeDeleteTone) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AicomposeGetTone is not served: aicompose.getTone.
func (c *TranslationCore) AicomposeGetTone(in *mtproto.TLAicomposeGetTone) (*mtproto.Aicompose_Tones, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AicomposeGetTones is not served: aicompose.getTones.
func (c *TranslationCore) AicomposeGetTones(in *mtproto.TLAicomposeGetTones) (*mtproto.Aicompose_Tones, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AicomposeGetToneExample is not served: aicompose.getToneExample.
func (c *TranslationCore) AicomposeGetToneExample(in *mtproto.TLAicomposeGetToneExample) (*mtproto.AiComposeToneExample, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
