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

// Package core is the translation of messages with a model of our own (task
// 101, ADR 0064): messages.translateText, messages.togglePeerTranslations,
// the translation form of messages.composeMessageWithAI, and the refusals of
// the AI editor (aicompose.*). What the clients expect of each step is cited
// at the step; the sources were read at Android 7be672ea (13.2.1), tdesktop
// v7.4.1, Telegram-iOS 24638bc69b and Web K aa74832e.
package core

import (
	"context"
	"time"

	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/translation/internal/svc"
	"github.com/teamgram/teamgram-server/pkg/translate"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/status"
)

// internalWait is the X of the FLOOD_WAIT_X a translation answers when a
// service it reads from failed. Never 500: Android and tdesktop repeat a
// 500 without end (ADR 0064, "Повторы транспорта"); after FLOOD_WAIT_X
// they repeat once, X seconds later.
const internalWait = 10 * time.Second

type TranslationCore struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
	MD *metadata.RpcMetadata
}

func New(ctx context.Context, svcCtx *svc.ServiceContext) *TranslationCore {
	return &TranslationCore{
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

// refuse counts a request the handler answers itself and returns err.
func refuse(method, result string, err error) error {
	translate.CountRequest(method, result)
	return err
}

// readFailed answers a translation whose texts could not be read.
func (c *TranslationCore) readFailed(method, step string, err error) error {
	c.Logger.Errorf("%s - %s: %v", method, step, err)
	return refuse(method, translate.ResultInternal, translate.FloodWait(internalWait))
}
