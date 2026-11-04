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

package channel_helper

import (
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/config"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/server/grpc/service"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/svc"
)

type (
	Config = config.Config
	Topics = config.Topics
)

// New returns the channel service: reads of channels and their
// participants (ADR 0034).
func New(c Config) *service.Service {
	return service.New(svc.NewServiceContext(c))
}
