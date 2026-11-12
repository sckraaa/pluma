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

// Package core is messages.transcribeAudio and messages.rateTranscribedAudio
// with their worker (task 090, ADR 0056). What the clients expect of each
// step is cited at the step; the sources were read at Android 7be672ea,
// tdesktop v7.4.1, Telegram-iOS release-13.2 and Web K aa74832.
package core

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/transcription/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/status"
)

// Errors of core.telegram.org/method/messages.transcribeAudio that the
// mtproto package does not define.
var (
	errVoiceMissing = status.Error(mtproto.ErrBadRequest, "MSG_VOICE_MISSING")
)

type TranscriptionCore struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
	MD *metadata.RpcMetadata
}

func New(ctx context.Context, svcCtx *svc.ServiceContext) *TranscriptionCore {
	return &TranscriptionCore{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
		MD:     metadata.RpcMetadataFromIncoming(ctx),
	}
}

// isError tells whether err is the RPC error want, here or as another
// service answered it.
func isError(err, want error) bool {
	a, b := status.Convert(err), status.Convert(want)
	return a.Code() == b.Code() && a.Message() == b.Message()
}
