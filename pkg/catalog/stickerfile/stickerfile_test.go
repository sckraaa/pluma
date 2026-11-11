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
	"errors"
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// A sticker file is told by its bytes and held to the requirements of
// core.telegram.org/stickers: the sides of a static and of a video one,
// the canvas, the rate and the length of an animated one, the codec of a
// video one, and the 100x100 of a custom emoji.
func TestInspect(t *testing.T) {
	big := make([]byte, MaxVideoSize+1)
	copy(big, SampleWebm(512, 512, "V_VP9", 2))
	for _, tc := range []struct {
		name   string
		data   []byte
		emoji  bool
		format Format
		w, h   int32
		err    error
	}{
		{name: "a square static sticker", data: SampleWebp(512, 512), format: Static, w: 512, h: 512},
		{name: "a static sticker with one side of 512", data: SampleWebp(512, 300), format: Static, w: 512, h: 300},
		{name: "a static sticker with no side of 512", data: SampleWebp(400, 300), err: mtproto.ErrStickerPngDimensions},
		{name: "a static sticker too large", data: SampleWebp(1024, 512), err: mtproto.ErrStickerPngDimensions},
		{name: "a static custom emoji", data: SampleWebp(100, 100), emoji: true, format: Static, w: 100, h: 100},
		{name: "a sticker for a custom emoji", data: SampleWebp(512, 512), emoji: true, err: mtproto.ErrStickerPngDimensions},
		{name: "a WEBP without an image", data: []byte("RIFF\x10\x00\x00\x00WEBPJUNK\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"), err: mtproto.ErrStickerPngNopng},
		{name: "an animated sticker", data: SampleTgs(512, 60, 180), format: Animated, w: 512, h: 512},
		{name: "an animation of another canvas", data: SampleTgs(256, 60, 60), err: mtproto.ErrStickerTgsNotgs},
		{name: "an animation of another rate", data: SampleTgs(512, 30, 60), err: mtproto.ErrStickerTgsNotgs},
		{name: "an animation longer than 3 seconds", data: SampleTgs(512, 60, 181), err: mtproto.ErrStickerTgsNotgs},
		{name: "a gzip of no animation", data: SampleTgs(512, 60, 60)[:20], err: mtproto.ErrStickerTgsNotgs},
		{name: "a video sticker", data: SampleWebm(512, 384, "V_VP9", 2.5), format: Video, w: 512, h: 384},
		{name: "a video of another codec", data: SampleWebm(512, 512, "V_VP8", 2), err: mtproto.ErrStickerVideoNowebm},
		{name: "a video longer than 3 seconds", data: SampleWebm(512, 512, "V_VP9", 4), err: mtproto.ErrStickerVideoNowebm},
		{name: "a video of another size", data: SampleWebm(640, 480, "V_VP9", 2), err: mtproto.ErrStickerGifDimensions},
		{name: "a video custom emoji", data: SampleWebm(100, 100, "V_VP9", 1), emoji: true, format: Video, w: 100, h: 100},
		{name: "a video too heavy", data: big, err: mtproto.ErrStickerVideoBig},
		{name: "a PNG", data: []byte("\x89PNG\r\n\x1a\n0000000000000000000000"), err: mtproto.ErrStickerFileInvalid},
		{name: "nothing", data: nil, err: mtproto.ErrStickerFileInvalid},
	} {
		info, err := Inspect(tc.data, tc.emoji)
		if !errors.Is(err, tc.err) {
			t.Errorf("%s: error %v, want %v", tc.name, err, tc.err)
			continue
		}
		if err == nil && (info.Format != tc.format || info.W != tc.w || info.H != tc.h) {
			t.Errorf("%s: format %d %dx%d, want %d %dx%d", tc.name, info.Format, info.W, info.H, tc.format, tc.w, tc.h)
		}
	}
	if info, _ := Inspect(SampleWebm(512, 512, "V_VP9", 2.5), false); info == nil || info.Duration < 2.49 || info.Duration > 2.51 || info.Mime() != MimeWebm {
		t.Errorf("a video of 2.5 seconds: %+v", info)
	}
}

// The emoji of a sticker come one after another; each is taken whole, with
// its skin tone, its joined parts or its flag, and once.
func TestSplitEmoji(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"😺", []string{"😺"}},
		{"😺🐶😺", []string{"😺", "🐶"}},
		{"👍🏽❤️", []string{"👍🏽", "❤️"}},
		{"👨‍👩‍👧🇷🇺", []string{"👨‍👩‍👧", "🇷🇺"}},
		{"#️⃣1️⃣", []string{"#️⃣", "1️⃣"}},
		{"", nil},
		{"cat", nil},
		{"😺 cat", nil},
		{"7", nil},
	} {
		got, ok := SplitEmoji(tc.in)
		if ok != (tc.want != nil) || !slices.Equal(got, tc.want) {
			t.Errorf("SplitEmoji(%q) = %q, %v; want %q", tc.in, got, ok, tc.want)
		}
	}
	many := ""
	for i := 0; i <= MaxEmoji; i++ {
		many += string(rune(0x1f600 + i))
	}
	if _, ok := SplitEmoji(many); ok {
		t.Errorf("%d emoji taken, the most is %d", MaxEmoji+1, MaxEmoji)
	}
	if got := SplitKeywords(" cat, Kitty ,,cat,meow "); !slices.Equal(got, []string{"cat", "Kitty", "meow"}) {
		t.Errorf("SplitKeywords: %q", got)
	}
}
