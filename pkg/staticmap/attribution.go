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
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// attributionText is the credit the data licence asks for
// (https://osmfoundation.org/wiki/Licence/Attribution_Guidelines: "©
// OpenStreetMap" is enough, in any corner). It is a proper name and a legal
// notice, not a text of the interface, so it is not localized (ADR 0063,
// p. 6).
const attributionText = "© OpenStreetMap"

// minAttributionSide is the smallest side, in points, of a map that gets the
// credit; smaller ones are thumbnails, which the guidelines let go without
// it (Android polls ask for 36×36, inline results for 72×72).
const minAttributionSide = 100

// Layout of the credit, in points (multiplied by the scale): a plate
// inset from the bottom-left corner — the bottom-right one is where the
// clients draw the time of the message — with the text inside.
const (
	attributionFontSize = 10
	attributionInset    = 3
	attributionPadX     = 2
	attributionPadY     = 1
)

var (
	attributionPlate = color.NRGBA{R: 255, G: 255, B: 255, A: 204} // rgba(255,255,255,0.8)
	attributionInk   = color.NRGBA{A: 204}                         // rgba(0,0,0,0.8)
)

// pngEncoder writes the maps with the credit. DefaultCompression:
// BenchmarkAttribute on a map-like 1260×888 image (Web K 420×296 at DPR 3)
// took 32 ms for 298 KB with it and 353 ms for 277 KB with BestCompression
// (Ryzen 5 5600, 2026-11-16); 7% fewer bytes are not worth ten times the
// CPU on every render.
var pngEncoder = png.Encoder{CompressionLevel: png.DefaultCompression}

var (
	goRegular     *opentype.Font
	goRegularErr  error
	goRegularOnce sync.Once
)

func attributionFace(scale int32) (font.Face, error) {
	goRegularOnce.Do(func() { goRegular, goRegularErr = opentype.Parse(goregular.TTF) })
	if goRegularErr != nil {
		return nil, goRegularErr
	}
	// A face keeps buffers and is not safe for concurrent use: one per call.
	return opentype.NewFace(goRegular, &opentype.FaceOptions{
		Size:    float64(attributionFontSize * scale),
		DPI:     72,               // Size is then in pixels
		Hinting: font.HintingNone, // advances scale exactly with the scale
	})
}

// needsAttribution reports whether the map of p gets the credit.
func needsAttribution(p Params) bool {
	return min(p.W, p.H) >= minAttributionSide
}

// decodeMap decodes a rendered map fully (the CRCs and the compressed data,
// not only the header) and checks its size. The header is checked first, so
// that a PNG claiming a huge size is refused before its pixels are allocated.
func decodeMap(b []byte, p Params) (image.Image, error) {
	if err := checkPNGHeader(b, p); err != nil {
		return nil, err
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("%w: not a PNG", ErrBadImage)
	}
	if s := img.Bounds().Size(); s.X != p.PixelWidth() || s.Y != p.PixelHeight() {
		return nil, fmt.Errorf("%w: not of the requested size", ErrBadImage)
	}
	return img, nil
}

// attribute draws the credit on img and encodes it as a PNG.
func attribute(img image.Image, scale int32) ([]byte, error) {
	face, err := attributionFace(scale)
	if err != nil {
		return nil, err
	}
	defer face.Close()

	var dst draw.Image
	switch m := img.(type) {
	case *image.NRGBA:
		dst = m
	case *image.RGBA:
		dst = m
	default:
		// Paletted and gray images would lose the blend: draw on a copy.
		c := image.NewNRGBA(img.Bounds())
		draw.Draw(c, c.Bounds(), img, img.Bounds().Min, draw.Src)
		dst = c
	}

	s := int(scale)
	m := face.Metrics()
	textW := font.MeasureString(face, attributionText).Ceil()
	ascent, descent := m.Ascent.Ceil(), m.Descent.Ceil()
	b := dst.Bounds()
	plate := image.Rect(0, 0, textW+2*attributionPadX*s, ascent+descent+2*attributionPadY*s).
		Add(image.Pt(b.Min.X+attributionInset*s, b.Max.Y-attributionInset*s-(ascent+descent+2*attributionPadY*s)))
	draw.Draw(dst, plate, image.NewUniform(attributionPlate), image.Point{}, draw.Over)

	d := font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(attributionInk),
		Face: face,
		Dot:  fixed.P(plate.Min.X+attributionPadX*s, plate.Min.Y+attributionPadY*s+ascent),
	}
	d.DrawString(attributionText)

	var out bytes.Buffer
	if err = pngEncoder.Encode(&out, dst); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
