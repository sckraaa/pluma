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

package config

import (
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	Name string
	// ListenOn is the HTTP address; Caddy sends the site's paths that are
	// no files of the site here (teamgramd/deploy/web/Caddyfile).
	ListenOn string
	Log      logx.LogConf
	// SiteURL is the link host's site, https://pluma.chat; WebURL the web
	// client, https://web.pluma.chat.
	SiteURL string
	WebURL  string
	// BizServiceClient reads users and channels, DfsClient their photos.
	BizServiceClient zrpc.RpcClientConf
	DfsClient        zrpc.RpcClientConf
	// Mysql holds the sticker sets of the catalog.
	Mysql sqlx.Config
}
