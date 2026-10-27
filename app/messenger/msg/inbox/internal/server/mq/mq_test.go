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

package mq

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"google.golang.org/protobuf/proto"
)

// Every event of the inbox schema has its handler: the row of an event
// without one would be dead (an unknown method) once a writer emits it.
func TestEveryInboxEventHandled(t *testing.T) {
	fd := inbox.File_inbox_tl_proto
	n := 0
	for i := range fd.Messages().Len() {
		md := fd.Messages().Get(i)
		if !strings.HasPrefix(string(md.Name()), "TL_inbox_") {
			continue
		}
		n++
		if _, ok := handlers[md.FullName()]; !ok {
			t.Errorf("%s has no handler", md.FullName())
		}
	}
	if n != len(handlers) {
		t.Errorf("%d events in the schema, %d handlers", n, len(handlers))
	}
}

// The event of updates with seq decodes from its row like the others; one
// addressed to its initiator is undecodable.
func TestDecodeSeqUpdatesToInbox(t *testing.T) {
	row := func(in *inbox.TLInboxSeqUpdatesToInbox) *outbox.Stored {
		data, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		return &outbox.Stored{Event: userbox.Event{Recipient: 7, Method: string(proto.MessageName(in)), Data: data}}
	}
	chat := mtproto.MakeTLUpdateChat(&mtproto.Update{ChatId_INT64: 5}).To_Update()
	in, _, err := decode(row(&inbox.TLInboxSeqUpdatesToInbox{UserId: 7, FromId: 3, Updates: []*mtproto.Update{chat}}))
	if err != nil {
		t.Fatal(err)
	}
	ev := in.(*inbox.TLInboxSeqUpdatesToInbox)
	if ev.GetFromId() != 3 || len(ev.GetUpdates()) != 1 || ev.GetUpdates()[0].GetPredicateName() != mtproto.Predicate_updateChat ||
		ev.GetUpdates()[0].GetChatId_INT64() != 5 {
		t.Errorf("decoded %v", ev)
	}
	_, _, err = decode(row(&inbox.TLInboxSeqUpdatesToInbox{UserId: 7, FromId: 7, Updates: []*mtproto.Update{chat}}))
	if !errors.Is(err, errUndecodable) || !userbox.Deterministic(err) {
		t.Errorf("event to its initiator: %v", err)
	}
}
