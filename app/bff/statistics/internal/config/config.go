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
	"fmt"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/statsgraph"

	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	// Mysql is only read: the events of pkg/channelstats and the tables of
	// posts, views, forwards, reactions, stories and members.
	Mysql sqlx.Config
	// ChannelClient reads the channel and the viewer's participant row (the
	// rights of the statistics).
	ChannelClient zrpc.RpcClientConf
	// UserClient reads the users the statistics name as the viewer sees
	// them.
	UserClient zrpc.RpcClientConf
	Stats      StatsConfig
}

// StatsConfig is the statistics of channels and groups (task 103, ADR
// 0066), in bff's config.
type StatsConfig struct {
	// ThisDc is channelFull.stats_dc: the DC the clients send the requests
	// of the statistics to, this server's own (this_dc of
	// teamgramd/bin/config.json). iOS sends them to DC 0 without it
	// (TelegramCore PeerStatistics.swift:318-319), Web K shows no
	// statistics without it (appProfileManager.ts:1444-1455).
	ThisDc int32 `json:",default=1"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// TokenKey signs the zoom tokens of the graphs (pkg/statsgraph,
	// STATS_GRAPH_TOKEN_KEY: the standard base64 of 32 random bytes or
	// more). Without it the statistics are off: stats.* answer
	// ERR_ENTERPRISE_IS_BLOCKED and can_view_stats is never set.
	TokenKey string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// Enabled tells whether the statistics are served.
func (c StatsConfig) Enabled() bool {
	return c.TokenKey != ""
}

// StatsDc is what channelFull.stats_dc carries next to can_view_stats:
// ThisDc while the statistics are served, 0 while they are off, when
// channels.getFullChannel sets neither and the clients offer no statistics.
func (c StatsConfig) StatsDc() int32 {
	if !c.Enabled() {
		return 0
	}
	return c.ThisDc
}

// Validate checks the settings; the errors never hold the key.
func (c StatsConfig) Validate() error {
	if c.ThisDc < 1 {
		return fmt.Errorf("stats: ThisDc %d", c.ThisDc)
	}
	if c.Enabled() {
		if _, err := statsgraph.ParseKey(c.TokenKey); err != nil {
			return fmt.Errorf("stats: STATS_GRAPH_TOKEN_KEY: %w", err)
		}
	}
	return nil
}
