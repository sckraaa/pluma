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

package contacttokens

import "testing"

// A token of this server is 24 characters of the URL alphabet; nothing
// else is looked up.
func TestValid(t *testing.T) {
	for token, want := range map[string]bool{
		"abcdefghijklmnopqrstuvwx":   true,
		"ABCDEFGH-_012345678901ab":   true,
		"":                           false,
		"abcdefghijklmnopqrstuvw":    false,
		"abcdefghijklmnopqrstuvwxy":  false,
		"abcdefghijklmnopqrstuv/x":   false,
		"abcdefghijklmnopqrstuv x":   false,
		"abcdefghijklmnopqrstuvwx\n": false,
	} {
		if got := Valid(token); got != want {
			t.Errorf("Valid(%q) = %v, want %v", token, got, want)
		}
	}
}
