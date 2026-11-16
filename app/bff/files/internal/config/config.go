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
	"github.com/teamgram/teamgram-server/pkg/staticmap"
	"github.com/teamgram/teamgram-server/pkg/webfile"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	Mysql       sqlx.Config // the encrypted files (pkg/secretchat)
	DfsClient   zrpc.RpcClientConf
	UserClient  zrpc.RpcClientConf
	MediaClient zrpc.RpcClientConf
	// WebFile is upload.getWebFile (ADR 0062, p. 5): the key of the
	// addresses it proxies and the cache of the files it fetched.
	WebFile webfile.Config `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// KV keeps the maps rendered and the counts of the limit per user
	// (pkg/staticmap); the Redis of bff.
	KV kv.KvConf `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Maps renders the maps of inputWebFileGeoPointLocation (task 100, ADR
	// 0063, docs/ops/maps.md); off without a URL, and such a map is
	// LOCATION_INVALID. Maps need KV.
	Maps staticmap.Config `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}
