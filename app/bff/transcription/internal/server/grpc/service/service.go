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
	"github.com/teamgram/teamgram-server/app/bff/transcription/internal/core"
	"github.com/teamgram/teamgram-server/app/bff/transcription/internal/svc"
)

type Service struct {
	svcCtx *svc.ServiceContext
}

func New(ctx *svc.ServiceContext) *Service {
	return &Service{svcCtx: ctx}
}

// MessagesTranscribeAudio
// messages.transcribeAudio#269e9a49 peer:InputPeer msg_id:int = messages.TranscribedAudio;
func (s *Service) MessagesTranscribeAudio(ctx context.Context, request *mtproto.TLMessagesTranscribeAudio) (*mtproto.Messages_TranscribedAudio, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.transcribeAudio - metadata: {%s}", c.MD)
	return c.MessagesTranscribeAudio(request)
}

// MessagesRateTranscribedAudio
// messages.rateTranscribedAudio#7f1d072f peer:InputPeer msg_id:int transcription_id:long good:Bool = Bool;
func (s *Service) MessagesRateTranscribedAudio(ctx context.Context, request *mtproto.TLMessagesRateTranscribedAudio) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.rateTranscribedAudio - metadata: {%s}", c.MD)
	return c.MessagesRateTranscribedAudio(request)
}
