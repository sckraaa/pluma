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

package chat

import "testing"

// TestGetInviteHashByLink: invite links of the server's host and of
// Telegram's (handed out before task 026), in every form clients send.
func TestGetInviteHashByLink(t *testing.T) {
	const hash = "Abcdefghij0123456789"
	for _, link := range []string{
		"https://pluma.chat/+" + hash,
		"https://pluma.chat/joinchat/" + hash,
		"pluma.chat/+" + hash,
		"http://pluma.chat/+" + hash,
		"https://t.me/+" + hash,
		"https://t.me/joinchat/" + hash,
		"https://telegram.me/joinchat/" + hash,
		"t.me/+" + hash,
		hash,
	} {
		if got := GetInviteHashByLink(link); got != hash {
			t.Errorf("%s: %q", link, got)
		}
	}
	if got := GetInviteHashByLink("https://example.com/+" + hash); got == hash {
		t.Error("a link of another site taken as an invite")
	}
}
