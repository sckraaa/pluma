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
	"errors"
	"testing"
)

// TestMergeImportMapping: a resumed import adds authors and changes none.
func TestMergeImportMapping(t *testing.T) {
	cases := []struct {
		old, next, want string
		err             error
	}{
		{"", "", `{}`, nil},
		{"", `{"user1":5}`, `{"user1":5}`, nil},
		{`{"user1":5}`, "", `{"user1":5}`, nil},
		{`{"user1":5}`, `{"user1":5,"user2":0}`, `{"user1":5,"user2":0}`, nil},
		{`{"user1":5}`, `{"user1":6}`, "", ErrImportMappingChanged},
		{`{"user1":0}`, `{"user1":6}`, "", ErrImportMappingChanged},
		{`{"user1":5}`, `{"user1":0}`, "", ErrImportMappingChanged},
		{"", `{"user1":-1}`, "", ErrImportInvalid},
		{"", `[1]`, "", ErrImportInvalid},
	}
	for _, c := range cases {
		got, err := MergeImportMapping(c.old, c.next)
		if !errors.Is(err, c.err) || got != c.want {
			t.Errorf("%q + %q: %q, %v; want %q, %v", c.old, c.next, got, err, c.want, c.err)
		}
	}
	if _, err := MergeImportMapping("{", ""); err == nil {
		t.Error("a broken stored mapping was merged")
	}
}
