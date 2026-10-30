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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func entity(predicate string, offset, length int32) *mtproto.MessageEntity {
	return &mtproto.MessageEntity{PredicateName: predicate, Offset: offset, Length: length}
}

func predicates(es []*mtproto.MessageEntity) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.GetPredicateName())
	}
	return out
}

// An edited message keeps the formatting the client sends with the new
// text; an edit that keeps the text keeps its formatting, and the
// automatic entities are left to msg, which makes them again.
func TestEditedEntities(t *testing.T) {
	bold := entity(mtproto.Predicate_messageEntityBold, 0, 4)
	old := []*mtproto.MessageEntity{
		bold,
		entity(mtproto.Predicate_messageEntityTextUrl, 5, 4),
		entity(mtproto.Predicate_messageEntityUrl, 10, 12),
		entity(mtproto.Predicate_messageEntityMention, 23, 5),
		entity(mtproto.Predicate_messageEntityHashtag, 29, 4),
		entity(mtproto.Predicate_messageEntityBotCommand, 34, 6),
		entity(mtproto.Predicate_messageEntityMentionName, 41, 3),
	}
	italic := entity(mtproto.Predicate_messageEntityItalic, 2, 3)
	for name, c := range map[string]struct {
		in   *mtproto.TLMessagesEditMessage
		want []string
	}{
		"new text with entities": {
			&mtproto.TLMessagesEditMessage{Message: wrapperspb.String("hello"), Entities: []*mtproto.MessageEntity{italic}},
			[]string{mtproto.Predicate_messageEntityItalic},
		},
		"new plain text": {
			&mtproto.TLMessagesEditMessage{Message: wrapperspb.String("hello")},
			[]string{},
		},
		"entities only": {
			&mtproto.TLMessagesEditMessage{Entities: []*mtproto.MessageEntity{italic}},
			[]string{mtproto.Predicate_messageEntityItalic},
		},
		"text kept": {
			&mtproto.TLMessagesEditMessage{},
			[]string{mtproto.Predicate_messageEntityBold, mtproto.Predicate_messageEntityTextUrl, mtproto.Predicate_messageEntityMentionName},
		},
	} {
		got := predicates(editedEntities(old, c.in))
		if len(got) != len(c.want) {
			t.Errorf("%s: %v, want %v", name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: %v, want %v", name, got, c.want)
				break
			}
		}
	}
}
