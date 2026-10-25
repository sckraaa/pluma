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

// Package mq reads Sync-T, the pushes of sync, in a group of its own and
// hands the events of one user to the core.
package mq

import (
	"context"
	"encoding/json"

	"github.com/teamgram/teamgram-server/app/messenger/push/internal/core"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/pkg/mqconsumer"
	"github.com/teamgram/teamgram-server/pkg/outbox"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Outcomes of mq_consumer_events_total.
const (
	outcomeHandled mqconsumer.Outcome = "handled"
	outcomeIgnored mqconsumer.Outcome = "ignored"
)

// Handler handles the events of Sync-T.
type Handler interface {
	Handle(ctx context.Context, ev *core.Event)
}

// events turns the methods of Sync-T into events. Pushes of RPC results and
// broadcasts carry no notification.
var events = map[protoreflect.FullName]func(data []byte) (*core.Event, error){
	proto.MessageName((*sync.TLSyncPushUpdates)(nil)): func(data []byte) (*core.Event, error) {
		var in sync.TLSyncPushUpdates
		err := json.Unmarshal(data, &in)
		return &core.Event{UserId: in.GetUserId(), Updates: in.GetUpdates()}, err
	},
	proto.MessageName((*sync.TLSyncUpdatesNotMe)(nil)): func(data []byte) (*core.Event, error) {
		var in sync.TLSyncUpdatesNotMe
		err := json.Unmarshal(data, &in)
		return &core.Event{UserId: in.GetUserId(), Except: in.GetPermAuthKeyId(), Updates: in.GetUpdates()}, err
	},
	proto.MessageName((*sync.TLSyncUpdatesMe)(nil)): func(data []byte) (*core.Event, error) {
		var in sync.TLSyncUpdatesMe
		err := json.Unmarshal(data, &in)
		return &core.Event{UserId: in.GetUserId(), Only: in.GetPermAuthKeyId(), Updates: in.GetUpdates()}, err
	},
}

// Consumer sends the pushes of Sync-T.
type Consumer struct {
	handler Handler
}

// Handle handles one record. It never fails: a push that is not sent is
// counted, and the device catches up when it opens.
func (c *Consumer) Handle(ctx context.Context, m *mqconsumer.Message) (mqconsumer.Outcome, error) {
	if tp := m.Header(outbox.HeaderTraceParent); tp != "" {
		ctx = propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier{outbox.HeaderTraceParent: tp})
	}
	decode, ok := events[protoreflect.FullName(m.Header(outbox.HeaderMethod))]
	if !ok {
		return outcomeIgnored, nil
	}
	ev, err := decode(m.Value)
	if err != nil {
		logx.WithContext(ctx).Errorf("push: record %d/%d: %v; skipped", m.Partition, m.Offset, err)
		return mqconsumer.OutcomeSkipped, nil
	}
	if ev.UserId == 0 || ev.Updates == nil {
		return outcomeIgnored, nil
	}
	c.handler.Handle(ctx, ev)
	return outcomeHandled, nil
}

// New returns the consumer of conf for h.
func New(conf mqconsumer.Conf, h Handler) *mqconsumer.Consumer {
	return mqconsumer.New(conf, "push", &Consumer{handler: h}, mqconsumer.Policy{})
}
