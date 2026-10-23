// Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
//  All rights reserved.
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package mq

import (
	"context"
	"encoding/json"

	"github.com/teamgram/teamgram-server/app/messenger/sync/internal/core"
	"github.com/teamgram/teamgram-server/app/messenger/sync/internal/svc"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/pkg/mqconsumer"
	"github.com/teamgram/teamgram-server/pkg/outbox"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// The consumer of pushes (Sync-T): best effort. A push is done once it was
// handed to the sessions of the user's devices; a push that is not
// understood is skipped. A push that does not reach a device is caught up
// with updates.getDifference (ADR 0012, p. 7).

const outcomePushed mqconsumer.Outcome = "pushed"

type pushHandler struct {
	new    func() proto.Message
	handle func(c *core.SyncCore, in proto.Message) error
}

func on[T proto.Message](newT func() T, handle func(c *core.SyncCore, in T) error) pushHandler {
	return pushHandler{
		new:    func() proto.Message { return newT() },
		handle: func(c *core.SyncCore, in proto.Message) error { return handle(c, in.(T)) },
	}
}

func void[T proto.Message, R any](f func(c *core.SyncCore, in T) (R, error)) func(c *core.SyncCore, in T) error {
	return func(c *core.SyncCore, in T) error {
		_, err := f(c, in)
		return err
	}
}

var handlers = map[protoreflect.FullName]pushHandler{
	proto.MessageName((*sync.TLSyncUpdatesMe)(nil)): on(
		func() *sync.TLSyncUpdatesMe { return new(sync.TLSyncUpdatesMe) }, void((*core.SyncCore).SyncUpdatesMe)),
	proto.MessageName((*sync.TLSyncUpdatesNotMe)(nil)): on(
		func() *sync.TLSyncUpdatesNotMe { return new(sync.TLSyncUpdatesNotMe) }, void((*core.SyncCore).SyncUpdatesNotMe)),
	proto.MessageName((*sync.TLSyncPushUpdates)(nil)): on(
		func() *sync.TLSyncPushUpdates { return new(sync.TLSyncPushUpdates) }, void((*core.SyncCore).SyncPushUpdates)),
	proto.MessageName((*sync.TLSyncPushRpcResult)(nil)): on(
		func() *sync.TLSyncPushRpcResult { return new(sync.TLSyncPushRpcResult) }, void((*core.SyncCore).SyncPushRpcResult)),
	proto.MessageName((*sync.TLSyncBroadcastUpdates)(nil)): on(
		func() *sync.TLSyncBroadcastUpdates { return new(sync.TLSyncBroadcastUpdates) }, void((*core.SyncCore).SyncBroadcastUpdates)),
}

// Consumer hands the pushes of Sync-T to the sessions.
type Consumer struct {
	svcCtx *svc.ServiceContext
}

// Handle pushes one record. It never fails: an error of a push is logged
// by the push, and the device catches up.
func (c *Consumer) Handle(ctx context.Context, m *mqconsumer.Message) (mqconsumer.Outcome, error) {
	if tp := m.Header(outbox.HeaderTraceParent); tp != "" {
		ctx = propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier{outbox.HeaderTraceParent: tp})
	}
	log := logx.WithContext(ctx)
	method := m.Header(outbox.HeaderMethod)
	h, ok := handlers[protoreflect.FullName(method)]
	if !ok {
		log.Errorf("sync: push %d/%d of unknown method %q: skipped", m.Partition, m.Offset, method)
		return mqconsumer.OutcomeSkipped, nil
	}
	in := h.new()
	if err := json.Unmarshal(m.Value, in); err != nil {
		log.Errorf("sync: push %d/%d: decode %s: %v; skipped", m.Partition, m.Offset, method, err)
		return mqconsumer.OutcomeSkipped, nil
	}
	if err := h.handle(core.New(ctx, c.svcCtx), in); err != nil {
		log.Errorf("sync: push %d/%d %s: %v", m.Partition, m.Offset, method, err)
	}
	return outcomePushed, nil
}

// New returns the consumer of pushes.
func New(svcCtx *svc.ServiceContext, conf mqconsumer.Conf) *mqconsumer.Consumer {
	return mqconsumer.New(conf, "sync", &Consumer{svcCtx: svcCtx}, mqconsumer.Policy{})
}
