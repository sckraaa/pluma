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

package svc

import (
	"fmt"

	"github.com/teamgram/teamgram-server/app/bff/voipcalls/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/voipcalls/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/phonecalls"
	"github.com/teamgram/teamgram-server/pkg/phonecalls/ice"
)

type ServiceContext struct {
	Config config.Config
	*dao.Dao
	Ice *ice.Servers
	// CallParameters is the custom_parameters of every call, "" for none.
	CallParameters string
}

// NewServiceContext loads the config: wrong ICE servers stop the service.
func NewServiceContext(c config.Config) *ServiceContext {
	servers, err := ice.Parse(c.Calls.IceServers, c.Calls.TurnSecret)
	if err != nil {
		panic(fmt.Errorf("voipcalls: Calls.IceServers: %w", err))
	}
	params, err := phonecalls.CustomParameters(c.Calls.Quality)
	if err != nil {
		panic(fmt.Errorf("voipcalls: Calls.Quality: %w", err))
	}
	return &ServiceContext{Config: c, Dao: dao.New(c), Ice: servers, CallParameters: params}
}
