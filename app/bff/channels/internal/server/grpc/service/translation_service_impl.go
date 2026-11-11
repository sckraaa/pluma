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
	"github.com/teamgram/teamgram-server/app/bff/channels/internal/core"
)

// The translations (RPCTranslation): the autotranslation of a channel is a
// setting of the channel (task 082, ADR 0050); the translation of texts is
// not served yet (task 027).

var _ mtproto.RPCTranslationServer = (*Service)(nil)

// ChannelsToggleAutotranslation
// channels.toggleAutotranslation#167fc0a1 channel:InputChannel enabled:Bool = Updates;
func (s *Service) ChannelsToggleAutotranslation(ctx context.Context, request *mtproto.TLChannelsToggleAutotranslation) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.toggleAutotranslation - metadata: {%s}", c.MD)
	return c.ChannelsToggleAutotranslation(request)
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
