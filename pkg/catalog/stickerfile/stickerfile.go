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

// Package stickerfile checks the files users make stickers and custom
// emoji of (task 084, ADR 0053) by the requirements of
// core.telegram.org/stickers: a static sticker is a WEBP image with one
// side of exactly 512 pixels and the other of 512 or less; an animated one
// a TGS file (gzipped Lottie) of a 512x512 canvas, at most 3 seconds at 60
// frames a second and at most 64 KB; a video one a WEBM file in VP9 with
// one side of exactly 512 pixels, at most 3 seconds and 256 KB. A static
// or a video custom emoji is exactly 100x100 pixels. The files are read by
// their bytes, never by the type or the sizes the client names.
//
// The page also lets a static sticker be a PNG; this server takes WEBP
// alone, which is what the official clients upload (Android
// StickerMakerView.java:1431, iOS ImportStickers.swift:154, Telegram
// Desktop api_stickers_creator.cpp:532), and stores the file as it is.
package stickerfile

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"

	"github.com/teamgram/proto/mtproto"
)

// Format is what a sticker file is.
type Format int

const (
	Static   Format = iota // WEBP
	Animated               // TGS
	Video                  // WEBM
)

// The MIME types of the formats, as the clients tell stickers by them.
const (
	MimeWebp = "image/webp"
	MimeTgs  = "application/x-tgsticker"
	MimeWebm = "video/webm"
)

// The limits of core.telegram.org/stickers; the page gives a static file
// none, and MaxStaticSize is this server's own bound on it.
const (
	StickerSide   = 512
	EmojiSide     = 100
	MaxDuration   = 3.0
	MaxTgsSize    = 64 << 10
	MaxVideoSize  = 256 << 10
	MaxStaticSize = 1 << 20
	tgsFrameRate  = 60
	maxLottieSize = 4 << 20 // of the JSON a TGS file unpacks to
)

// MaxSize is the largest file of any format.
const MaxSize = MaxStaticSize

// Info is what a sticker file was found to be.
type Info struct {
	Format   Format
	W, H     int32
	Duration float64 // seconds; 0 for a static file, and for a video that does not say
}

// Mime is the MIME type of the file.
func (i *Info) Mime() string {
	switch i.Format {
	case Animated:
		return MimeTgs
	case Video:
		return MimeWebm
	default:
		return MimeWebp
	}
}

// FileName is the name the file is stored under: its extension tells dfs
// the storage type of the document.
func (i *Info) FileName() string {
	switch i.Format {
	case Animated:
		return "AnimatedSticker.tgs"
	case Video:
		return "sticker.webm"
	default:
		return "sticker.webp"
	}
}

// Inspect reads a sticker file and checks it against the requirements of
// a sticker, or of a custom emoji if emoji. The errors are those of
// stickers.createStickerSet (core.telegram.org/method/stickers.createStickerSet).
func Inspect(data []byte, emoji bool) (*Info, error) {
	switch {
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		if len(data) > MaxStaticSize {
			return nil, mtproto.ErrStickerFileInvalid
		}
		w, h, ok := webpSize(data)
		if !ok {
			return nil, mtproto.ErrStickerPngNopng
		}
		if !sidesFit(w, h, emoji) {
			return nil, mtproto.ErrStickerPngDimensions
		}
		return &Info{Format: Static, W: w, H: h}, nil
	case len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b:
		if len(data) > MaxTgsSize {
			return nil, mtproto.ErrStickerTgsNotgs
		}
		d, ok := tgsDuration(data)
		if !ok {
			return nil, mtproto.ErrStickerTgsNotgs
		}
		return &Info{Format: Animated, W: StickerSide, H: StickerSide, Duration: d}, nil
	case len(data) >= 4 && binary.BigEndian.Uint32(data) == idEBML:
		if len(data) > MaxVideoSize {
			return nil, mtproto.ErrStickerVideoBig
		}
		v, ok := webmVideo(data)
		if !ok || v.duration > MaxDuration+0.05 {
			return nil, mtproto.ErrStickerVideoNowebm
		}
		if !sidesFit(v.w, v.h, emoji) {
			return nil, mtproto.ErrStickerGifDimensions
		}
		return &Info{Format: Video, W: v.w, H: v.h, Duration: v.duration}, nil
	default:
		return nil, mtproto.ErrStickerFileInvalid
	}
}

