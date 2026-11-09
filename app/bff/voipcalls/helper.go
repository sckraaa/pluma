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

package voipcalls_helper

import (
	"github.com/teamgram/teamgram-server/app/bff/voipcalls/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/voipcalls/internal/core"
	"github.com/teamgram/teamgram-server/app/bff/voipcalls/internal/server/grpc/service"
	"github.com/teamgram/teamgram-server/app/bff/voipcalls/internal/svc"
)

type (
	Config = config.Config
	// CallsConfig is the media side of the calls.
	CallsConfig = config.Calls
	// Sweeper ends the calls their clients abandoned.
	Sweeper = core.Sweeper
)

// New returns the phone.* methods of one-to-one calls (task 068).
func New(c Config) *service.Service {
	return service.New(svc.NewServiceContext(c))
}

// StartSweeper starts the sweep of expired calls; Stop waits for it.
func StartSweeper(c Config) *Sweeper {
	return core.StartSweeper(svc.NewServiceContext(c))
}
