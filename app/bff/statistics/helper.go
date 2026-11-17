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

package statistics_helper

import (
	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/server/grpc/service"
	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/svc"
)

type (
	Config = config.Config
	// StatsConfig is the section Stats of bff's config: stats_dc and the
	// key of the zoom tokens.
	StatsConfig = config.StatsConfig
)

// New returns RPCStatistics, the statistics of channels and groups (task
// 103, ADR 0066). It panics on settings it cannot run with.
func New(c Config) *service.Service {
	return service.New(svc.NewServiceContext(c))
}
