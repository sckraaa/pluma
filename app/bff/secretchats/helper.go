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

// Package secretchats_helper serves the secret chat methods of the client
// API (task 069).
package secretchats_helper

import (
	"github.com/teamgram/teamgram-server/app/bff/secretchats/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/secretchats/internal/core"
	"github.com/teamgram/teamgram-server/app/bff/secretchats/internal/server/grpc/service"
	"github.com/teamgram/teamgram-server/app/bff/secretchats/internal/svc"

	"github.com/zeromicro/go-zero/zrpc"
)

type (
	Config = config.Config
)

func init() {
	// Requests and pushes carry ciphertext and DH values: not for the logs.
	for _, m := range []string{"messages_sendEncrypted", "messages_sendEncryptedFile", "messages_sendEncryptedService", "messages_requestEncryption", "messages_acceptEncryption"} {
		zrpc.DontLogContentForMethod("/mtproto.RPCSecretChats/" + m)
	}
}

// New returns the service and starts the janitor of the chats.
func New(c Config) *service.Service {
	ctx := svc.NewServiceContext(c)
	core.StartSweeper(ctx)
	return service.New(ctx)
}
