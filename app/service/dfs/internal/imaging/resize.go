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
	"bytes"
	"image"
	"strings"

	"github.com/teamgram/marmota/pkg/bytes2"
	"github.com/teamgram/proto/mtproto"

	"github.com/disintegration/imaging"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	JPEG imaging.Format = iota
	PNG
	GIF
	TIFF
	BMP
	WEBP
)

type resizeInfo struct {
	isWidth bool
	size    int
}

func makeResizeInfo(img image.Image) resizeInfo {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()

	if w >= h {
		return resizeInfo{
			isWidth: true,
			size:    w,
		}
	} else {
		return resizeInfo{
			isWidth: false,
			size:    h,
		}
	}
}

func getImageFormat(extName string) (int, error) {
	formats := map[string]imaging.Format{
		".jpg":  JPEG,
		".jpeg": JPEG,
		".png":  PNG,
		".tif":  TIFF,
		".tiff": TIFF,
		".bmp":  BMP,
		".gif":  GIF,
		// ".webp": WEBP,
	}

	ext := strings.ToLower(extName)
	f, ok := formats[ext]
	if !ok {
		return -1, imaging.ErrUnsupportedFormat
	}

	return int(f), nil
}

func ReSizeImage(rb []byte, extName string, isABC bool, cb func(szType string, localId int, w, h int32, b []byte) error) (err error) {
	var (
		img image.Image
	)

	img, err = Decode(bytes.NewReader(rb))
	if err != nil {
		logx.Errorf("decode r(%d) error: %v", len(rb), err)
		return
	}

	return ReSizeImageByImage(img, extName, isABC, cb)
}

func ReSizeImageByImage(img image.Image, extName string, isABC bool, cb func(szType string, localId int, w, h int32, b []byte) error) (err error) {
	var (
		f int
	)

	if isABC {
		if img.Bounds().Dx() >= mtproto.PhotoSZDSize && img.Bounds().Dy() >= mtproto.PhotoSZDSize {
			if img.Bounds().Dx() != img.Bounds().Dy() {
				img = imaging.Fill(img, mtproto.PhotoSZDSize, mtproto.PhotoSZDSize, imaging.Center, imaging.Lanczos)
			}
		} else if img.Bounds().Dx() <= mtproto.PhotoSZCSize && img.Bounds().Dy() <= mtproto.PhotoSZCSize {
			img = imaging.Fill(img, mtproto.PhotoSZCSize, mtproto.PhotoSZCSize, imaging.Center, imaging.Lanczos)
		} else {
			if img.Bounds().Dx() != img.Bounds().Dy() {
				img = imaging.Fill(img, mtproto.PhotoSZCSize, mtproto.PhotoSZCSize, imaging.Center, imaging.Lanczos)
			}
		}
	}

	imgSz := makeResizeInfo(img)

	var (
		szList    []mtproto.ReSizeInfo
		willBreak = false
		rsz       int
	)

	if isABC {
		szList = mtproto.ReSizeInfoABCList
	} else {
		szList = mtproto.ReSizeInfoPhotoList
	}

	for _, sz := range szList {
		rsz = sz.Size
		if rsz >= imgSz.size {
			rsz = imgSz.size
			willBreak = true
		}

		// TODO(@benqi): FIXME
		var dst *image.NRGBA
		if imgSz.isWidth {
			dst = imaging.Resize(img, rsz, 0, imaging.Lanczos)
		} else {
			dst = imaging.Resize(img, 0, rsz, imaging.Lanczos)
		}

		f, err = getImageFormat(extName)
		if err != nil {
			logx.Error(err.Error())
			return
		}

		o := bytes2.NewBuffer(make([]byte, 0, 512*1024))
		if f == int(imaging.JPEG) {
			// err = imaging.Encode(o, dst, imaging.JPEG, imaging.JPEGQuality(95))
			err = imaging.Encode(o, dst, imaging.JPEG)
		} else {
			err = imaging.Encode(o, dst, imaging.Format(f))
		}

		if err != nil {
			logx.Error(err.Error())
			return
		}
		err = cb(sz.Type, sz.LocalId, int32(dst.Bounds().Dx()), int32(dst.Bounds().Dy()), o.Bytes())
		if err != nil {
			return
		}

		if willBreak {
			break
		}
	}

	return
}

// StrippedSizeSide is the longer side of a photoStrippedSize, as the
// documents' (dfs.uploadDocumentFileV2).
const StrippedSizeSide = 40

// ReSizePhoto is ReSizeImage that also returns the photo's photoStrippedSize
// ("i"), from the same decoded image. The clients draw it until a real size
// is loaded; tdesktop's privacy box of the profile photo has nothing else to
// draw a public photo with and stops on an empty image without it
// (prepare_short_info_box.cpp:179-183, settings_privacy_controllers.cpp:1184,
// lib_ui image_prepare.cpp:1016 at a1e89e1f).
func ReSizePhoto(rb []byte, extName string, isABC bool, cb func(szType string, localId int, w, h int32, b []byte) error) (*mtproto.PhotoSize, error) {
	img, err := Decode(bytes.NewReader(rb))
	if err != nil {
		logx.Errorf("decode r(%d) error: %v", len(rb), err)
		return nil, err
	}
	stripped, err := StrippedSize(img, isABC)
	if err != nil {
		return nil, err
	}
	if err = ReSizeImageByImage(img, extName, isABC, cb); err != nil {
		return nil, err
	}
	return stripped, nil
}

// StrippedSize is the photoStrippedSize of img: its longer side fitted to
// StrippedSizeSide, or, for a profile photo (square), the centre square, as
// its other sizes (ReSizeImageByImage with isABC).
func StrippedSize(img image.Image, square bool) (*mtproto.PhotoSize, error) {
	var small image.Image
	switch {
	case square:
		small = Fill(img, StrippedSizeSide, StrippedSizeSide)
	case img.Bounds().Dx() >= img.Bounds().Dy():
		small = Resize(img, StrippedSizeSide, 0)
	default:
		small = Resize(img, 0, StrippedSizeSide)
	}
	var b bytes.Buffer
	if err := EncodeStripped(&b, small); err != nil {
		return nil, err
	}
	return mtproto.MakeTLPhotoStrippedSize(&mtproto.PhotoSize{
		Type:  mtproto.PhotoSZStrippedType,
		Bytes: b.Bytes(),
	}).To_PhotoSize(), nil
}
