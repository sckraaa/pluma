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

package stories_helper

import (
	"github.com/teamgram/teamgram-server/app/bff/stories/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/stories/internal/core"
	"github.com/teamgram/teamgram-server/app/bff/stories/internal/server/grpc/service"
	"github.com/teamgram/teamgram-server/app/bff/stories/internal/svc"
)

type (
	Config = config.Config
	// Sweeper takes the stories out of the feeds when their period ends
	// and finishes what a request left undone.
	Sweeper = core.Sweeper
)

// Module is the stories of the server (task 084, ADR 0054) over one set of
// connections: the stories.* methods, what the other modules need of the
// stories, and the worker.
type Module struct {
	svcCtx *svc.ServiceContext
	// Service serves mtproto.RPCStories.
	Service *service.Service
	// UserStories answers the users and the messages modules.
	UserStories *UserStories
}

// New returns the stories over c.
func New(c Config) *Module {
	svcCtx := svc.NewServiceContext(c)
	return &Module{
		svcCtx:      svcCtx,
		Service:     service.New(svcCtx),
		UserStories: &UserStories{svcCtx: svcCtx},
	}
}

// StartSweeper starts the worker of the stories; Stop waits for it.
func (m *Module) StartSweeper() *Sweeper {
	return core.StartSweeper(m.svcCtx)
}
