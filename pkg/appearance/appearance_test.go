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

package appearance

import (
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func TestValidThemeSlug(t *testing.T) {
	for slug, want := range map[string]bool{
		"myTheme":               true,
		"abc_1":                 true,
		"x1234":                 true,
		"abcd":                  false, // too short
		strings.Repeat("a", 65): false, // too long
		"_theme":                false,
		"theme_":                false,
		"1theme":                false,
		"the-me":                false,
		"слово":                 false,
		strings.Repeat("a", 64): true,
	} {
		if got := ValidThemeSlug(slug); got != want {
			t.Errorf("ValidThemeSlug(%q) = %v, want %v", slug, got, want)
		}
	}
}

func TestNewSlugIsValid(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		s, err := NewSlug()
		if err != nil {
			t.Fatal(err)
		}
		if !ValidThemeSlug(s) || seen[s] {
			t.Fatalf("slug %q (seen before: %v)", s, seen[s])
		}
		seen[s] = true
	}
}

// CalcHash as Android's MediaDataController.calcHash folds it: 0 starts it and
// the last value only adds in.
func TestCalcHash(t *testing.T) {
	if got := CalcHash(0, 5); got != 5 {
		t.Fatalf("CalcHash(0, 5) = %d", got)
	}
	if CalcHash(CalcHash(0, 1), 2) == CalcHash(CalcHash(0, 2), 1) {
		t.Fatalf("the hash must depend on the order")
	}
}

func TestPeerWallpaperUpdate(t *testing.T) {
	w := mtproto.MakeTLWallPaperNoFile(&mtproto.WallPaper{Id: 3}).To_WallPaper()
	u := PeerWallpaperUpdate(mtproto.MakePeerUser(9), w, true)
	if u.GetPredicateName() != mtproto.Predicate_updatePeerWallpaper || u.GetPeer_PEER().GetUserId() != 9 || u.GetWallpaper().GetId() != 3 || !u.GetWallpaperOverridden() {
		t.Fatalf("update %v", u)
	}
	if u = PeerWallpaperUpdate(mtproto.MakePeerUser(9), nil, false); u.GetWallpaper() != nil || u.GetWallpaperOverridden() {
		t.Fatalf("a cleared wallpaper: %v", u)
	}
}
