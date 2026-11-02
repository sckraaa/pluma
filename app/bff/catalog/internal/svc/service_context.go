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

package svc

import (
	"context"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/bff/catalog/internal/config"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/pkg/catalog"
)

// Catalog is what the handlers read of the catalog (catalog.Store).
type Catalog interface {
	Answer(ctx context.Context, method, arg string) (*catalog.Answer, error)
	SetByID(ctx context.Context, id int64) (*catalog.Set, error)
	SetByShortName(ctx context.Context, name string) (*catalog.Set, error)
	SetByKind(ctx context.Context, kind string) (*catalog.Set, error)
	CatalogDocuments(ctx context.Context, ids []int64) ([]int64, error)
}

type ServiceContext struct {
	Config  config.Config
	Catalog Catalog
	Media   media_client.MediaClient
}

func NewServiceContext(c config.Config) *ServiceContext {
	return &ServiceContext{
		Config:  c,
		Catalog: catalog.NewStore(sqlx.NewMySQL(&c.Mysql)),
		Media:   media_client.NewMediaClient(rpcx.GetCachedRpcClient(c.MediaClient)),
	}
}
