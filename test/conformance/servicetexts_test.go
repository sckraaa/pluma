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

package conformance

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func bold(offset, length int32) *mtproto.MessageEntity {
	return mtproto.MakeTLMessageEntityBold(&mtproto.MessageEntity{Offset: offset, Length: length}).To_MessageEntity()
}

// The entities count UTF-16 units: an emoji before the bold text takes two.
func TestServiceTextsMarkup(t *testing.T) {
	got := markup("❗️Код для входа: 12345. Не давайте код никому", []*mtproto.MessageEntity{bold(2, 14), bold(39, 6)})
	want := "❗️**Код для входа:** 12345. Не давайте код **никому**"
	if got != want {
		t.Fatalf("markup:\n got %q\nwant %q", got, want)
	}
}

// Nothing of the account is left: the code, the name, the date and time,
// the address, the device and the place, a website code.
func TestServiceTextsScrub(t *testing.T) {
	self := mtproto.MakeTLUser(&mtproto.User{
		FirstName: wrapperspb.String("Анна"),
		LastName:  wrapperspb.String("Иванова"),
	}).To_User()
	s := newScrubber(self)
	in := "**Код для входа в Telegram:** 54321. Не давайте код **никому**!\n\n" +
		"**Новый вход.** Анна Иванова, вход 29/10/2026 в 15:00:20 UTC.\n\n" +
		"Устройство: Telegram Desktop, 7.0.9, Mac14,2, macOS 26.5\n" +
		"Место: Berlin, Germany (IP = 203.0.113.9)\n\n" +
		"Вот он:\nZrxtbd8YNdk"
	want := "**Код для входа в Telegram:** {code}. Не давайте код **никому**!\n\n" +
		"**Новый вход.** {name}, вход {date} в {time} UTC.\n\n" +
		"Устройство: {value}\n" +
		"Место: {value}\n\n" +
		"Вот он:\n{code}"
	if got := s.text(in); got != want {
		t.Fatalf("scrub:\n got %q\nwant %q", got, want)
	}
}
