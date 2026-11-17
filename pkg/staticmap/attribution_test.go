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
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math/rand/v2"
	"testing"
)

var mapGreen = color.NRGBA{R: 0x60, G: 0x80, B: 0x50, A: 0xff}

// changedBounds returns the bounds of the pixels of a PNG that are not
// mapGreen.
func changedBounds(t *testing.T, b []byte) (image.Rectangle, image.Image) {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	var r image.Rectangle
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if color.NRGBAModel.Convert(img.At(x, y)) != mapGreen {
				r = r.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return r, img
}

func renderedMap(t *testing.T, p Params) []byte {
	t.Helper()
	s, _ := testService(t, Config{}, solidRenderer{mapGreen})
	b, err := s.GetParams(context.Background(), 1, "en", p)
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes
}

// solidRenderer draws a map of one color.
type solidRenderer struct{ c color.Color }

func (r solidRenderer) Render(_ context.Context, _ string, p Params) ([]byte, error) {
	return encodeSolid(p.PixelWidth(), p.PixelHeight(), r.c)
}

func TestAttributionBottomLeft(t *testing.T) {
	p := Params{W: 100, H: 100, Zoom: 15, Scale: 1}
	changed, img := changedBounds(t, renderedMap(t, p))
	if changed.Empty() {
		t.Fatal("no credit on a 100×100 map")
	}
	w, h := p.PixelWidth(), p.PixelHeight()
	// Inset from the bottom-left corner, in the left part of the map.
	if changed.Min.X != attributionInset || changed.Max.Y != h-attributionInset {
		t.Fatalf("credit at %v, want its corner at (%d, %d)", changed, attributionInset, h-attributionInset)
	}
	if changed.Max.X > w-10 || changed.Min.Y < h/2 {
		t.Fatalf("credit at %v reaches into the rest of the map", changed)
	}
	// The plate lightens the map, the text darkens it.
	var lighter, darker bool
	for y := changed.Min.Y; y < changed.Max.Y; y++ {
		for x := changed.Min.X; x < changed.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			lighter = lighter || c.G > mapGreen.G+50
			darker = darker || c.G < mapGreen.G-20
		}
	}
	if !lighter || !darker {
		t.Fatalf("plate %v, text %v", lighter, darker)
	}
}

func TestAttributionSkippedOnThumbnails(t *testing.T) {
	for _, p := range []Params{
		{W: 99, H: 200, Zoom: 15, Scale: 1},
		{W: 200, H: 99, Zoom: 15, Scale: 2},
		{W: 36, H: 36, Zoom: 13, Scale: 2},
	} {
		raw, err := encodeSolid(p.PixelWidth(), p.PixelHeight(), mapGreen)
		if err != nil {
			t.Fatal(err)
		}
		got := renderedMap(t, p)
		if !bytes.Equal(got, raw) {
			t.Errorf("%dx%d: the map was changed", p.W, p.H)
		}
	}
}

func TestAttributionScales(t *testing.T) {
	one, _ := changedBounds(t, renderedMap(t, Params{W: 277, H: 195, Zoom: 15, Scale: 1}))
	two, _ := changedBounds(t, renderedMap(t, Params{W: 277, H: 195, Zoom: 15, Scale: 2}))
	if one.Empty() || two.Empty() {
		t.Fatal("no credit")
	}
	near := func(a, b int) bool { return a-b <= 4 && b-a <= 4 }
	if !near(two.Dx(), 2*one.Dx()) || !near(two.Dy(), 2*one.Dy()) {
		t.Fatalf("plate at scale 1 %v, at scale 2 %v: not twice as large", one.Size(), two.Size())
	}
	if two.Min.X != 2*attributionInset || two.Max.Y != 390-2*attributionInset {
		t.Fatalf("plate at scale 2 at %v", two)
	}
}

func TestAttributionOnPalettedMap(t *testing.T) {
	pal := image.NewPaletted(image.Rect(0, 0, 200, 200), color.Palette{mapGreen, color.White})
	var raw bytes.Buffer
	if err := png.Encode(&raw, pal); err != nil {
		t.Fatal(err)
	}
	p := Params{W: 200, H: 200, Zoom: 15, Scale: 1}
	img, err := decodeMap(raw.Bytes(), p)
	if err != nil {
		t.Fatal(err)
	}
	out, err := attribute(img, 1)
	if err != nil {
		t.Fatal(err)
	}
	if changed, _ := changedBounds(t, out); changed.Empty() {
		t.Fatal("no credit on a paletted map")
	}
}

// mapLike is a w×h image with the structure of a rendered map: a light
// background, areas, roads and noise from labels.
func mapLike(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.NRGBA{R: 0xf2, G: 0xef, B: 0xe9, A: 0xff}), image.Point{}, draw.Src)
	rnd := rand.New(rand.NewPCG(1, 2))
	palette := []color.NRGBA{
		{R: 0xc8, G: 0xe6, B: 0xc0, A: 0xff}, {R: 0xaa, G: 0xd3, B: 0xdf, A: 0xff},
		{R: 0xd9, G: 0xd0, B: 0xc9, A: 0xff}, {R: 0xff, G: 0xff, B: 0xff, A: 0xff},
		{R: 0xf7, G: 0xfa, B: 0xbf, A: 0xff},
	}
	for i := 0; i < 400; i++ {
		x, y := rnd.IntN(w), rnd.IntN(h)
		r := image.Rect(x, y, x+rnd.IntN(w/6)+2, y+rnd.IntN(h/6)+2)
		draw.Draw(img, r, image.NewUniform(palette[rnd.IntN(len(palette))]), image.Point{}, draw.Src)
	}
	for i := 0; i < w*h/40; i++ {
		img.Set(rnd.IntN(w), rnd.IntN(h), color.NRGBA{R: uint8(rnd.IntN(256)), G: uint8(rnd.IntN(256)), B: uint8(rnd.IntN(256)), A: 0xff})
	}
	return img
}

// BenchmarkAttribute measures the credit on the largest bubble the clients
// ask for in practice (Web K 420×296 at devicePixelRatio 3), with the
// encoder in use and the others for comparison.
func BenchmarkAttribute(b *testing.B) {
	img := mapLike(1260, 888)
	src := image.NewNRGBA(img.Bounds())
	inUse := pngEncoder
	defer func() { pngEncoder = inUse }()
	for _, level := range []png.CompressionLevel{png.DefaultCompression, png.BestSpeed, png.BestCompression} {
		b.Run(fmt.Sprintf("level=%d", level), func(b *testing.B) {
			pngEncoder = png.Encoder{CompressionLevel: level}
			b.ReportAllocs()
			var size int
			for b.Loop() {
				copy(src.Pix, img.Pix)
				out, err := attribute(src, 3)
				if err != nil {
					b.Fatal(err)
				}
				size = len(out)
			}
			b.ReportMetric(float64(size), "bytes")
		})
	}
}

// BenchmarkDecodeAttribute is the whole work on a rendered map: decoding the
// renderer's PNG, the credit and the encoder in use.
func BenchmarkDecodeAttribute(b *testing.B) {
	var raw bytes.Buffer
	if err := png.Encode(&raw, mapLike(1260, 888)); err != nil {
		b.Fatal(err)
	}
	p := Params{W: 420, H: 296, Zoom: 16, Scale: 3}
	b.ReportAllocs()
	for b.Loop() {
		img, err := decodeMap(raw.Bytes(), p)
		if err != nil {
			b.Fatal(err)
		}
		if _, err = attribute(img, 3); err != nil {
			b.Fatal(err)
		}
	}
}
