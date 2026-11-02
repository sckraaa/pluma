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

	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/catalog/internal/svc"
	"google.golang.org/protobuf/proto"

	"github.com/zeromicro/go-zero/core/logx"
)

type CatalogCore struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
	MD *metadata.RpcMetadata
}

func New(ctx context.Context, svcCtx *svc.ServiceContext) *CatalogCore {
	return &CatalogCore{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
		MD:     metadata.RpcMetadataFromIncoming(ctx),
	}
}

// stored reads the stored answer of method for arg into answer and returns
// its hash; found is false while the catalog has none, and the method then
// answers as Telegram does with nothing to give: an empty list, never an
// error, which Android asks again for every second
// (MediaDataController, docs/tasks/024-research/critique.md, A.4).
func (c *CatalogCore) stored(method, arg string, answer proto.Message) (hash int64, found bool, err error) {
	a, err := c.svcCtx.Catalog.Answer(c.ctx, method, arg)
	if err != nil {
		c.Logger.Errorf("%s - the catalog: %v", method, err)
		return 0, false, err
	}
	if a == nil {
		return 0, false, nil
	}
	if err := proto.Unmarshal(a.Data, answer); err != nil {
		c.Logger.Errorf("%s - a stored answer: %v", method, err)
		return 0, false, err
	}
	return a.Hash, true, nil
}

// notModified reports whether a client that sent hash has the answer that
// has stored: never for hash 0, which asks for the whole answer
// (core.telegram.org/api/offsets#hash-generation).
func notModified(hash, stored int64) bool {
	return hash != 0 && hash == stored
}
