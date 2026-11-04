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
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	Mysql sqlx.Config
	// Cache is biz dialog's cache of dialog rows: a box transaction that
	// writes a user's dialog with a channel deletes its key after commit.
	Cache cache.CacheConf
	// MediaClient loads the photos of channels.
	MediaClient zrpc.RpcClientConf
	// Topics of the events the box transactions of the service write
	// (ADR 0012, 0034): the updates with seq of a user's own channels.
	Topics Topics
}

// Topics are the Kafka topics of the service's events.
type Topics struct {
	Sync string // pushes to devices (Sync-T)
}
