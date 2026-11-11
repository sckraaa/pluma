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

package channelcontents

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// voice is a voice message of channel 5.
func voice(id int32) *mtproto.Message {
	doc := mtproto.MakeTLDocument(&mtproto.Document{
		Id: 1, MimeType: "audio/ogg",
		Attributes: []*mtproto.DocumentAttribute{mtproto.MakeTLDocumentAttributeAudio(&mtproto.DocumentAttribute{Voice: true, Duration: 3}).To_DocumentAttribute()},
	}).To_Document()
	return mtproto.MakeTLMessage(&mtproto.Message{
		Id: id, PeerId: mtproto.MakePeerChannel(5),
		Media: mtproto.MakeTLMessageMediaDocument(&mtproto.MessageMedia{Document: doc}).To_MessageMedia(),
	}).To_Message()
}

// round is a round video message of channel 5.
func round(id int32) *mtproto.Message {
	doc := mtproto.MakeTLDocument(&mtproto.Document{
		Id: 2, MimeType: "video/mp4",
		Attributes: []*mtproto.DocumentAttribute{mtproto.MakeTLDocumentAttributeVideo(&mtproto.DocumentAttribute{RoundMessage: true, W: 384, H: 384}).To_DocumentAttribute()},
	}).To_Document()
	return mtproto.MakeTLMessage(&mtproto.Message{
		Id: id, PeerId: mtproto.MakePeerChannel(5),
		Media: mtproto.MakeTLMessageMediaDocument(&mtproto.MessageMedia{Document: doc}).To_MessageMedia(),
	}).To_Message()
}

func text(id int32) *mtproto.Message {
	return mtproto.MakeTLMessage(&mtproto.Message{Id: id, PeerId: mtproto.MakePeerChannel(5), Message: "t"}).To_Message()
}

func TestConsumable(t *testing.T) {
	if !Consumable(voice(1)) || !Consumable(round(2)) {
		t.Error("a voice or round video message is not consumable")
	}
	if Consumable(text(3)) {
		t.Error("a text is consumable")
	}
	service := mtproto.MakeTLMessageService(&mtproto.Message{Id: 4, Action: mtproto.MakeTLMessageActionPinMessage(&mtproto.MessageAction{}).To_MessageAction()}).To_Message()
	if Consumable(service) {
		t.Error("a service message is consumable")
	}
}

// TestView: a voice message is unread for a member until it plays it,
// never for its author or one outside the channel; a mention is unread
// until it is read.
func TestView(t *testing.T) {
	const viewer = int64(7)
	s := &State{
		read:     map[key]bool{{viewer, 2}: true},
		mentions: map[key]bool{{viewer, 3}: true, {viewer, 4}: false},
	}
	cases := []struct {
		name                 string
		m                    *mtproto.Message
		own, member, out     bool
		unread, wasMentioned bool
	}{
		{name: "not played", m: voice(1), member: true, unread: true},
		{name: "round not played", m: round(1), member: true, unread: true},
		{name: "played", m: voice(2), member: true},
		{name: "own post", m: voice(1), member: true, own: true},
		{name: "out", m: voice(1), member: true, out: true},
		{name: "not a member", m: voice(1)},
		{name: "text", m: text(1), member: true},
		{name: "unread mention", m: text(3), member: true, unread: true, wasMentioned: true},
		{name: "read mention", m: text(4), member: true, wasMentioned: true},
		{name: "unread mention, not a member", m: text(3), wasMentioned: true},
	}
	for _, c := range cases {
		m := c.m
		m.Out = c.out
		m.MediaUnread = true // what a forward may have brought
		s.View(m, viewer, c.own, c.member)
		if m.GetMediaUnread() != c.unread || m.GetMentioned() != c.wasMentioned {
			t.Errorf("%s: media_unread %v mentioned %v, want %v %v", c.name, m.GetMediaUnread(), m.GetMentioned(), c.unread, c.wasMentioned)
		}
	}
	var none *State
	m := voice(1)
	none.View(m, viewer, false, true)
	if !m.GetMediaUnread() {
		t.Error("nil state: a voice message is not unread")
	}
	m = voice(1)
	s.View(m, 0, false, true)
	if m.GetMediaUnread() {
		t.Error("no viewer: unread")
	}
}

type fakeQuerier struct {
	queries []string
	args    [][]any
}

func (q *fakeQuerier) QueryRowsPartial(_ context.Context, _ interface{}, query string, args ...interface{}) error {
	q.queries = append(q.queries, query)
	q.args = append(q.args, args)
	return nil
}

// TestLoadQueries: the plays are read for the consumable messages only,
// the mentions for all, of one user or of all; no user and no messages
// read nothing.
func TestLoadQueries(t *testing.T) {
	q := &fakeQuerier{}
	if _, err := Load(context.Background(), q, 5, 7, []*mtproto.Message{voice(1), text(2), mtproto.MakeTLMessageEmpty(&mtproto.Message{Id: 3}).To_Message()}); err != nil {
		t.Fatal(err)
	}
	if len(q.queries) != 2 || !strings.Contains(q.queries[0], "channel_contents_read") || !strings.Contains(q.queries[1], "channel_message_mentions") {
		t.Fatalf("queries %q", q.queries)
	}
	if !reflect.DeepEqual(q.args[0], []any{int64(5), int64(7), int32(1)}) || !reflect.DeepEqual(q.args[1], []any{int64(5), int64(7), int32(1), int32(2)}) {
		t.Errorf("args %v", q.args)
	}

	// Of all users, the authors of the voice messages as well: a push
	// comes from whoever changed them.
	q = &fakeQuerier{}
	if _, err := LoadAll(context.Background(), q, 5, []*mtproto.Message{voice(1)}); err != nil {
		t.Fatal(err)
	}
	if len(q.queries) != 3 || !strings.Contains(q.queries[1], "sender_user_id FROM channel_messages") || !reflect.DeepEqual(q.args[1], []any{int64(5), int32(1)}) {
		t.Errorf("authors: %q %v", q.queries, q.args)
	}

	q = &fakeQuerier{}
	if _, err := LoadAll(context.Background(), q, 5, []*mtproto.Message{text(2)}); err != nil {
		t.Fatal(err)
	}
	if len(q.queries) != 1 || strings.Contains(q.queries[0], "user_id = ?") || !reflect.DeepEqual(q.args[0], []any{int64(5), int32(2)}) {
		t.Errorf("all users: %q %v", q.queries, q.args)
	}

	q = &fakeQuerier{}
	if _, err := Load(context.Background(), q, 5, 0, []*mtproto.Message{voice(1)}); err != nil || len(q.queries) != 0 {
		t.Errorf("no user: %q, %v", q.queries, err)
	}
	if _, err := Load(context.Background(), q, 5, 7, nil); err != nil || len(q.queries) != 0 {
		t.Errorf("no messages: %q, %v", q.queries, err)
	}
}
