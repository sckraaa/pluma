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

package stickerfile

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"math"
)

// The smallest files Inspect takes for a sticker of each format, for the
// tests of this package and of those that make stickers: headers that say
// what the file is, without a picture in them.

// SampleWebp is the header of a lossless WEBP image of w by h.
func SampleWebp(w, h int) []byte {
	bits := uint32(w-1) | uint32(h-1)<<14
	body := []byte{0x2f, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(body[1:], bits)
	out := []byte("RIFF\x00\x00\x00\x00WEBPVP8L\x00\x00\x00\x00")
	binary.LittleEndian.PutUint32(out[4:], uint32(4+8+len(body)))
	binary.LittleEndian.PutUint32(out[16:], uint32(len(body)))
	return append(out, body...)
}

// SampleTgs is a TGS file: a gzipped Lottie animation of a side by side
// canvas, frames frames at rate a second.
func SampleTgs(side int, rate, frames float64) []byte {
	var b bytes.Buffer
	z := gzip.NewWriter(&b)
	fmt.Fprintf(z, `{"tgs":1,"v":"5.5.2","fr":%g,"ip":0,"op":%g,"w":%d,"h":%d,"layers":[{"ty":4}]}`, rate, frames, side, side)
	_ = z.Close()
	return b.Bytes()
}

func ebml(id uint32, body []byte) []byte {
	var out []byte
	for shift := 24; shift >= 0; shift -= 8 {
		if b := byte(id >> shift); b != 0 || len(out) > 0 {
			out = append(out, b)
		}
	}
	// The size in eight bytes: the marker of the length, then the size.
	out = append(out, 0x01)
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(body)))
	out = append(out, size[1:]...)
	return append(out, body...)
}

// SampleWebm is the header of a WEBM file with one video track of w by h in
// codec ("V_VP9"), seconds long.
func SampleWebm(w, h int, codec string, seconds float64) []byte {
	be := func(v uint16) []byte { return []byte{byte(v >> 8), byte(v)} }
	var duration [8]byte
	binary.BigEndian.PutUint64(duration[:], math.Float64bits(seconds*1000)) // in ticks of a millisecond
	video := append(ebml(idPixelWidth, be(uint16(w))), ebml(idPixelHeight, be(uint16(h)))...)
	track := append(ebml(idTrackType, []byte{1}), ebml(idCodecID, []byte(codec))...)
	track = append(track, ebml(idVideo, video)...)
	info := append(ebml(idTimecodeScale, []byte{0x0f, 0x42, 0x40}), ebml(idDuration, duration[:])...)
	segment := append(ebml(idInfo, info), ebml(idTracks, ebml(idTrackEntry, track))...)
	segment = append(segment, ebml(idCluster, []byte{0})...)
	return append(ebml(idEBML, ebml(idDocType, []byte("webm"))), ebml(idSegment, segment)...)
}
