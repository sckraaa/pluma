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

package business

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// The clients take the limits from help.getAppConfig: the app config must
// give the numbers the server checks.
func TestLimitsAreTheAppConfigs(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "teamgramd", "bin", "app_config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	for key, limit := range map[string]int{
		"quick_replies_limit":            QuickRepliesLimit,
		"quick_reply_messages_limit":     QuickReplyMessagesLimit,
		"business_chat_links_limit":      ChatLinksLimit,
		"intro_title_length_limit":       IntroTitleLengthLimit,
		"intro_description_length_limit": IntroDescriptionLengthLimit,
		"message_length_limit_default":   ChatLinkMessageLengthMax,
	} {
		if v, ok := config[key].(float64); !ok || v != float64(limit) {
			t.Errorf("%s of app_config.json %v, the server's limit %d", key, config[key], limit)
		}
	}
}

func TestSlug(t *testing.T) {
	seen := make(map[string]bool)
	chars := make(map[rune]bool)
	for range 2000 {
		s, err := NewSlug()
		if err != nil {
			t.Fatal(err)
		}
		if !ValidSlug(s) {
			t.Fatalf("NewSlug made %q, which ValidSlug refuses", s)
		}
		if seen[s] {
			t.Fatalf("%q twice", s)
		}
		seen[s] = true
		for _, r := range s {
			chars[r] = true
		}
	}
	// 32000 characters drawn: every one of the 62 comes up.
	if len(chars) != len(slugAlphabet) {
		t.Errorf("%d different characters, want %d", len(chars), len(slugAlphabet))
	}
	for _, s := range []string{"", "abcdefghijklmno", "abcdefghijklmnopq", "abcdefghijklmno-", "abcdefghijklmnoж", "abcdefghijklmno "} {
		if ValidSlug(s) {
			t.Errorf("%q taken for a slug", s)
		}
	}
	if !ValidSlug("AZaz09AZaz09AZaz") {
		t.Error("a slug of the alphabet refused")
	}
}

func TestAddress(t *testing.T) {
	if !ValidAddress(strings.Repeat("ж", AddressLengthMax)) {
		t.Error("96 characters of two bytes refused")
	}
	if ValidAddress(strings.Repeat("a", AddressLengthMax+1)) {
		t.Error("97 characters taken")
	}
	if ValidAddress("street \xff") {
		t.Error("an address that is not UTF-8 taken")
	}
}

func TestPoint(t *testing.T) {
	for _, c := range []struct {
		lat, long float64
		want      bool
	}{
		{52.52, 13.405, true},
		{-90, 180, true},
		{90.0001, 0, false},
		{0, -180.5, false},
		{math.NaN(), 0, false},
		{0, math.Inf(1), false},
	} {
		if got := ValidPoint(c.lat, c.long); got != c.want {
			t.Errorf("%v, %v: %v, want %v", c.lat, c.long, got, c.want)
		}
	}
}

func TestUTF16Length(t *testing.T) {
	for s, want := range map[string]int{"": 0, "abc": 3, "жук": 3, "\U0001F600": 2, "a\U0001F600b": 4} {
		if got := UTF16Length(s); got != want {
			t.Errorf("%q: %d, want %d", s, got, want)
		}
	}
}

func TestProfileToTL(t *testing.T) {
	plain := (&Location{Address: "Main St 1"}).ToTL()
	if plain.GetPredicateName() != mtproto.Predicate_businessLocation || plain.GetAddress() != "Main St 1" || plain.GetGeoPoint() != nil {
		t.Errorf("a location without a point: %v", plain)
	}
	geo := (&Location{Address: "Main St 1", HasGeo: true, Lat: 52.5, Long: 13.4}).ToTL()
	if geo.GetGeoPoint().GetPredicateName() != mtproto.Predicate_geoPoint || geo.GetGeoPoint().GetLat() != 52.5 || geo.GetGeoPoint().GetLong() != 13.4 {
		t.Errorf("a location with a point: %v", geo)
	}
	doc := mtproto.MakeTLDocument(&mtproto.Document{Id: 5}).To_Document()
	intro := (&Intro{Title: "Hi", Description: "Ask", StickerID: 5}).ToTL(doc)
	if intro.GetPredicateName() != mtproto.Predicate_businessIntro || intro.GetTitle() != "Hi" || intro.GetDescription() != "Ask" || intro.GetSticker().GetId() != 5 {
		t.Errorf("an intro: %v", intro)
	}
}
