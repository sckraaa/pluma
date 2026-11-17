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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strings"

	"github.com/teamgram/teamgram-server/pkg/langpack"

	"github.com/teamgram/proto/mtproto"
)

// Bounds of inputWebFileGeoPointLocation (ADR 0063, p. 2). The documented
// ranges (https://core.telegram.org/constructor/inputWebFileGeoPointLocation:
// w, h 16-1024, zoom 13-20, scale 1-3) are clamped to, not refused; scale
// goes up to 4 because tdesktop asks for it: scale = 1 + cScale·DPR/200 with
// cScale up to 300 and an integer DPR (Telegram/SourceFiles/data/data_location.cpp:96).
const (
	MinSide  = 16
	MaxSide  = 1024
	MinZoom  = 13
	MaxZoom  = 20
	MinScale = 1
	MaxScale = 4
	// MaxPixels bounds each side of the image after scale; it is the
	// maxSize of the maps service (tileserver-gl answers 400 above it).
	MaxPixels = 2048
	// MaxLat is the edge of the Web Mercator square; tileserver-gl refuses a
	// center beyond 85.06 (src/serve_rendered.js:571-578, v5.7.0).
	MaxLat = 85.0511
)

// coordScale rounds coordinates to 6 decimals (~0.1 m), the precision of the
// Android client's own cache key "maps_%.6f_%.6f_..." (messenger/WebFile.java:44).
const coordScale = 1e6

// Params is a normalized map request: center, size in points, zoom in the
// 256-pixel-tile sense (the world is 256·2^Zoom pixels wide) and scale. The
// image is W·Scale × H·Scale pixels.
type Params struct {
	Lat, Long float64
	W, H      int32
	Zoom      int32
	Scale     int32
}

// PixelWidth and PixelHeight are the size of the image.
func (p Params) PixelWidth() int  { return int(p.W) * int(p.Scale) }
func (p Params) PixelHeight() int { return int(p.H) * int(p.Scale) }

// Normalize checks an inputWebFileGeoPointLocation and brings it into the
// ranges the maps service renders (ADR 0063, p. 2). It returns
// mtproto.ErrLocationInvalid for another kind of location, an empty or
// non-numeric point, a longitude beyond ±180 or a side ≤ 0. access_hash is not
// checked: Android sends 0 for a business address
// (ui/Business/LocationActivity.java:322), and Pluma's points all carry 0.
func Normalize(in *mtproto.InputWebFileLocation) (Params, error) {
	if in == nil || in.GetPredicateName() != mtproto.Predicate_inputWebFileGeoPointLocation {
		return Params{}, mtproto.ErrLocationInvalid
	}
	pt := in.GetGeoPoint()
	if pt == nil || pt.GetPredicateName() != mtproto.Predicate_inputGeoPoint {
		return Params{}, mtproto.ErrLocationInvalid
	}
	lat, long := pt.GetLat(), pt.GetLong()
	if !finite(lat) || !finite(long) || math.Abs(long) > 180 {
		return Params{}, mtproto.ErrLocationInvalid
	}
	if in.GetW() <= 0 || in.GetH() <= 0 {
		return Params{}, mtproto.ErrLocationInvalid
	}
	p := Params{
		Lat:   round6(clamp(lat, -MaxLat, MaxLat)),
		Long:  round6(long),
		W:     clamp(in.GetW(), MinSide, MaxSide),
		H:     clamp(in.GetH(), MinSide, MaxSide),
		Zoom:  clamp(in.GetZoom(), MinZoom, MaxZoom),
		Scale: clamp(in.GetScale(), MinScale, MaxScale),
	}
	// The largest scale that keeps both sides within MaxPixels; the client
	// stretches the image to its size.
	for p.Scale > MinScale && (p.PixelWidth() > MaxPixels || p.PixelHeight() > MaxPixels) {
		p.Scale--
	}
	return p, nil
}

// Key is the cache key of the map of p drawn in style: the hex SHA-256 of a
// fixed text form of every field, so that equal requests share one image.
func Key(style string, p Params) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%s|%.6f|%.6f|%d|%d|%d|%d", style, p.Lat, p.Long, p.W, p.H, p.Zoom, p.Scale))
	return hex.EncodeToString(sum[:])
}

// russianLabelLanguages are the languages whose sessions see Russian labels
// when there is no style of their own: the CIS languages Protomaps has no
// labels for or that read Cyrillic place names more easily than Latin ones
// (ADR 0063, p. 5).
var russianLabelLanguages = map[string]bool{
	"be": true, "kk": true, "ky": true, "tg": true, "uz": true, "hy": true, "az": true,
}

// StyleFor picks the style of a session's language among styles (ADR 0063,
// p. 5): the language's own style (by the full code, then by its base
// language), Russian for the languages of russianLabelLanguages, otherwise
// English, otherwise the first style. styles must not be empty.
func StyleFor(langCode string, styles []string) string {
	has := func(s string) bool {
		for _, v := range styles {
			if v == s {
				return true
			}
		}
		return false
	}
	code := langpack.CanonicalLangCode(langCode)
	base, _, _ := strings.Cut(code, "-")
	switch {
	case code != "" && has(code):
		return code
	case base != "" && has(base):
		return base
	case russianLabelLanguages[base] && has("ru"):
		return "ru"
	case has("en"):
		return "en"
	default:
		return styles[0]
	}
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func round6(v float64) float64 {
	v = math.Round(v*coordScale) / coordScale
	if v == 0 {
		// -0 would print as "-0.000000": a different key and URL for the
		// same point.
		return 0
	}
	return v
}

func clamp[T int32 | float64](v, lo, hi T) T {
	return min(max(v, lo), hi)
}
