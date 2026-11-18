// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package config

import (
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/iosupdate"
	"github.com/teamgram/teamgram-server/pkg/staticmap"
	"github.com/teamgram/teamgram-server/pkg/translate"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	Mysql             sqlx.Config // dismissed suggestions
	UserClient        zrpc.RpcClientConf
	AuthsessionClient zrpc.RpcClientConf // the client's time zone, its app (help.getAppUpdate)
	// MediaClient gives the APK of an Android release (help.getAppUpdate).
	MediaClient zrpc.RpcClientConf
	// ReleasesURL is where the releases are, the site's /dl/ (task 054):
	// the update prefix of the desktop app (help.getConfig) and the install
	// page of the iPhone app.
	ReleasesURL string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// IOSUpdate lets an Ad Hoc build of the iPhone app update from the app
	// (task 085); off without its secret.
	IOSUpdate iosupdate.Settings `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Translate is the translation engine of bff (task 101, ADR 0064);
	// without its URL the app config hides the translation from the
	// clients.
	Translate translate.Config `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// GifSearch is whether @gif searches GIFs by text (KLIPY, task 099,
	// ADR 0062): the app config then offers the emoji categories of the GIF
	// tab, which it leaves empty otherwise.
	GifSearch bool `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Maps is the Maps block of bff (task 112, ADR 0070): with its
	// PublicURL the app config hands the clients the public map tiles of
	// their interactive maps and the styles there.
	Maps staticmap.Config `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Edge is the edges of the patched clients, pluma_edge of the app
	// config (task 109, ADR 0072, docs/ops/edge.md): no edges without
	// WSURLs.
	Edge EdgeConfig `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// EdgeConfig is the Edge block of bff.yaml, parsed by appconfig.ParseEdges
// at the start.
type EdgeConfig struct {
	// WSURLs is EDGE_WS_URLS, the edges by name:
	// "gcore=wss://<prefix>.gcorecdn.com/apiws,ru=wss://ru.pluma.chat/apiws".
	WSURLs string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Routes is EDGE_ROUTES, the edges and their order by the country of
	// the client: "RU:edge,direct:ru,gcore;*:direct,edge:gcore,ru".
	Routes string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// StickyS is EDGE_STICKY_S, how many seconds a client keeps to an edge
	// it switched to ("1800").
	StickyS string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}
