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

package dao

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dal/dataobject"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// An edit of the state of a widget alone is updateMessageWidgetData of the
// owner's copy, by the chat as the owner sees it, with the fields that
// changed; any other edit is updateEditMessage (task 106).
func TestWidgetDataUpdate(t *testing.T) {
	const owner, bot = int64(10), int64(20)
	widget := mtproto.MakeTLMessage(&mtproto.Message{
		Id: 5, Message: "caption",
		Media: mtproto.MakeTLMessageMediaWidget(&mtproto.MessageMedia{
			Payload: []byte{0x78, 1}, CanQuery: true,
			PublicData: wrapperspb.String("p"), PublicDataHash: wrapperspb.Int64(1), ChatData: wrapperspb.String("c"),
		}).To_MessageMedia(),
	}).To_Message()
	private := &dataobject.MessagesDO{UserMessageBoxId: 7, DialogId1: owner, DialogId2: bot}
	group := &dataobject.MessagesDO{UserMessageBoxId: 8, DialogId1: -int64(mtproto.PEER_CHAT), DialogId2: 30}
	edit := func(f func(m *mtproto.Message)) *mtproto.Message {
		m := proto.Clone(widget).(*mtproto.Message)
		f(m)
		return m
	}

	u := widgetDataUpdate(owner, private, widget, edit(func(m *mtproto.Message) { m.Media.ChatData = wrapperspb.String("") }))
	if u.GetPredicateName() != mtproto.Predicate_updateMessageWidgetData || u.GetMsgId_INT32() != 7 ||
		u.GetPeer_PEER().GetUserId() != bot || u.GetChatData() == nil || u.GetChatData().GetValue() != "" || u.GetPublicData() != nil {
		t.Fatalf("chat state cleared: %v", u)
	}
	u = widgetDataUpdate(owner, group, widget, edit(func(m *mtproto.Message) {
		m.Media.PublicData, m.Media.PublicDataHash = wrapperspb.String("p2"), wrapperspb.Int64(2)
	}))
	if u.GetPeer_PEER().GetChatId() != 30 || u.GetPublicData().GetValue() != "p2" || u.GetPublicDataHash().GetValue() != 2 || u.GetChatData() != nil {
		t.Fatalf("public state in a group: %v", u)
	}

	for name, other := range map[string]*mtproto.Message{
		"the caption too": edit(func(m *mtproto.Message) { m.Message, m.Media.ChatData = "new", wrapperspb.String("x") }),
		"with an edit date": edit(func(m *mtproto.Message) {
			m.EditDate, m.Media.ChatData = wrapperspb.Int32(1), wrapperspb.String("x")
		}),
		"the payload":   edit(func(m *mtproto.Message) { m.Media.Payload = []byte{0x78, 2} }),
		"nothing":       edit(func(*mtproto.Message) {}),
		"another media": edit(func(m *mtproto.Message) { m.Media = mtproto.MakeTLMessageMediaEmpty(nil).To_MessageMedia() }),
	} {
		if u := widgetDataUpdate(owner, private, widget, other); u != nil {
			t.Errorf("%s: %v, want an ordinary edit", name, u)
		}
	}
}
