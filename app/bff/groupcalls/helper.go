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

// Package groupcalls_helper is the phone.* methods of the group calls of
// chats and channels (task 093, ADR 0059) with the work that runs beside
// them: the collector and the intake of the SFU nodes' events.
package groupcalls_helper

import (
	"context"
	"sync"

	"github.com/teamgram/teamgram-server/app/bff/groupcalls/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/groupcalls/internal/core"
	"github.com/teamgram/teamgram-server/app/bff/groupcalls/internal/server/grpc/service"
	"github.com/teamgram/teamgram-server/app/bff/groupcalls/internal/sfuclient"
	"github.com/teamgram/teamgram-server/app/bff/groupcalls/internal/svc"
)

type (
	Config = config.Config
	// GroupCallsConfig is the media side of group calls.
	GroupCallsConfig = config.GroupCalls
)

// Module is the service and its background work over one set of
// connections.
type Module struct {
	*service.Service
	collector *core.Collector
	cancel    context.CancelFunc
	events    *sync.WaitGroup
	sfu       *sfuclient.Client
}

// New returns the methods and starts the collector and the intake of the
// events. Without SFU nodes the methods say group calls are off, and
// nothing runs beside them.
func New(c Config) *Module {
	svcCtx := svc.NewServiceContext(c)
	m := &Module{Service: service.New(svcCtx)}
	sfu, ok := svcCtx.SFU.(*sfuclient.Client)
	if !ok || !sfu.Enabled() {
		return m
	}
	m.sfu = sfu
	m.collector = core.StartCollector(svcCtx)
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.events = sfu.Watch(ctx, core.NewEvents(svcCtx).Handle)
	return m
}

// Stop stops the work beside the methods and waits for it.
func (m *Module) Stop() {
	if m.sfu == nil {
		return
	}
	m.cancel()
	m.events.Wait()
	m.collector.Stop()
	m.sfu.Close()
}
