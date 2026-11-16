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

package webfetch

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // fetched pictures: decoders
	"image/jpeg"
	_ "image/png"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp"
)

// MaxSide is the longest side of a picture stored: the largest size
// Telegram makes of one is 1280 px ("y", github.com's 1200 x 600).
const MaxSide = 1280

// maxPixels guards against a picture that decodes to gigabytes.
const maxPixels = 40_000_000

// PictureJPEG turns a fetched picture into the JPEG the server stores: any
// of JPEG, PNG, GIF (its first frame) or WebP, at most MaxSide on its
// longer side. A picture under 32 px on a side is refused: it is an icon.
func PictureJPEG(b []byte) ([]byte, int, int, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return nil, 0, 0, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return nil, 0, 0, fmt.Errorf("webfetch: picture of %d x %d", cfg.Width, cfg.Height)
	}
	if cfg.Width < 32 || cfg.Height < 32 {
		return nil, 0, 0, errors.New("webfetch: picture too small")
	}
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, 0, 0, err
	}
	if cfg.Width > MaxSide || cfg.Height > MaxSide {
		img = imaging.Fit(img, MaxSide, MaxSide, imaging.Lanczos)
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 87}); err != nil {
		return nil, 0, 0, err
	}
	bounds := img.Bounds()
	return out.Bytes(), bounds.Dx(), bounds.Dy(), nil
}
