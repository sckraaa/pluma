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

package inbox

import (
	"fmt"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
)

// EmitSeqInbox adds to b's transaction the inbox event that gives the
// account of to the updates with seq the transaction's change sets for it
// (ADR 0021): the inbox consumer of msg journals each one with the next seq
// of to, in to's box transaction, and pushes it alone in an updates
// container to every device of to, with the users and chats it refers to
// as to sees them then. The event is in to's lane of inboxTopic after the
// events the transaction emitted before it, so a client gets the updates
// after the messages that go with them.
//
// The event is checked here, not when it is applied: an update without seq
// would make it dead and park every later event of to. EmitSeqInbox
// refuses (userbox.ErrInvalidEvent, the caller's transaction rolls back) a
// recipient that is nobody or b's own account (EmitSeqUpdate journals that
// one in b itself), an empty list, an update that does not go with seq
// (userbox.SeqUpdate) and a topic that is not of inbox events.
func EmitSeqInbox(b *userbox.Box, inboxTopic string, to int64, updates ...*mtproto.Update) error {
	if err := checkSeqInbox(b.UserId(), inboxTopic, to, updates); err != nil {
		return err
	}
	in := &TLInboxSeqUpdatesToInbox{
		UserId:  to,
		FromId:  b.UserId(),
		Updates: updates,
	}
	data, err := jsonx.Marshal(in)
	if err != nil {
		return err
	}
	b.Emit(userbox.Event{
		Topic:     inboxTopic,
		Recipient: to,
		Method:    string(proto.MessageName(in)),
		Data:      data,
	})
	return nil
}

// checkSeqInbox tells whether initiator may write the event of updates to
// to on topic (EmitSeqInbox).
func checkSeqInbox(initiator int64, topic string, to int64, updates []*mtproto.Update) error {
	switch {
	case to <= 0:
		return fmt.Errorf("%w: updates with seq to user %d", userbox.ErrInvalidEvent, to)
	case to == initiator:
		return fmt.Errorf("%w: updates with seq of user %d as an inbox event of their own", userbox.ErrInvalidEvent, to)
	case len(updates) == 0:
		return fmt.Errorf("%w: no updates with seq to user %d", userbox.ErrInvalidEvent, to)
	}
	if class, ok := userbox.TopicClassOf(topic); !ok || class != userbox.TopicInbox {
		return fmt.Errorf("%w: topic %q is not of inbox events", userbox.ErrInvalidEvent, topic)
	}
	for _, u := range updates {
		if !userbox.SeqUpdate(u.GetPredicateName()) {
			return fmt.Errorf("%w: %q to user %d is not an update with seq", userbox.ErrInvalidEvent, u.GetPredicateName(), to)
		}
	}
	return nil
}
