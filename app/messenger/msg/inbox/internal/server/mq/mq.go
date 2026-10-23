// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package mq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/internal/core"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/internal/svc"
	"github.com/teamgram/teamgram-server/pkg/mqconsumer"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// The consumer of inbox events (ADR 0012, p. 5). A copy in Kafka names the
// event's mq_outbox row (header event-id); the event is applied from the
// row by its recipient's box transaction, which deletes the row with the
// change. A copy of an applied event finds no row and changes nothing.

// loadTimeout bounds the read of an event's row before its transaction; the
// transaction has the limits of userbox.Run.
const loadTimeout = 5 * time.Second

// Outcomes of an inbox message, for mq_consumer_events_total.
const (
	outcomeDuplicate mqconsumer.Outcome = "duplicate"
	outcomeMismatch  mqconsumer.Outcome = "mismatch"
)

// event is an inbox event: addressed to user_id, caused by from_id.
type event interface {
	proto.Message
	GetUserId() int64
	GetFromId() int64
}

type handler struct {
	new   func() event
	apply func(c *core.InboxCore, in event) error
}

func on[T event](newT func() T, apply func(c *core.InboxCore, in T) error) handler {
	return handler{
		new:   func() event { return newT() },
		apply: func(c *core.InboxCore, in event) error { return apply(c, in.(T)) },
	}
}

var handlers = map[protoreflect.FullName]handler{
	proto.MessageName((*inbox.TLInboxSendUserMessageToInboxV2)(nil)): on(
		func() *inbox.TLInboxSendUserMessageToInboxV2 { return new(inbox.TLInboxSendUserMessageToInboxV2) },
		(*core.InboxCore).InboxSendUserMessageToInboxV2),
	proto.MessageName((*inbox.TLInboxEditMessageToInboxV2)(nil)): on(
		func() *inbox.TLInboxEditMessageToInboxV2 { return new(inbox.TLInboxEditMessageToInboxV2) },
		(*core.InboxCore).InboxEditMessageToInboxV2),
	proto.MessageName((*inbox.TLInboxDeleteMessagesToInbox)(nil)): on(
		func() *inbox.TLInboxDeleteMessagesToInbox { return new(inbox.TLInboxDeleteMessagesToInbox) },
		(*core.InboxCore).InboxDeleteMessagesToInbox),
	proto.MessageName((*inbox.TLInboxDeleteUserHistoryToInbox)(nil)): on(
		func() *inbox.TLInboxDeleteUserHistoryToInbox { return new(inbox.TLInboxDeleteUserHistoryToInbox) },
		(*core.InboxCore).InboxDeleteUserHistoryToInbox),
	proto.MessageName((*inbox.TLInboxDeleteChatHistoryToInbox)(nil)): on(
		func() *inbox.TLInboxDeleteChatHistoryToInbox { return new(inbox.TLInboxDeleteChatHistoryToInbox) },
		(*core.InboxCore).InboxDeleteChatHistoryToInbox),
	proto.MessageName((*inbox.TLInboxReadOutboxHistory)(nil)): on(
		func() *inbox.TLInboxReadOutboxHistory { return new(inbox.TLInboxReadOutboxHistory) },
		(*core.InboxCore).InboxReadOutboxHistory),
	proto.MessageName((*inbox.TLInboxReadMediaUnreadToInboxV2)(nil)): on(
		func() *inbox.TLInboxReadMediaUnreadToInboxV2 { return new(inbox.TLInboxReadMediaUnreadToInboxV2) },
		(*core.InboxCore).InboxReadMediaUnreadToInboxV2),
	proto.MessageName((*inbox.TLInboxUpdatePinnedMessage)(nil)): on(
		func() *inbox.TLInboxUpdatePinnedMessage { return new(inbox.TLInboxUpdatePinnedMessage) },
		(*core.InboxCore).InboxUpdatePinnedMessage),
	proto.MessageName((*inbox.TLInboxUnpinAllMessages)(nil)): on(
		func() *inbox.TLInboxUnpinAllMessages { return new(inbox.TLInboxUnpinAllMessages) },
		(*core.InboxCore).InboxUnpinAllMessages),
}

// errUndecodable marks an event whose row cannot be applied by any version
// of the code that wrote it: an unknown method, a payload that is not its
// JSON, a recipient that is not the row's or is its initiator.
var errUndecodable = fmt.Errorf("%w: undecodable inbox event", userbox.ErrMisuse)

