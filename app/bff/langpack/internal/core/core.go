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
	"context"
	"errors"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/langpack/internal/svc"
	"github.com/teamgram/teamgram-server/pkg/langpack"

	"github.com/zeromicro/go-zero/core/logx"
)

type LangpackCore struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
	MD *metadata.RpcMetadata
}

func New(ctx context.Context, svcCtx *svc.ServiceContext) *LangpackCore {
	return &LangpackCore{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
		MD:     metadata.RpcMetadataFromIncoming(ctx),
	}
}

// pack returns the stored name of the pack a request asks for. Android and
// iOS send lang_pack "" (or use constructors without it) and mean the pack
// of their initConnection (LocaleController.java:3306, 3334;
// Localizations.swift:23), which the session passes in the metadata.
func (c *LangpackCore) pack(requested string) string {
	if requested == "" {
		requested = c.MD.GetLangpack()
	}
	return langpack.CanonicalPack(requested)
}

// language returns the stored language of pack, or LANG_PACK_INVALID for a
// pack the server does not have and LANG_CODE_NOT_SUPPORTED for a language
// the pack lacks (errors of the methods on core.telegram.org; which one
// Telegram answers when is not verified).
func (c *LangpackCore) language(pack, code string) (*langpack.Language, error) {
	if pack == "" {
		return nil, mtproto.ErrLangPackInvalid
	}
	l, err := c.svcCtx.Dao.Language(c.ctx, pack, langpack.CanonicalLangCode(code))
	if err == nil {
		return l, nil
	}
	if !errors.Is(err, langpack.ErrNotFound) {
		return nil, err
	}
	return nil, c.notFound(pack)
}

// notFound is the error for a language of pack that is not stored.
func (c *LangpackCore) notFound(pack string) error {
	ok, err := c.svcCtx.Dao.PackExists(c.ctx, pack)
	switch {
	case err != nil:
		return err
	case ok:
		return mtproto.ErrLangCodeNotSupported
	default:
		return mtproto.ErrLangPackInvalid
	}
}
