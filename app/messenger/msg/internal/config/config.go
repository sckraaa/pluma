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
	"github.com/teamgram/teamgram-server/pkg/mqconsumer"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	Mysql            sqlx.Config
	Cache            cache.CacheConf
	IdgenClient      zrpc.RpcClientConf
	BizServiceClient zrpc.RpcClientConf
	// Topics of the events msg writes; the relay of msg publishes them.
	Topics Topics
	// Relay publishes mq_outbox to Kafka (pkg/outbox).
	Relay outbox.RelayConf
	// InboxConsumer applies the inbox events (pkg/mqconsumer).
	InboxConsumer   mqconsumer.Conf
	MessageSharding int `json:",default=1"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// Topics are the Kafka topics of msg's events.
type Topics struct {
	Inbox string // inbox events, applied by msg itself
	Sync  string // pushes to users' devices
	// BotSync gets the pushes to bots; empty when bots are not served.
	BotSync string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}
