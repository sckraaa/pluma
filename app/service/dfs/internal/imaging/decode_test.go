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

package imaging

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// pngHeader is the start of a PNG declaring width x height: all
// image.DecodeConfig reads.
func pngHeader(width, height uint32) []byte {
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], width)
	binary.BigEndian.PutUint32(ihdr[4:], height)
	ihdr[8], ihdr[9] = 8, 6 // 8-bit RGBA
	chunk := append([]byte("IHDR"), ihdr...)
	_ = binary.Write(&b, binary.BigEndian, uint32(len(ihdr)))
	b.Write(chunk)
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return b.Bytes()
}

// TestDecodeLimit: an image declaring more than MaxDecodePixels is refused
// from its header, before anything is allocated for its pixels; an
// ordinary one decodes whole.
func TestDecodeLimit(t *testing.T) {
	if _, err := Decode(bytes.NewReader(pngHeader(20000, 20000))); err != ErrImageTooLarge {
		t.Fatalf("20000x20000: %v", err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 64, 48))
	img.Set(10, 10, color.NRGBA{R: 255, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	got, err := Decode(&b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds().Dx() != 64 || got.Bounds().Dy() != 48 {
		t.Errorf("bounds %v", got.Bounds())
	}
	if r, _, _, _ := got.At(10, 10).RGBA(); r>>8 != 255 {
		t.Error("pixels lost")
	}
}
