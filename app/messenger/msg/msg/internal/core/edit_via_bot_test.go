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

package core

import (
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// A bot edits only a message sent via itself, and one already sent.
func TestCheckViaBotEdit(t *testing.T) {
	const bot = int64(20)
	request := func(stored, edited int64, scheduled bool) *msg.TLMsgEditMessageV2 {
		via := func(id int64) *wrapperspb.Int64Value {
			if id == 0 {
				return nil
			}
			return wrapperspb.Int64(id)
		}
		out := &msg.OutboxMessage{Message: mtproto.MakeTLMessage(&mtproto.Message{Message: "new", ViaBotId: via(edited)}).To_Message()}
		if scheduled {
			out.ScheduleDate = wrapperspb.Int32(1)
		}
		return &msg.TLMsgEditMessageV2{
			UserId:     10,
			EditType:   msg.EditTypeViaBot,
			NewMessage: msg.MakeTLOutboxMessage(out).To_OutboxMessage(),
			DstMessage: &mtproto.MessageBox{MessageId: 9, Message: mtproto.MakeTLMessage(&mtproto.Message{Message: "old", ViaBotId: via(stored)}).To_Message()},
		}
	}
	if err := checkViaBotEdit(request(bot, bot, false)); err != nil {
		t.Fatalf("the bot's edit: %v", err)
	}
	for name, c := range map[string]struct {
		in   *msg.TLMsgEditMessageV2
		want error
	}{
		"a message sent by its sender": {request(0, bot, false), mtproto.ErrMessageIdInvalid},
		"another bot's message":        {request(21, bot, false), mtproto.ErrMessageIdInvalid},
		"an edit naming no bot":        {request(bot, 0, false), mtproto.ErrInputRequestInvalid},
		"a waiting message":            {request(bot, bot, true), mtproto.ErrInputRequestInvalid},
	} {
		if err := checkViaBotEdit(c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
}
