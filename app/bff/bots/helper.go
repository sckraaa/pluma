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

// Package bots serves the methods of bot accounts at the MTProto level (task
// 070, ADR 0043): commands, texts and the menu button of a bot, button
// presses, inline queries and /start. The Bot API over HTTP and mini apps
// are out of its scope.
package bots

import (
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/server/grpc/service"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/svc"
)

type (
	// Config is the configuration of the service.
	Config = config.Config
)

// New returns the service.
func New(c Config) *service.Service {
	return service.New(svc.NewServiceContext(c))
}
