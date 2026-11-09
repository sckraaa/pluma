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
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/moderation"
)

func TestCheckProtocolLibraryNames(t *testing.T) {
	p := func(versions ...string) *mtproto.PhoneCallProtocol {
		return &mtproto.PhoneCallProtocol{UdpP2P: true, MinLayer: 65, MaxLayer: 92, LibraryVersions: versions}
	}
	if err := checkProtocol(p("9.0.0", "13.0.0")); err != nil {
		t.Fatalf("real versions: %v", err)
	}
	if err := checkProtocol(p(strings.Repeat("9", maxVersionLen+1))); err == nil {
		t.Fatal("an oversized library name passed")
	}
	if err := checkProtocol(p("")); err == nil {
		t.Fatal("an empty library name passed")
	}
}

func TestClip(t *testing.T) {
	// "я" is two bytes: the limit falls inside the last one.
	got := clip(strings.Repeat("я", 3), 5)
	if got != "яя" || !utf8.ValidString(got) {
		t.Fatalf("clip inside a character: %q", got)
	}
	if got := clip("ok\xff", 10); got != "ok" {
		t.Fatalf("bytes that are no text: %q", got)
	}
	if got := clip("short", 10); got != "short" {
		t.Fatalf("short text: %q", got)
	}
}

func TestOverCallLimit(t *testing.T) {
	for _, tc := range []struct {
		total, peer int64
		want        bool
	}{
		{0, 0, false},
		{maxCallsPerWindow - 1, maxCallsToOnePeer - 1, false},
		{maxCallsPerWindow, 0, true},
		{5, maxCallsToOnePeer, true},
	} {
		if got := overCallLimit(tc.total, tc.peer); got != tc.want {
			t.Errorf("%d calls, %d to one peer: %v", tc.total, tc.peer, got)
		}
	}
}

func TestRestrictionRefuses(t *testing.T) {
	limited := &moderation.Restriction{Kind: moderation.RestrictLimited}
	banned := &moderation.Restriction{Kind: moderation.RestrictBanned}
	for _, tc := range []struct {
		name    string
		r       *moderation.Restriction
		contact bool
		want    bool
	}{
		{"not restricted", nil, false, false},
		{"limited, a stranger", limited, false, true},
		{"limited, a contact", limited, true, false},
		{"banned, a contact", banned, true, true},
	} {
		if got := restrictionRefuses(tc.r, tc.contact); got != tc.want {
			t.Errorf("%s: %v", tc.name, got)
		}
	}
}
