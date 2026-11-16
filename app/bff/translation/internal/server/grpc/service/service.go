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

package service

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/translation/internal/core"
	"github.com/teamgram/teamgram-server/app/bff/translation/internal/svc"
)

// Service is RPCTranslation and RPCAiComposeTone (task 101, ADR 0064).
type Service struct {
	svcCtx *svc.ServiceContext
}

var (
	_ mtproto.RPCTranslationServer   = (*Service)(nil)
	_ mtproto.RPCAiComposeToneServer = (*Service)(nil)
)

func New(ctx *svc.ServiceContext) *Service {
	return &Service{svcCtx: ctx}
}

// MessagesTranslateText
// messages.translateText#a5eec345 flags:# peer:flags.0?InputPeer id:flags.0?Vector<int> text:flags.1?Vector<TextWithEntities> to_lang:string tone:flags.2?string = messages.TranslatedText;
func (s *Service) MessagesTranslateText(ctx context.Context, request *mtproto.TLMessagesTranslateText) (*mtproto.Messages_TranslatedText, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.translateText - metadata: {%s}", c.MD)
	return c.MessagesTranslateText(request)
}

// MessagesTogglePeerTranslations
// messages.togglePeerTranslations#e47cb579 flags:# disabled:flags.0?true peer:InputPeer = Bool;
func (s *Service) MessagesTogglePeerTranslations(ctx context.Context, request *mtproto.TLMessagesTogglePeerTranslations) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.togglePeerTranslations - metadata: {%s}", c.MD)
	return c.MessagesTogglePeerTranslations(request)
}

// ChannelsToggleAutotranslation
// channels.toggleAutotranslation#167fc0a1 channel:InputChannel enabled:Bool = Updates;
func (s *Service) ChannelsToggleAutotranslation(ctx context.Context, request *mtproto.TLChannelsToggleAutotranslation) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	return c.ChannelsToggleAutotranslation(request)
}

// MessagesComposeMessageWithAI
// messages.composeMessageWithAI#daecc589 flags:# proofread:flags.0?true emojify:flags.3?true text:TextWithEntities translate_to_lang:flags.1?string tone:flags.2?InputAiComposeTone = messages.ComposedMessageWithAI;
func (s *Service) MessagesComposeMessageWithAI(ctx context.Context, request *mtproto.TLMessagesComposeMessageWithAI) (*mtproto.Messages_ComposedMessageWithAI, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.composeMessageWithAI - metadata: {%s}", c.MD)
	return c.MessagesComposeMessageWithAI(request)
}

// AicomposeCreateTone
// aicompose.createTone#4aa83913 flags:# display_author:flags.0?true emoji_id:long title:string prompt:string = AiComposeTone;
func (s *Service) AicomposeCreateTone(ctx context.Context, request *mtproto.TLAicomposeCreateTone) (*mtproto.AiComposeTone, error) {
	return core.New(ctx, s.svcCtx).AicomposeCreateTone(request)
}

// AicomposeUpdateTone
// aicompose.updateTone#903bcf59 flags:# tone:InputAiComposeTone display_author:flags.0?Bool emoji_id:flags.1?long title:flags.2?string prompt:flags.3?string = AiComposeTone;
func (s *Service) AicomposeUpdateTone(ctx context.Context, request *mtproto.TLAicomposeUpdateTone) (*mtproto.AiComposeTone, error) {
	return core.New(ctx, s.svcCtx).AicomposeUpdateTone(request)
}

// AicomposeSaveTone
// aicompose.saveTone#1782cbb1 tone:InputAiComposeTone unsave:Bool = Bool;
func (s *Service) AicomposeSaveTone(ctx context.Context, request *mtproto.TLAicomposeSaveTone) (*mtproto.Bool, error) {
	return core.New(ctx, s.svcCtx).AicomposeSaveTone(request)
}

// AicomposeDeleteTone
// aicompose.deleteTone#dd39316a tone:InputAiComposeTone = Bool;
func (s *Service) AicomposeDeleteTone(ctx context.Context, request *mtproto.TLAicomposeDeleteTone) (*mtproto.Bool, error) {
	return core.New(ctx, s.svcCtx).AicomposeDeleteTone(request)
}

// AicomposeGetTone
// aicompose.getTone#b2e8ba03 tone:InputAiComposeTone = aicompose.Tones;
func (s *Service) AicomposeGetTone(ctx context.Context, request *mtproto.TLAicomposeGetTone) (*mtproto.Aicompose_Tones, error) {
	return core.New(ctx, s.svcCtx).AicomposeGetTone(request)
}

// AicomposeGetTones
// aicompose.getTones#abd59201 hash:long = aicompose.Tones;
func (s *Service) AicomposeGetTones(ctx context.Context, request *mtproto.TLAicomposeGetTones) (*mtproto.Aicompose_Tones, error) {
	return core.New(ctx, s.svcCtx).AicomposeGetTones(request)
}

// AicomposeGetToneExample
// aicompose.getToneExample#d1b4ab14 tone:InputAiComposeTone num:int = AiComposeToneExample;
func (s *Service) AicomposeGetToneExample(ctx context.Context, request *mtproto.TLAicomposeGetToneExample) (*mtproto.AiComposeToneExample, error) {
	return core.New(ctx, s.svcCtx).AicomposeGetToneExample(request)
}
