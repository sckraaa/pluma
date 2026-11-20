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

package sfu

import "testing"

func TestValidRoute(t *testing.T) {
	for _, ok := range []string{"direct", "ru", "ru-2", "a", "abcdefghijklmnop"} {
		if !ValidRoute(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	for _, bad := range []string{"", "RU", "2ru", "-ru", "ru_2", "ru 2", "abcdefghijklmnopq", "рус"} {
		if ValidRoute(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}
