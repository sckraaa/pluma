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
	"fmt"
	"strconv"
	"time"

	kafka "github.com/teamgram/marmota/pkg/mq"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/internal/core"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/internal/svc"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Retries of an event whose box transaction failed with a retryable error
// (userbox.Retryable): a brief failure of MySQL or a busy user must not
// lose the event. The consumer is sequential, so meanwhile the partition
// waits (ADR 0011, p. 9). Until task 003 an event that never applies is
// logged, counted and acknowledged.
const (
	retryBudget   = 30 * time.Second
	retryFirst    = 100 * time.Millisecond
	retryMaxPause = 5 * time.Second
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

// New returns the consumer of inbox events.
func New(svcCtx *svc.ServiceContext, conf kafka.KafkaConsumerConf) *kafka.ConsumerGroup {
	s := kafka.MustKafkaConsumer(&conf)
	s.RegisterHandlers(conf.Topics[0], func(ctx context.Context, method, key string, value []byte) {
		Handle(ctx, svcCtx, method, key, value)
	})
	return s
}

// Handle applies one inbox event: the box transaction of its recipient,
// retried on retryable errors within retryBudget. An event that is not
// applied is logged with its method, recipient and messages, without
// content, and counted.
func Handle(ctx context.Context, svcCtx *svc.ServiceContext, method, key string, value []byte) {
	log := logx.WithContext(ctx)
	h, ok := handlers[protoreflect.FullName(method)]
	if !ok {
		dao.InboxHandlerFailed("unknown")
		log.Errorf("inbox: unknown event %q", method)
		return
	}
	in := h.new()
	if err := json.Unmarshal(value, in); err != nil {
		dao.InboxHandlerFailed(method)
		log.Errorf("inbox: decode %s: %v", method, err)
		return
	}
	// The initiator's own box is written in its transaction: an event to
	// it would write it twice.
	if in.GetUserId() <= 0 || in.GetUserId() == in.GetFromId() {
		dao.InboxHandlerFailed(method)
		log.Errorf("inbox: %s rejected: recipient %d, from %d", method, in.GetUserId(), in.GetFromId())
		return
	}
	if key != strconv.FormatInt(in.GetUserId(), 10) {
		log.Errorf("inbox: %s to user %d published with key %q", method, in.GetUserId(), key)
	}

	deadline := time.Now().Add(retryBudget)
	pause := retryFirst
	for attempt := 1; ; attempt++ {
		err := h.apply(core.New(ctx, svcCtx), in)
		if err == nil {
			return
		}
		if !userbox.Retryable(err) || time.Now().Add(pause).After(deadline) {
			dao.InboxHandlerFailed(method)
			log.Errorf("inbox: %s to user %d from %d (%s) not applied after %d attempts: %v",
				method, in.GetUserId(), in.GetFromId(), messagesOf(in), attempt, err)
			return
		}
		select {
		case <-ctx.Done():
			dao.InboxHandlerFailed(method)
			log.Errorf("inbox: %s to user %d from %d (%s) not applied: %v", method, in.GetUserId(), in.GetFromId(), messagesOf(in), ctx.Err())
			return
		case <-time.After(pause):
		}
		pause = min(2*pause, retryMaxPause)
	}
}

// messagesOf names the messages of an event by dialog_message_id, to find
// the event again.
func messagesOf(in event) string {
	switch e := in.(type) {
	case *inbox.TLInboxSendUserMessageToInboxV2:
		ids := make([]int64, 0, len(e.BoxList))
		for _, b := range e.BoxList {
			ids = append(ids, b.GetDialogMessageId())
		}
		return fmt.Sprintf("dialog_message_id %v", ids)
	case *inbox.TLInboxEditMessageToInboxV2:
		return fmt.Sprintf("dialog_message_id %d", e.GetNewMessage().GetDialogMessageId())
	case *inbox.TLInboxDeleteMessagesToInbox:
		return fmt.Sprintf("dialog_message_id %v", e.Id)
	case *inbox.TLInboxReadMediaUnreadToInboxV2:
		return fmt.Sprintf("dialog_message_id %v", e.Id)
	case *inbox.TLInboxReadOutboxHistory:
		return fmt.Sprintf("dialog_message_id %d", e.MaxDialogMessageId)
	case *inbox.TLInboxUpdatePinnedMessage:
		return fmt.Sprintf("dialog_message_id %d", e.DialogMessageId)
	}
	return "dialog"
}
