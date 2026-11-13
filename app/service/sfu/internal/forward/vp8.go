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

// Package forward moves video from senders to receivers: one simulcast layer
// per receiver and sender, rewritten into one continuous stream, with
// retransmissions from a packet cache, and the estimate of each receiver's
// channel that picks the layers.
package forward

import "errors"

var errVP8Short = errors.New("forward: short VP8 payload")

// vp8Descriptor is the part of the VP8 payload descriptor (RFC 7741 §4.2)
// the SFU reads and rewrites.
type vp8Descriptor struct {
	// start: the packet begins a partition (S) of partition 0 (PID).
	start bool
	// keyframe: the packet begins a key frame (P bit of the payload
	// header is 0, RFC 7741 §4.3).
	keyframe bool

	pictureID     int // -1 when absent
	pictureIDLen  int // 1 (7 bits) or 2 (15 bits)
	pictureIDOff  int
	tl0PicIdx     int // -1 when absent
	tl0PicIdxOff  int
	descriptorLen int
}

func parseVP8(p []byte) (vp8Descriptor, error) {
	d := vp8Descriptor{pictureID: -1, tl0PicIdx: -1}
	if len(p) < 1 {
		return d, errVP8Short
	}
	x := p[0]&0x80 != 0
	d.start = p[0]&0x10 != 0 && p[0]&0x07 == 0
	i := 1
	if x {
		if len(p) < 2 {
			return d, errVP8Short
		}
		hasI := p[1]&0x80 != 0
		hasL := p[1]&0x40 != 0
		hasTK := p[1]&0x30 != 0
		i = 2
		if hasI {
			if len(p) < i+1 {
				return d, errVP8Short
			}
			d.pictureIDOff = i
			if p[i]&0x80 != 0 {
				if len(p) < i+2 {
					return d, errVP8Short
				}
				d.pictureID = int(p[i]&0x7f)<<8 | int(p[i+1])
				d.pictureIDLen = 2
				i += 2
			} else {
				d.pictureID = int(p[i] & 0x7f)
				d.pictureIDLen = 1
				i++
			}
		}
		if hasL {
			if len(p) < i+1 {
				return d, errVP8Short
			}
			d.tl0PicIdxOff = i
			d.tl0PicIdx = int(p[i])
			i++
		}
		if hasTK {
			i++
		}
	}
	if len(p) < i {
		return d, errVP8Short
	}
	d.descriptorLen = i
	if d.start && len(p) > i {
		d.keyframe = p[i]&0x01 == 0
	}
	return d, nil
}

// setPictureID writes a picture id of the same width into a payload.
func (d vp8Descriptor) setPictureID(p []byte, id int) {
	switch d.pictureIDLen {
	case 1:
		p[d.pictureIDOff] = byte(id & 0x7f)
	case 2:
		p[d.pictureIDOff] = 0x80 | byte((id>>8)&0x7f)
		p[d.pictureIDOff+1] = byte(id)
	}
}

func (d vp8Descriptor) setTL0PicIdx(p []byte, v int) {
	if d.tl0PicIdx >= 0 {
		p[d.tl0PicIdxOff] = byte(v)
	}
}

func (d vp8Descriptor) pictureIDMod() int {
	if d.pictureIDLen == 1 {
		return 1 << 7
	}
	return 1 << 15
}

// isKeyframeStart reports whether a video payload starts a key frame. Only
// VP8 is parsed: the SFU offers VP8 alone (tgcallsjson.BuildResponse) and
// native clients prefer it (GroupInstanceCustomImpl.cpp:3110-3140).
func isKeyframeStart(payloadType uint8, payload []byte) bool {
	if payloadType != payloadTypeVP8 {
		return false
	}
	d, err := parseVP8(payload)
	return err == nil && d.keyframe
}

const payloadTypeVP8 = 100
