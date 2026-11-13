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
	"time"

	"github.com/teamgram/teamgram-server/app/bff/groupcalls/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/groupcalls/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/groupcalls/internal/e2emon"
	"github.com/teamgram/teamgram-server/app/bff/groupcalls/internal/sfuclient"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"
)

type ServiceContext struct {
	Config config.Config
	Dao    *dao.Dao
	// SFU is the nodes of the media (docs/ops/group-calls.md).
	SFU sfuclient.API
	// VideoLimit is groupCall.unmuted_video_limit.
	VideoLimit int32
	// AbsentTimeout: one the SFU has not held for this long leaves.
	AbsentTimeout time.Duration
	// ChainKeep: the blocks of a call that is over go this long after it.
	ChainKeep time.Duration
	// E2E watches the encryption of the calls (ADR 0060 §5).
	E2E *e2emon.Monitor
	// Members are the group states of the chains the reads check.
	Members *ChainMembers
}

// NewServiceContext connects the nodes of c: a node's configuration that
// is wrong (its certificates, its address) stops the service.
func NewServiceContext(c config.Config) *ServiceContext {
	sfu, err := sfuclient.New(c.GroupCalls)
	if err != nil {
		panic(fmt.Errorf("groupcalls: GroupCalls: %w", err))
	}
	limit := c.GroupCalls.VideoLimit
	if limit <= 0 {
		limit = groupcalls.DefaultVideoLimit
	}
	absent := c.GroupCalls.AbsentTimeout
	if absent <= 0 {
		absent = 30 * time.Second
	}
	keep := c.GroupCalls.ChainKeep
	if keep <= 0 {
		keep = time.Hour
	}
	d := dao.New(c)
	return &ServiceContext{
		Config: c, Dao: d, SFU: sfu, VideoLimit: limit, AbsentTimeout: absent, ChainKeep: keep,
		E2E: e2emon.New(d.Store), Members: NewChainMembers(),
	}
}
