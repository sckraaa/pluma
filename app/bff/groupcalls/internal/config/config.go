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
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"

	"github.com/zeromicro/go-zero/zrpc"
)

// Node is one SFU (app/service/sfu) the calls may run on (ADR 0059 §3).
// Any node takes any call: every group call becomes end-to-end encrypted
// (ADR 0060 §3), and the signalling side does not choose a node by what
// it may see.
type Node struct {
	// Name is how the calls' table names the node (group_calls.sfu_node);
	// it must not change while calls run on it.
	Name string
	// Endpoint is host:port of the node's control API; empty leaves the
	// node out.
	Endpoint string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// ServerName is the name of the node's certificate; the host of
	// Endpoint when empty.
	ServerName string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// GroupCalls is the media side of group calls (docs/ops/group-calls.md).
type GroupCalls struct {
	// Nodes are the SFUs, in the order of preference. None (or none with
	// an endpoint): group calls are off, and phone.createGroupCall and
	// phone.joinGroupCall say so.
	Nodes []Node `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// CertFile and KeyFile are the client certificate of the control API;
	// CAFile signs the nodes' certificates. Required with Nodes: the API
	// is mutual TLS only.
	CertFile string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	KeyFile  string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	CAFile   string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// VideoLimit is how many cameras are on at once in a call
	// (groupCall.unmuted_video_limit); it should match the nodes'
	// Limits.MaxVideos and appConfig groupcall_video_participants_max.
	VideoLimit int32 `json:",default=30"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// AbsentTimeout: a participant the SFU has not held for this long
	// leaves the call (ADR 0059 §10).
	AbsentTimeout time.Duration `json:",default=30s"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// RequestTimeout bounds one call of a node's control API.
	RequestTimeout time.Duration `json:",default=5s"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// ChainKeep: the blocks of the encryption of a call that is over are
	// deleted this long after its end (ADR 0060 §2).
	ChainKeep time.Duration `json:",default=1h"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

type Config struct {
	zrpc.RpcServerConf
	Mysql         sqlx.Config
	GroupCalls    GroupCalls
	UserClient    zrpc.RpcClientConf
	ChatClient    zrpc.RpcClientConf
	ChannelClient zrpc.RpcClientConf
	MsgClient     zrpc.RpcClientConf
	SyncClient    sync_client.OutboxConf
}
