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

// Package catalog_helper serves the catalog imported from Telegram (task
// 024, ADR 0028): reactions, effects, sticker and emoji sets, emoji
// keywords and groups, colors, themes, wallpapers, the default emoji lists
// and time zones, by the methods Telegram answers them with.
package catalog_helper

import (
	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/bff/catalog/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/catalog/internal/server/grpc/service"
	"github.com/teamgram/teamgram-server/app/bff/catalog/internal/svc"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/pkg/catalog"
	"github.com/teamgram/teamgram-server/pkg/catalog/usersets"
	"github.com/zeromicro/go-zero/zrpc"
)

type (
	Config = config.Config
)

func New(c Config) *service.Service {
	return service.New(svc.NewServiceContext(c))
}

// SetThumbFiles finds the files of the thumbnails of sets for upload.getFile
// (the plugin of app/bff/files): the sets users make have their own (task
// 084).
func SetThumbFiles(mysql sqlx.Config, mediaClient zrpc.RpcClientConf) usersets.ThumbFiles {
	return usersets.ThumbFiles{
		Sets:  catalog.NewStore(sqlx.NewMySQL(&mysql)),
		Media: media_client.NewMediaClient(rpcx.GetCachedRpcClient(mediaClient)),
	}
}
