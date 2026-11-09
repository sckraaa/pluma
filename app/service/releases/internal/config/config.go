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
	// ListenOn is the HTTP address; Caddy sends /dl/ of the site here.
	ListenOn string
	Log      logx.LogConf
	// PublicURL is the site, https://pluma.chat: the files are at
	// PublicURL/dl/..., the iPhone install page at PublicURL/iphone/install.
	PublicURL string
	// DataDir holds the files of the releases (a volume of its own).
	DataDir string
	// UploadTokenFile holds the token CI uploads with (Bearer), never
	// logged.
	UploadTokenFile string
	// AdminToken is the token of the panel's routes (/internal/releases,
	// docs/ops/releases.md); empty turns them off. Never logged.
	AdminToken string
	// Mysql holds app_releases.
	Mysql sqlx.Config
	// DfsClient and MediaClient store the APK of Android as a document,
	// which help.getAppUpdate offers.
	DfsClient   zrpc.RpcClientConf
	MediaClient zrpc.RpcClientConf
}