// sidesFit tells if an image of w by h is of the size of a sticker (one
// side 512, the other no more) or of a custom emoji (100x100).
func sidesFit(w, h int32, emoji bool) bool {
	if emoji {
		return w == EmojiSide && h == EmojiSide
	}
	return w > 0 && h > 0 && w <= StickerSide && h <= StickerSide && (w == StickerSide || h == StickerSide)
}

// webpSize is the size of the canvas of a WEBP file, by its first chunk:
// VP8X (extended), VP8L (lossless) or VP8 (lossy).
func webpSize(data []byte) (w, h int32, ok bool) {
	if len(data) < 30 {
		return 0, 0, false
	}
	body := data[20:]
	switch string(data[12:16]) {
	case "VP8X":
		// flags (4 bytes), then the canvas width and height minus one, 24 bits each.
		w = int32(body[4]) | int32(body[5])<<8 | int32(body[6])<<16
		h = int32(body[7]) | int32(body[8])<<8 | int32(body[9])<<16
		return w + 1, h + 1, true
	case "VP8L":
		if body[0] != 0x2f {
			return 0, 0, false
		}
		bits := binary.LittleEndian.Uint32(body[1:5])
		return int32(bits&0x3fff) + 1, int32(bits>>14&0x3fff) + 1, true
	case "VP8 ":
		// A key frame: the frame tag (3 bytes), the start code, then the sizes.
		if body[3] != 0x9d || body[4] != 0x01 || body[5] != 0x2a {
			return 0, 0, false
		}
		return int32(binary.LittleEndian.Uint16(body[6:8]) & 0x3fff), int32(binary.LittleEndian.Uint16(body[8:10]) & 0x3fff), true
	default:
		return 0, 0, false
	}
}

// tgsDuration unpacks a TGS file and returns the length of its animation
// in seconds, if it is a Lottie animation of a sticker: a 512x512 canvas
// at 60 frames a second, no longer than 3 seconds.
func tgsDuration(data []byte) (float64, bool) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return 0, false
	}
	raw, err := io.ReadAll(io.LimitReader(zr, maxLottieSize+1))
	if err != nil || len(raw) > maxLottieSize {
		return 0, false
	}
	var lottie struct {
		W      *float64        `json:"w"`
		H      *float64        `json:"h"`
		FR     *float64        `json:"fr"`
		IP     *float64        `json:"ip"`
		OP     *float64        `json:"op"`
		Layers json.RawMessage `json:"layers"`
	}
	if json.Unmarshal(raw, &lottie) != nil || lottie.W == nil || lottie.H == nil || lottie.FR == nil || lottie.IP == nil || lottie.OP == nil || len(lottie.Layers) == 0 {
		return 0, false
	}
	if *lottie.W != StickerSide || *lottie.H != StickerSide || *lottie.FR != tgsFrameRate {
		return 0, false
	}
	d := (*lottie.OP - *lottie.IP) / *lottie.FR
	if d <= 0 || d > MaxDuration {
		return 0, false
	}
	return d, true
}

// The elements of a WEBM (Matroska) file this package reads.
const (
	idEBML          = 0x1A45DFA3
	idDocType       = 0x4282
	idSegment       = 0x18538067
	idInfo          = 0x1549A966
	idTimecodeScale = 0x2AD7B1
	idDuration      = 0x4489
	idTracks        = 0x1654AE6B
	idTrackEntry    = 0xAE
	idTrackType     = 0x83
	idCodecID       = 0x86
	idVideo         = 0xE0
	idPixelWidth    = 0xB0
	idPixelHeight   = 0xBA
	idCluster       = 0x1F43B675
)

type webm struct {
	w, h     int32
	duration float64
}

var errEBML = errors.New("stickerfile: not EBML")

