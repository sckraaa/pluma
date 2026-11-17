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

package dao

import (
	"encoding/base64"
	"testing"
)

// A cursor comes back as it went out; a string the server did not make is
// refused, never read as some other place in the list.
func TestForwardCursor(t *testing.T) {
	for _, c := range []ForwardCursor{
		{Date: 1793577600, PeerId: 77, Id: 1},
		{Date: 1793577600, Story: true, PeerId: 1 << 40, Id: 1<<31 - 1},
		{Date: 0, PeerId: 1, Id: 1},
	} {
		got, ok := ParseForwardCursor(c.String())
		if !ok || got != c {
			t.Errorf("%+v came back as %+v, %t", c, got, ok)
		}
	}
	raw := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	for _, s := range []string{
		"", "!", "MTox", raw("1:0:2"), raw("1:0:2:3:4"), raw("1:2:3:4"), raw("-1:0:2:3"),
		raw("1:0:0:3"), raw("1:0:2:0"), raw("1:0:2:2147483648"), raw("a:0:2:3"), raw("1:0:2:3") + "=",
	} {
		if c, ok := ParseForwardCursor(s); ok {
			t.Errorf("%q taken as %+v", s, c)
		}
	}
}
