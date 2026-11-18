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
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"

	"github.com/zeromicro/go-zero/zrpc"
)

// Calls is the media side of the calls (docs/ops/calls.md).
type Calls struct {
	// IceServers are the STUN and TURN servers the clients get, comma
	// separated: "host:port:stun+turn", "host:port:stun+turn@label" for a
	// TURN server with a secret of its own. Empty: calls are off and
	// phone.requestCall says so.
	IceServers string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// TurnSecret is the static-auth-secret of coturn; the credentials of a
	// call are made from it. Required when a server has "turn" and no
	// label.
	TurnSecret string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// TurnSecrets are the secrets of the labelled TURN servers,
	// "label=secret" comma separated (task 109: the node in Russia has its
	// own). A label in IceServers without its secret stops the service.
	TurnSecrets string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Quality is the profile of the call parameters, "pluma" (empty) or
	// "stock", and changes to it: phonecalls.CustomParameters,
	// docs/ops/calls.md.
	Quality string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// ClientConfig is the answer to phone.getCallConfig; "{}" if empty.
	ClientConfig string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

type Config struct {
	zrpc.RpcServerConf
	Mysql        sqlx.Config
	Calls        Calls
	UserClient   zrpc.RpcClientConf
	MsgClient    zrpc.RpcClientConf
	StatusClient zrpc.RpcClientConf
	SyncClient   sync_client.OutboxConf
}
