// Copyright 2022 Teamgram Authors
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
//

package imaging

import (
	"bytes"
	"image"
	"image/color"
	stdjpeg "image/jpeg"
	"os"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// inflateStripped rebuilds a JPEG from photoStrippedSize bytes the way the
// clients do (Android ImageLoader.getStrippedPhotoBitmap): the fixed header,
// the scan after the 3-byte prefix, the footer, and the size patched into the
// SOF0 marker.
func inflateStripped(t *testing.T, stripped []byte) image.Image {
	t.Helper()
	if len(stripped) < 3 || stripped[0] != 0x01 {
		t.Fatalf("stripped prefix = % x, want 01 h w", stripped[:min(3, len(stripped))])
	}
	data := make([]byte, 0, len(JpegHeader)+len(stripped)-3+len(JpegFooter))
	data = append(data, JpegHeader...)
	data = append(data, stripped[3:]...)
	data = append(data, JpegFooter...)
	data[164] = stripped[1]
	data[166] = stripped[2]
	img, err := stdjpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode inflated stripped thumbnail: %v", err)
	}
	return img
}

// gradient is an opaque 40x30 test image with smooth colour ramps.
func gradient() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 40, 30))
	for y := 0; y < 30; y++ {
		for x := 0; x < 40; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(40 + x*4), G: uint8(60 + y*5), B: uint8(200 - x*3), A: 0xff})
		}
	}
	return img
}

// TestStrippedMatchesClientHeader checks that a stripped thumbnail decodes,
// with the header the clients use, close to its source: the scan is
// quantized with the header's tables.
func TestStrippedMatchesClientHeader(t *testing.T) {
	src := gradient()

	var b bytes.Buffer
	if err := EncodeStripped(&b, src); err != nil {
		t.Fatalf("EncodeStripped: %v", err)
	}
	if b.Bytes()[1] != 30 || b.Bytes()[2] != 40 {
		t.Fatalf("stripped size = %dx%d, want 40x30", b.Bytes()[2], b.Bytes()[1])
	}

	got := inflateStripped(t, b.Bytes())
	if got.Bounds().Dx() != 40 || got.Bounds().Dy() != 30 {
		t.Fatalf("decoded size = %v, want 40x30", got.Bounds())
	}

	var sum, n float64
	for y := 0; y < 30; y++ {
		for x := 0; x < 40; x++ {
			r1, g1, b1, _ := src.At(x, y).RGBA()
			r2, g2, b2, _ := got.At(x, y).RGBA()
			for _, d := range []int{int(r1>>8) - int(r2>>8), int(g1>>8) - int(g2>>8), int(b1>>8) - int(b2>>8)} {
				if d < 0 {
					d = -d
				}
				sum += float64(d)
				n++
			}
		}
	}
	// Matching tables give an error of a few levels; tables of another
	// quality scale every coefficient and move the colours by tens of levels.
	if mae := sum / n; mae > 6 {
		t.Errorf("mean absolute error of the decoded thumbnail = %.1f levels, want <= 6", mae)
	}
}

// TestReSizePhotoStripped: every photo gets a photoStrippedSize "i" that the
// clients can inflate; a profile photo's is the centre square, as its other
// sizes, another photo's keeps its proportions with the longer side 40.
func TestReSizePhotoStripped(t *testing.T) {
	data, err := os.ReadFile("testdata/video-001.png")
	if err != nil {
		t.Fatal(err)
	}
	for _, square := range []bool{true, false} {
		sizes := 0
		stripped, err := ReSizePhoto(data, ".png", square, func(string, int, int32, int32, []byte) error {
			sizes++
			return nil
		})
		if err != nil || stripped == nil || sizes == 0 {
			t.Fatalf("square %v: stripped %v, %d sizes, %v", square, stripped, sizes, err)
		}
		if stripped.GetPredicateName() != mtproto.Predicate_photoStrippedSize || stripped.GetType() != mtproto.PhotoSZStrippedType {
			t.Fatalf("square %v: %s %q", square, stripped.GetPredicateName(), stripped.GetType())
		}
		b := inflateStripped(t, stripped.GetBytes()).Bounds()
		switch {
		case square && (b.Dx() != StrippedSizeSide || b.Dy() != StrippedSizeSide):
			t.Errorf("profile photo: stripped %dx%d, want %dx%d", b.Dx(), b.Dy(), StrippedSizeSide, StrippedSizeSide)
		case !square && max(b.Dx(), b.Dy()) != StrippedSizeSide:
			t.Errorf("photo: stripped %dx%d, want the longer side %d", b.Dx(), b.Dy(), StrippedSizeSide)
		}
	}
	if _, err := ReSizePhoto([]byte("not an image"), ".jpg", true, func(string, int, int32, int32, []byte) error { return nil }); err == nil {
		t.Error("a file that is not an image gave no error")
	}
}
