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

// Package stargifts_helper serves the star gifts of mtproto.RPCGifts (task
// 020). Pluma has no payments (ADR 0016), so nobody holds a star gift: the
// list of a user's gifts is empty, as Telegram answers for a user without
// gifts, and the methods that buy, upgrade or move gifts are not served.
package stargifts_helper

import (
	"github.com/teamgram/teamgram-server/app/bff/stargifts/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/stargifts/internal/server/grpc/service"
	"github.com/teamgram/teamgram-server/app/bff/stargifts/internal/svc"
)

type (
	Config = config.Config
)

func New(c Config) *service.Service {
	return service.New(svc.NewServiceContext(c))
}