// decode returns the request of an event row.
func decode(ev *outbox.Stored) (event, handler, error) {
	h, ok := handlers[protoreflect.FullName(ev.Method)]
	if !ok {
		return nil, h, fmt.Errorf("%w: unknown method %q", errUndecodable, ev.Method)
	}
	in := h.new()
	if err := json.Unmarshal(ev.Data, in); err != nil {
		return nil, h, fmt.Errorf("%w: %s: %v", errUndecodable, ev.Method, err)
	}
	// The initiator's own box is written in its transaction: an event to it
	// would write it twice.
	if in.GetUserId() != ev.Recipient || in.GetUserId() == in.GetFromId() {
		return nil, h, fmt.Errorf("%w: %s to user %d from %d in the row of user %d", errUndecodable, ev.Method, in.GetUserId(), in.GetFromId(), ev.Recipient)
	}
	return in, h, nil
}

// eventId is the id of the row a copy names, 0 when it names none.
func eventId(m *mqconsumer.Message) int64 {
	id, err := strconv.ParseInt(m.Header(outbox.HeaderEventId), 10, 64)
	if err != nil || id < 0 {
		return 0
	}
	return id
}

// Consumer handles the copies of inbox events.
type Consumer struct {
	svcCtx *svc.ServiceContext
}

func New(svcCtx *svc.ServiceContext) *Consumer { return &Consumer{svcCtx: svcCtx} }

// Handle applies the event a copy names. A transient error leaves the copy
// to be handled again; a deterministic one, after the policy's attempts,
// makes the event dead (Dead).
func (c *Consumer) Handle(ctx context.Context, m *mqconsumer.Message) (mqconsumer.Outcome, error) {
	log := logx.WithContext(ctx)
	id := eventId(m)
	if id <= 0 {
		log.Errorf("inbox: copy %s %d/%d without an event id: skipped", m.Topic, m.Partition, m.Offset)
		return mqconsumer.OutcomeSkipped, nil
	}
	// Without a transaction or a lock: most copies after a lost offset or
	// a repeated publication end here.
	lctx, cancel := context.WithTimeout(ctx, loadTimeout)
	ev, err := outbox.Load(lctx, c.svcCtx.Dao.BoxDB, id)
	cancel()
	if err != nil {
		return "", err
	}
	if ev == nil {
		return outcomeDuplicate, nil
	}
	if ev.Topic != m.Topic || strconv.FormatInt(ev.Recipient, 10) != string(m.Key) ||
		strconv.FormatInt(ev.CreatedAt, 10) != m.Header(outbox.HeaderCreatedAt) {
		// Another event with the same id: the ids were reused against the
		// procedures (a restore without raising AUTO_INCREMENT). Its own
		// copy comes separately or the redelivery timer sends it.
		log.Errorf("inbox: copy %d/%d of event %d does not match its row (topic %s, user %d): skipped",
			m.Partition, m.Offset, id, ev.Topic, ev.Recipient)
		return outcomeMismatch, nil
	}
	if ev.Trace != "" {
		ctx = propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier{outbox.HeaderTraceParent: ev.Trace})
	}
	in, h, err := decode(ev)
	if err != nil {
		logx.WithContext(ctx).Errorf("inbox: event %d: %v; dead", id, err)
		if err = core.Fail(ctx, c.svcCtx, id, ev.Recipient, err, 1); err != nil {
			return "", err
		}
		return mqconsumer.OutcomeDead, nil
	}
	ic := core.New(ctx, c.svcCtx, id)
	if err = h.apply(ic, in); err != nil {
		return "", err
	}
	switch o := ic.Outcome(); o {
	case userbox.TakeWaiting, userbox.TakeParked:
		return "parked", nil
	default:
		return mqconsumer.Outcome(o.String()), nil
	}
}

// Policy is the consumer's: userbox.Deterministic errors, three attempts,
// then the event is dead.
func (c *Consumer) Policy() mqconsumer.Policy {
	return mqconsumer.Policy{
		Deterministic: userbox.Deterministic,
		Attempts:      mqconsumer.DefaultAttempts,
		Dead: func(ctx context.Context, m *mqconsumer.Message, cause error, attempts int) error {
			id := eventId(m)
			recipient, err := strconv.ParseInt(string(m.Key), 10, 64)
			if err != nil || recipient <= 0 {
				return errors.Join(fmt.Errorf("inbox: copy of event %d with key %q", id, m.Key), err)
			}
			logx.WithContext(ctx).Errorf("inbox: event %d to user %d not applied after %d attempts, dead: %s", id, recipient, attempts, userbox.EventError(cause))
			return core.Fail(ctx, c.svcCtx, id, recipient, cause, attempts)
		},
	}
}
