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

package contactnotes

import (
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/teamgram/proto/mtproto"
)

func entity(predicate string, offset, length int32) *mtproto.MessageEntity {
	return &mtproto.MessageEntity{PredicateName: predicate, Offset: offset, Length: length}
}

// A note is kept up to LengthMax UTF-16 code units, cut at a character,
// with the entities that lie inside it; an empty one is no note.
func TestClean(t *testing.T) {
	if n := Clean(nil); n != nil {
		t.Errorf("no note: %v", n)
	}
	if n := Clean(&mtproto.TextWithEntities{Text: ""}); n != nil {
		t.Errorf("an empty note: %v", n)
	}
	if n := Clean(&mtproto.TextWithEntities{Text: "a\xffb"}); n != nil {
		t.Errorf("a text that is not UTF-8: %v", n)
	}

	short := Clean(&mtproto.TextWithEntities{Text: "likes tea", Entities: []*mtproto.MessageEntity{
		entity(mtproto.Predicate_messageEntityBold, 0, 5),
		entity(mtproto.Predicate_messageEntityItalic, 6, 3),
		entity(mtproto.Predicate_messageEntityBold, 6, 4),             // past the end
		entity(mtproto.Predicate_messageEntityBold, -1, 2),            // before the start
		entity(mtproto.Predicate_messageEntityBold, 2, 0),             // empty
		entity(mtproto.Predicate_inputMessageEntityMentionName, 0, 5), // only the sender reads it
	}})
	if short.GetPredicateName() != mtproto.Predicate_textWithEntities || short.GetText() != "likes tea" || len(short.GetEntities()) != 2 ||
		short.GetEntities()[0].GetLength() != 5 || short.GetEntities()[1].GetOffset() != 6 {
		t.Errorf("a short note: %v", short)
	}

	// 127 letters and an emoji of two code units: the emoji does not fit.
	long := Clean(&mtproto.TextWithEntities{Text: strings.Repeat("a", LengthMax-1) + "\U0001F600" + "tail", Entities: []*mtproto.MessageEntity{
		entity(mtproto.Predicate_messageEntityBold, 0, LengthMax-1),
		entity(mtproto.Predicate_messageEntityCustomEmoji, LengthMax-1, 2),
	}})
	if long.GetText() != strings.Repeat("a", LengthMax-1) || len(long.GetEntities()) != 1 {
		t.Errorf("a long note: %d characters, %d entities; want %d and 1", len(long.GetText()), len(long.GetEntities()), LengthMax-1)
	}
	// 126 letters and the emoji: exactly the limit.
	exact := Clean(&mtproto.TextWithEntities{Text: strings.Repeat("a", LengthMax-2) + "\U0001F600" + "tail"})
	if units := len(utf16.Encode([]rune(exact.GetText()))); units != LengthMax {
		t.Errorf("a note cut to %d code units, want %d", units, LengthMax)
	}
}
