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

package pushdevices

import "testing"

// The token of Android's push session is the session id as tgnet writes
// it: an unsigned decimal of 64 bits.
func TestPushSessionId(t *testing.T) {
	for token, want := range map[string]int64{
		"1":                    1,
		"9223372036854775807":  9223372036854775807,
		"9223372036854775808":  -9223372036854775808, // the high bit set
		"18446744073709551615": -1,
	} {
		if id, ok := PushSessionId(token); !ok || id != want {
			t.Errorf("%q: %d %v, want %d", token, id, ok, want)
		}
	}
	for _, token := range []string{"", "0", "-1", "+1", "18446744073709551616", "0x10", "1 ", "abc"} {
		if id, ok := PushSessionId(token); ok {
			t.Errorf("%q: %d, want no id", token, id)
		}
	}
}
