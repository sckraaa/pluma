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

package user

import "testing"

// TestReservedUsername: the paths of the link host are no usernames, in any
// case; ordinary names are.
func TestReservedUsername(t *testing.T) {
	for _, name := range []string{"android", "Android", "IPHONE", "joinchat", "addstickers", "proxy", "share"} {
		if !ReservedUsername(name) {
			t.Errorf("%s is free", name)
		}
	}
	for _, name := range []string{"androids", "durov", "pluma_news", "iphone12"} {
		if ReservedUsername(name) {
			t.Errorf("%s is reserved", name)
		}
	}
}