// element reads the id and the size of the EBML element at the start of
// data, and returns its body and what follows it. An element of unknown
// size takes the rest of data.
func element(data []byte) (id uint32, body, rest []byte, err error) {
	if len(data) < 2 {
		return 0, nil, nil, errEBML
	}
	n := 1
	for mask := byte(0x80); mask != 0 && data[0]&mask == 0; mask >>= 1 {
		n++
	}
	if n > 4 || len(data) < n+1 {
		return 0, nil, nil, errEBML
	}
	for _, b := range data[:n] {
		id = id<<8 | uint32(b)
	}
	data = data[n:]
	n = 1
	mask := byte(0x80)
	for ; mask != 0 && data[0]&mask == 0; mask >>= 1 {
		n++
	}
	if n > 8 || len(data) < n {
		return 0, nil, nil, errEBML
	}
	size, unknown := uint64(data[0]&(mask-1)), data[0]&(mask-1) == mask-1
	for _, b := range data[1:n] {
		size = size<<8 | uint64(b)
		unknown = unknown && b == 0xff
	}
	data = data[n:]
	if unknown || size > uint64(len(data)) {
		// Unknown, or cut short by this reader: the element is the rest.
		return id, data, nil, nil
	}
	return id, data[:size], data[size:], nil
}

// children calls f for each element of data until it returns false.
func children(data []byte, f func(id uint32, body []byte) bool) error {
	for len(data) > 0 {
		id, body, rest, err := element(data)
		if err != nil {
			return err
		}
		if !f(id, body) {
			return nil
		}
		data = rest
	}
	return nil
}

func ebmlUint(b []byte) uint64 {
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return v
}

func ebmlFloat(b []byte) float64 {
	switch len(b) {
	case 4:
		return float64(math.Float32frombits(binary.BigEndian.Uint32(b)))
	case 8:
		return math.Float64frombits(binary.BigEndian.Uint64(b))
	default:
		return 0
	}
}

// webmVideo reads the header of a WEBM file: its one video track must be
// VP9 (core.telegram.org/stickers), and its size is the track's. The
// duration is 0 when the file does not say it.
func webmVideo(data []byte) (*webm, bool) {
	var (
		out      webm
		docType  string
		segment  []byte
		vp9      bool
		scale    = uint64(1000000) // nanoseconds a tick, Matroska's default
		duration float64
	)
	err := children(data, func(id uint32, body []byte) bool {
		switch id {
		case idEBML:
			_ = children(body, func(id uint32, b []byte) bool {
				if id == idDocType {
					docType = string(bytes.TrimRight(b, "\x00"))
				}
				return true
			})
		case idSegment:
			segment = body
			return false
		}
		return true
	})
	if err != nil || docType != "webm" || segment == nil {
		return nil, false
	}
	err = children(segment, func(id uint32, body []byte) bool {
		switch id {
		case idInfo:
			_ = children(body, func(id uint32, b []byte) bool {
				switch id {
				case idTimecodeScale:
					scale = ebmlUint(b)
				case idDuration:
					duration = ebmlFloat(b)
				}
				return true
			})
		case idTracks:
			_ = children(body, func(id uint32, entry []byte) bool {
				if id != idTrackEntry {
					return true
				}
				var (
					typ   uint64
					codec string
					w, h  uint64
				)
				_ = children(entry, func(id uint32, b []byte) bool {
					switch id {
					case idTrackType:
						typ = ebmlUint(b)
					case idCodecID:
						codec = string(bytes.TrimRight(b, "\x00"))
					case idVideo:
						_ = children(b, func(id uint32, v []byte) bool {
							switch id {
							case idPixelWidth:
								w = ebmlUint(v)
							case idPixelHeight:
								h = ebmlUint(v)
							}
							return true
						})
					}
					return true
				})
				if typ == 1 && out.w == 0 {
					vp9 = codec == "V_VP9"
					if w <= math.MaxInt32 && h <= math.MaxInt32 {
						out.w, out.h = int32(w), int32(h)
					}
				}
				return true
			})
		case idCluster:
			return false // the frames: the header is over
		}
		return true
	})
	if err != nil || !vp9 || out.w <= 0 || out.h <= 0 {
		return nil, false
	}
	out.duration = duration * float64(scale) / 1e9
	return &out, true
}
