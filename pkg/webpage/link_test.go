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

package webpage

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func TestFirstLink(t *testing.T) {
	url := func(o, l int32) *mtproto.MessageEntity {
		return mtproto.MakeTLMessageEntityUrl(&mtproto.MessageEntity{Offset: o, Length: l}).To_MessageEntity()
	}
	textURL := func(o, l int32, u string) *mtproto.MessageEntity {
		return mtproto.MakeTLMessageEntityTextUrl(&mtproto.MessageEntity{Offset: o, Length: l, Url: u}).To_MessageEntity()
	}
	bold := mtproto.MakeTLMessageEntityBold(&mtproto.MessageEntity{Offset: 0, Length: 3}).To_MessageEntity()
	for _, tc := range []struct {
		name     string
		message  string
		entities []*mtproto.MessageEntity
		want     string
	}{
		{"none", "no links here", nil, ""},
		{"entity", "see https://a.example/x", []*mtproto.MessageEntity{url(4, 19)}, "https://a.example/x"},
		// Offsets count UTF-16 units: the emoji is two of them.
		{"after an emoji", "😀 a.example", []*mtproto.MessageEntity{url(3, 9)}, "a.example"},
		{"hidden link first", "here and https://b.example",
			[]*mtproto.MessageEntity{url(9, 17), textURL(0, 4, "https://hidden.example/")}, "https://hidden.example/"},
		{"visible link first", "https://b.example and here",
			[]*mtproto.MessageEntity{textURL(22, 4, "https://hidden.example/"), url(0, 17)}, "https://b.example"},
		{"out of range", "short", []*mtproto.MessageEntity{url(2, 40)}, ""},
		{"no entities", "look at github.com/teamgram and more", []*mtproto.MessageEntity{bold}, "github.com/teamgram"},
	} {
		if got := FirstLink(tc.message, tc.entities); got != tc.want {
			t.Errorf("%s: FirstLink = %q, want %q", tc.name, got, tc.want)
		}
	}
}
