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
// Author: teamgramio (teamgram.io@gmail.com)
//

package imaging

import (
	"image"
	"io"

	"github.com/teamgram/teamgram-server/app/service/dfs/internal/imaging/jpeg"
)

// StrippedQuality is the JPEG quality of stripped thumbnails (photoStrippedSize).
// A stripped thumbnail carries only the scan data: the clients decode it with
// the fixed JPEG header of Telegram's format (JpegHeader here; Android
// Bitmaps.header, ImageLoader.getStrippedPhotoBitmap), whose quantization
// tables are the standard ones at quality 20. The scan must be quantized with
// the same tables, or the clients decode the coefficients with the wrong
// scale and the thumbnail comes out with exaggerated contrast.
const StrippedQuality = 20

// EncodeStripped encodes img (at most 255x255, normally fitted to 40x40) as
// the bytes of a photoStrippedSize.
func EncodeStripped(w io.Writer, img image.Image) error {
	quality := StrippedQuality
	var (
		rgba *image.RGBA
		err  error
	)

	if nrgba, ok := img.(*image.NRGBA); ok {
		if nrgba.Opaque() {
			rgba = &image.RGBA{
				Pix:    nrgba.Pix,
				Stride: nrgba.Stride,
				Rect:   nrgba.Rect,
			}
		}
	}

	if rgba != nil {
		err = jpeg.EncodeStripped(w, rgba, &jpeg.Options{Quality: quality})
	} else {
		err = jpeg.EncodeStripped(w, img, &jpeg.Options{Quality: quality})
	}

	return err
}
