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

	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	KV    kv.KvConf   // 2FA of channels.editCreator: SRP sessions and limits
	Mysql sqlx.Config // 2FA passwords
	// The channels themselves (biz channel), their users (biz user) and
	// their invite links, kept with those of basic groups (biz chat).
	ChannelClient zrpc.RpcClientConf
	UserClient    zrpc.RpcClientConf
	ChatClient    zrpc.RpcClientConf
	// AuthsessionClient says when the session was logged in (channels.editCreator).
	AuthsessionClient zrpc.RpcClientConf
	// MsgClient tells has_scheduled of the full channel (task 061).
	MsgClient zrpc.RpcClientConf
	// MediaClient stores the photos of channels.editPhoto (media).
	MediaClient zrpc.RpcClientConf
	// StatsDc is channelFull.stats_dc of those who may view the statistics
	// (task 103, ADR 0066): bff's Stats.StatsDc(), this server's DC while
	// the statistics are served, 0 while they are off and can_view_stats is
	// never set.
	StatsDc int32 `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}
