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
)

func TestIsEmptyDraft(t *testing.T) {
	topicOnly := mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{TopMsgId: mtproto.MakeFlagsInt32(7)}).To_InputReplyTo()
	toMessage := mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{ReplyToMsgId: 5}).To_InputReplyTo()
	toStory := mtproto.MakeTLInputReplyToStory(&mtproto.InputReplyTo{StoryId: 3}).To_InputReplyTo()
	media := mtproto.MakeTLInputMediaWebPage(&mtproto.InputMedia{Url: "https://example.com"}).To_InputMedia()
	cases := []struct {
		name  string
		in    *mtproto.TLMessagesSaveDraft
		reply *mtproto.InputReplyTo
		want  bool
	}{
		{"nothing", &mtproto.TLMessagesSaveDraft{}, nil, true},
		{"no_webpage only", &mtproto.TLMessagesSaveDraft{NoWebpage: true}, nil, true},
		{"topic only", &mtproto.TLMessagesSaveDraft{}, topicOnly, true},
		{"text", &mtproto.TLMessagesSaveDraft{Message: "x"}, nil, false},
		{"reply", &mtproto.TLMessagesSaveDraft{}, toMessage, false},
		{"story reply", &mtproto.TLMessagesSaveDraft{}, toStory, false},
		{"media", &mtproto.TLMessagesSaveDraft{Media: media}, nil, false},
	}
	for _, c := range cases {
		if got := isEmptyDraft(c.in, c.reply); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDraftReplyToOlderLayers(t *testing.T) {
	r := draftReplyTo(&mtproto.TLMessagesSaveDraft{ReplyToMsgId: mtproto.MakeFlagsInt32(5), TopMsgId: mtproto.MakeFlagsInt32(9)})
	if r.GetReplyToMsgId() != 5 || r.GetTopMsgId().GetValue() != 9 {
		t.Errorf("reply %v", r)
	}
	if r := draftReplyTo(&mtproto.TLMessagesSaveDraft{}); r != nil {
		t.Errorf("reply of a draft with none: %v", r)
	}
}

func TestDraftEntitiesMentions(t *testing.T) {
	entities := draftEntities(11, []*mtproto.MessageEntity{
		mtproto.MakeTLMessageEntityBold(&mtproto.MessageEntity{Offset: 0, Length: 1}).To_MessageEntity(),
		mtproto.MakeTLInputMessageEntityMentionName(&mtproto.MessageEntity{
			Offset: 1, Length: 2,
			UserId_INPUTUSER: mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: 22}).To_InputUser(),
		}).To_MessageEntity(),
		mtproto.MakeTLInputMessageEntityMentionName(&mtproto.MessageEntity{
			Offset: 3, Length: 2,
			UserId_INPUTUSER: mtproto.MakeTLInputUserSelf(nil).To_InputUser(),
		}).To_MessageEntity(),
	})
	if len(entities) != 3 || entities[0].GetPredicateName() != mtproto.Predicate_messageEntityBold {
		t.Fatalf("entities %v", entities)
	}
	for i, want := range map[int]int64{1: 22, 2: 11} {
		if e := entities[i]; e.GetPredicateName() != mtproto.Predicate_messageEntityMentionName || e.GetUserId_INT64() != want {
			t.Errorf("entity %d: %v, want a mention of %d", i, e, want)
		}
	}
}
