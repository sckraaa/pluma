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

package staticmap

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func geoLocation(lat, long float64, w, h, zoom, scale int32) *mtproto.InputWebFileLocation {
	return mtproto.MakeTLInputWebFileGeoPointLocation(&mtproto.InputWebFileLocation{
		GeoPoint: mtproto.MakeTLInputGeoPoint(&mtproto.InputGeoPoint{Lat: lat, Long: long}).To_InputGeoPoint(),
		W:        w,
		H:        h,
		Zoom:     zoom,
		Scale:    scale,
	}).To_InputWebFileLocation()
}

func TestNormalize(t *testing.T) {
	ok := func(lat, long float64, w, h, zoom, scale int32) Params {
		return Params{Lat: lat, Long: long, W: w, H: h, Zoom: zoom, Scale: scale}
	}
	tests := []struct {
		name string
		in   *mtproto.InputWebFileLocation
		want Params
		bad  bool
	}{
		{"android bubble", geoLocation(55.755814, 37.617635, 260, 160, 15, 2), ok(55.755814, 37.617635, 260, 160, 15, 2), false},
		{"rounds to 6 decimals", geoLocation(55.7558144, 37.6176355, 100, 100, 15, 1), ok(55.755814, 37.617636, 100, 100, 15, 1), false},
		{"negative zero is zero", geoLocation(-0.0000001, -0.0000004, 100, 100, 15, 1), ok(0, 0, 100, 100, 15, 1), false},
		{"lat 85.06 clamped", geoLocation(85.06, 0, 100, 100, 15, 1), ok(MaxLat, 0, 100, 100, 15, 1), false},
		{"lat -85.06 clamped", geoLocation(-85.06, 0, 100, 100, 15, 1), ok(-MaxLat, 0, 100, 100, 15, 1), false},
		{"lat 90 clamped", geoLocation(90, 0, 100, 100, 15, 1), ok(MaxLat, 0, 100, 100, 15, 1), false},
		{"lat -90 clamped", geoLocation(-90, 0, 100, 100, 15, 1), ok(-MaxLat, 0, 100, 100, 15, 1), false},
		{"long 180", geoLocation(0, 180, 100, 100, 15, 1), ok(0, 180, 100, 100, 15, 1), false},
		{"long -180", geoLocation(0, -180, 100, 100, 15, 1), ok(0, -180, 100, 100, 15, 1), false},
		{"long 180.000001", geoLocation(0, 180.000001, 100, 100, 15, 1), Params{}, true},
		{"long -180.000001", geoLocation(0, -180.000001, 100, 100, 15, 1), Params{}, true},
		{"long 200", geoLocation(0, 200, 100, 100, 15, 1), Params{}, true},
		{"lat NaN", geoLocation(math.NaN(), 0, 100, 100, 15, 1), Params{}, true},
		{"long NaN", geoLocation(0, math.NaN(), 100, 100, 15, 1), Params{}, true},
		{"lat Inf", geoLocation(math.Inf(1), 0, 100, 100, 15, 1), Params{}, true},
		{"w 0", geoLocation(0, 0, 0, 100, 15, 1), Params{}, true},
		{"h 0", geoLocation(0, 0, 100, 0, 15, 1), Params{}, true},
		{"w negative", geoLocation(0, 0, -5, 100, 15, 1), Params{}, true},
		{"w 15", geoLocation(0, 0, 15, 100, 15, 1), ok(0, 0, 16, 100, 15, 1), false},
		{"w 16", geoLocation(0, 0, 16, 100, 15, 1), ok(0, 0, 16, 100, 15, 1), false},
		{"h 1024", geoLocation(0, 0, 100, 1024, 15, 1), ok(0, 0, 100, 1024, 15, 1), false},
		{"h 1025", geoLocation(0, 0, 100, 1025, 15, 1), ok(0, 0, 100, 1024, 15, 1), false},
		{"zoom 12", geoLocation(0, 0, 100, 100, 12, 1), ok(0, 0, 100, 100, 13, 1), false},
		{"zoom 13", geoLocation(0, 0, 100, 100, 13, 1), ok(0, 0, 100, 100, 13, 1), false},
		{"zoom 20", geoLocation(0, 0, 100, 100, 20, 1), ok(0, 0, 100, 100, 20, 1), false},
		{"zoom 21", geoLocation(0, 0, 100, 100, 21, 1), ok(0, 0, 100, 100, 20, 1), false},
		{"zoom 0", geoLocation(0, 0, 100, 100, 0, 1), ok(0, 0, 100, 100, 13, 1), false},
		{"scale 0", geoLocation(0, 0, 100, 100, 15, 0), ok(0, 0, 100, 100, 15, 1), false},
		{"scale 1", geoLocation(0, 0, 100, 100, 15, 1), ok(0, 0, 100, 100, 15, 1), false},
		{"scale 3", geoLocation(0, 0, 100, 100, 15, 3), ok(0, 0, 100, 100, 15, 3), false},
		{"scale 4", geoLocation(0, 0, 100, 100, 15, 4), ok(0, 0, 100, 100, 15, 4), false},
		{"scale 5", geoLocation(0, 0, 100, 100, 15, 5), ok(0, 0, 100, 100, 15, 4), false},
		{"1024x1024@4 to @2", geoLocation(0, 0, 1024, 1024, 15, 4), ok(0, 0, 1024, 1024, 15, 2), false},
		{"700x100@4 to @2", geoLocation(0, 0, 700, 100, 15, 4), ok(0, 0, 700, 100, 15, 2), false},
		{"683x100@3 to @2", geoLocation(0, 0, 683, 100, 15, 3), ok(0, 0, 683, 100, 15, 2), false},
		{"512x512@4 kept", geoLocation(0, 0, 512, 512, 15, 4), ok(0, 0, 512, 512, 15, 4), false},
		// Web K writes the integers into an Int32Array, so a fractional
		// devicePixelRatio and height arrive truncated (lib/mtproto/tl_utils.ts:114-120):
		// width 420, height 420·195/277 = 295.66 → 295, dpr 2.625 → 2.
		{"web k after truncation", geoLocation(48.858370, 2.294481, 420, 295, 16, 2), ok(48.85837, 2.294481, 420, 295, 16, 2), false},
		// tdesktop at 300% and DPR 2: scale 4, zoom 16, locationSize / 4.
		{"tdesktop 300%", geoLocation(59.93863, 30.31413, 80, 50, 16, 4), ok(59.93863, 30.31413, 80, 50, 16, 4), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Normalize(tt.in)
			if tt.bad {
				if !errors.Is(err, mtproto.ErrLocationInvalid) {
					t.Fatalf("Normalize = %+v, %v; want LOCATION_INVALID", got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Normalize: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Normalize = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestNormalizeRefusesOtherLocations(t *testing.T) {
	empty := geoLocation(0, 0, 100, 100, 15, 1)
	empty.GeoPoint = mtproto.MakeTLInputGeoPointEmpty(nil).To_InputGeoPoint()
	noPoint := geoLocation(0, 0, 100, 100, 15, 1)
	noPoint.GeoPoint = nil
	for name, in := range map[string]*mtproto.InputWebFileLocation{
		"nil":          nil,
		"empty point":  empty,
		"no point":     noPoint,
		"url location": mtproto.MakeTLInputWebFileLocation(&mtproto.InputWebFileLocation{Url: "https://example.com/a.png"}).To_InputWebFileLocation(),
	} {
		if _, err := Normalize(in); !errors.Is(err, mtproto.ErrLocationInvalid) {
			t.Errorf("%s: Normalize error = %v, want LOCATION_INVALID", name, err)
		}
	}
}

func TestKey(t *testing.T) {
	base := Params{Lat: 55.755814, Long: 37.617635, W: 277, H: 195, Zoom: 16, Scale: 2}
	k := Key("ru", base)
	if k != Key("ru", base) {
		t.Fatal("Key is not stable")
	}
	if len(k) != 64 || strings.Trim(k, "0123456789abcdef") != "" {
		t.Fatalf("Key = %q, want 64 hex digits", k)
	}
	variants := map[string]func(*Params){
		"lat":   func(p *Params) { p.Lat += 0.000001 },
		"long":  func(p *Params) { p.Long += 0.000001 },
		"w":     func(p *Params) { p.W++ },
		"h":     func(p *Params) { p.H++ },
		"zoom":  func(p *Params) { p.Zoom++ },
		"scale": func(p *Params) { p.Scale++ },
	}
	seen := map[string]string{k: "base"}
	for name, change := range variants {
		p := base
		change(&p)
		v := Key("ru", p)
		if other, dup := seen[v]; dup {
			t.Errorf("Key of changed %s equals the key of %s", name, other)
		}
		seen[v] = name
	}
	if Key("en", base) == k {
		t.Error("Key does not depend on the style")
	}
	// Swapped lat and long must not collide.
	swapped := base
	swapped.Lat, swapped.Long = base.Long, base.Lat
	if Key("ru", swapped) == k {
		t.Error("Key of swapped coordinates collides")
	}
}

func TestStyleFor(t *testing.T) {
	ruEn := []string{"ru", "en"}
	ruEnUk := []string{"ru", "en", "uk"}
	tests := []struct {
		lang   string
		styles []string
		want   string
	}{
		{"ru", ruEn, "ru"},
		{"ru-RU", ruEn, "ru"},
		{"RU_ru", ruEn, "ru"},
		{"en", ruEn, "en"},
		{"en-US", ruEn, "en"},
		{"be", ruEn, "ru"},
		{"kk", ruEn, "ru"},
		{"uz", ruEn, "ru"},
		{"uk", ruEn, "en"},
		{"uk", ruEnUk, "uk"},
		{"kk", ruEnUk, "ru"},
		{"", ruEn, "en"},
		{"de", ruEn, "en"},
		{"zz-unknown", ruEn, "en"},
		{"be", []string{"en", "de"}, "en"},
		{"fr", []string{"de", "it"}, "de"},
	}
	for _, tt := range tests {
		if got := StyleFor(tt.lang, tt.styles); got != tt.want {
			t.Errorf("StyleFor(%q, %v) = %q, want %q", tt.lang, tt.styles, got, tt.want)
		}
	}
}

func TestConfig(t *testing.T) {
	if (Config{}).Enabled() {
		t.Fatal("empty config is enabled")
	}
	if err := (Config{Styles: "!!"}).Validate(); err != nil {
		t.Fatalf("disabled config with junk is refused: %v", err)
	}
	s, err := Config{URL: "http://maps:8080/"}.parse()
	if err != nil {
		t.Fatal(err)
	}
	if s.url != "http://maps:8080" || strings.Join(s.styles, ",") != "ru,en" || s.timeout != DefaultTimeout ||
		s.concurrency != DefaultConcurrency || s.cacheTTL != DefaultCacheTTL || s.perUserPerMinute != DefaultPerUserPerMinute {
		t.Fatalf("defaults = %+v", s)
	}
	s, err = Config{URL: "https://maps.internal", Styles: " ru , en,uk ", Timeout: "3s", Concurrency: "2", CacheTTL: "1h", PerUserPerMinute: "10"}.parse()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(s.styles, ",") != "ru,en,uk" || s.timeout.String() != "3s" || s.concurrency != 2 || s.cacheTTL.String() != "1h0m0s" || s.perUserPerMinute != 10 {
		t.Fatalf("parsed = %+v", s)
	}
	for name, c := range map[string]Config{
		"ftp":           {URL: "ftp://maps"},
		"path":          {URL: "http://maps:8080/styles"},
		"query":         {URL: "http://maps:8080?a=b"},
		"credentials":   {URL: "http://u:p@maps:8080"},
		"no host":       {URL: "http://"},
		"bad style":     {URL: "http://maps", Styles: "ru,EN"},
		"short style":   {URL: "http://maps", Styles: "r"},
		"long style":    {URL: "http://maps", Styles: "abcdefghijk"},
		"slash style":   {URL: "http://maps", Styles: "ru/../x"},
		"dup style":     {URL: "http://maps", Styles: "ru,ru"},
		"empty style":   {URL: "http://maps", Styles: "ru,,en"},
		"many styles":   {URL: "http://maps", Styles: "aa,bb,cc,dd,ee,ff,gg,hh,ii"},
		"timeout":       {URL: "http://maps", Timeout: "10"},
		"short timeout": {URL: "http://maps", Timeout: "10ms"},
		"concurrency":   {URL: "http://maps", Concurrency: "0"},
		"ttl":           {URL: "http://maps", CacheTTL: "1s"},
		"per user":      {URL: "http://maps", PerUserPerMinute: "x"},
	} {
		if err := c.Validate(); err == nil {
			t.Errorf("%s: Validate accepted %+v", name, c)
		}
	}
}
