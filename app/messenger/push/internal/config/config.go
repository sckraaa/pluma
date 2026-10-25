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
	"github.com/teamgram/teamgram-server/pkg/mqconsumer"
	"github.com/teamgram/teamgram-server/pkg/pushprovider"

	"github.com/zeromicro/go-zero/zrpc"
)

// Config of the push service (task 009, ADR 0017). It runs no gRPC server;
// the RpcServerConf gives logs, the metrics endpoint and tracing.
type Config struct {
	zrpc.RpcServerConf
	// Mysql keeps the devices (pkg/pushdevices), whose secrets
	// PushSecretEncryptionKey opens.
	Mysql                   sqlx.Config
	PushSecretEncryptionKey string
	// PushConsumer reads Sync-T in a group of its own.
	PushConsumer      mqconsumer.Conf
	StatusClient      zrpc.RpcClientConf
	AuthsessionClient zrpc.RpcClientConf
	BizServiceClient  zrpc.RpcClientConf // notify settings, messages
	APNs              pushprovider.APNsConfig
	FCM               pushprovider.FCMConfig
}
