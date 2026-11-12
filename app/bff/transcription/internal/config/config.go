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
	"github.com/teamgram/teamgram-server/pkg/transcribe"

	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	Mysql sqlx.Config // transcription_jobs, transcription_requests, mq_outbox
	// Transcribe is the engine and the limits (docs/ops/transcription.md);
	// without a URL transcription is off.
	Transcribe transcribe.Config
	// MessageClient and ChannelClient read the message asked about, as
	// its reader sees it.
	MessageClient zrpc.RpcClientConf
	ChannelClient zrpc.RpcClientConf
	// DfsClient reads the record.
	DfsClient zrpc.RpcClientConf
	// SyncTopic: the final updates go to the reader's devices through
	// mq_outbox, as the other pushes of bff.
	SyncTopic string
}
