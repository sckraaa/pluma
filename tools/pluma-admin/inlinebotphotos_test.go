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

package main

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/inlinebots"
)

// Every bot of the registry has a photo: a square JPEG of 640 px, as the
// service account's, and nothing but the background where a client's round
// crop and its edge fall, so the crop cuts no part of the picture.
func TestInlineBotPhotoImages(t *testing.T) {
	sage := [3]int{0x5B, 0x7A, 0x5E}
	near := func(v uint32, want int) bool {
		d := int(v>>8) - want
		// JPEG moves the flat background by a step and rings faintly at edges.
		return d >= -24 && d <= 24
	}
	for _, b := range inlinebots.All() {
		data, name, ok := inlineBotPhoto(b)
		if !ok || !strings.HasSuffix(name, ".jpg") {
			t.Fatalf("@%s: photo %q, %v", b.Username, name, ok)
		}
		img, format, err := image.Decode(bytes.NewReader(data))
		if err != nil || format != "jpeg" {
			t.Fatalf("@%s: %s, %v", b.Username, format, err)
		}
		if r := img.Bounds(); r != image.Rect(0, 0, 640, 640) {
			t.Fatalf("@%s: %v, want 640x640", b.Username, r)
		}
		var glyph int
		for y := 0; y < 640; y++ {
			for x := 0; x < 640; x++ {
				r, g, bl, _ := img.At(x, y).RGBA()
				dx, dy := x-320, y-320
				background := near(r, sage[0]) && near(g, sage[1]) && near(bl, sage[2])
				if !background {
					glyph++
				}
				// 290 px: a margin of a tenth of the radius inside the crop.
				if dx*dx+dy*dy > 290*290 && !background {
					t.Fatalf("@%s: pixel %d,%d of the picture is outside the round crop", b.Username, x, y)
				}
			}
		}
		if glyph < 640*640/10 {
			t.Errorf("@%s: the picture covers %d pixels only", b.Username, glyph)
		}
	}
	if bytes.Equal(inlineGifPhoto, inlinePicPhoto) {
		t.Error("@gif and @pic have the same photo")
	}
}

// fakeInlinePhotos are the services with accounts; it records the photo set
// on each account.
type fakeInlinePhotos struct {
	fakePhotoServices
	accounts   map[int64]*mtproto.ImmutableUser
	failRead   map[int64]error
	set        map[int64]int64
	uploadSize map[int64]int
}

func (f *fakeInlinePhotos) Account(_ context.Context, id int64) (*mtproto.ImmutableUser, error) {
	if err := f.failRead[id]; err != nil {
		return nil, err
	}
	u, ok := f.accounts[id]
	if !ok {
		return nil, mtproto.ErrUserIdInvalid
	}
	return u, nil
}

func (f *fakeInlinePhotos) SetProfilePhoto(_ context.Context, userId, photoId int64) error {
	if f.set == nil {
		f.set, f.uploadSize = map[int64]int64{}, map[int64]int{}
	}
	f.set[userId] = photoId
	size := 0
	for _, p := range f.parts {
		size += len(p)
	}
	f.uploadSize[userId] = size
	f.parts = nil
	return nil
}

func botAccount(id, photoId int64) *mtproto.ImmutableUser {
	u := &mtproto.UserData{Id: id, Bot: &mtproto.BotData{}}
	if photoId != 0 {
		u.ProfilePhoto = madePhoto(photoId)
	}
	return &mtproto.ImmutableUser{User: u}
}

func newFakeInlinePhotos() *fakeInlinePhotos {
	return &fakeInlinePhotos{
		fakePhotoServices: fakePhotoServices{photo: madePhoto(500)},
		accounts: map[int64]*mtproto.ImmutableUser{
			inlinebots.Gif.Id: botAccount(inlinebots.Gif.Id, 0),
			inlinebots.Pic.Id: botAccount(inlinebots.Pic.Id, 0),
		},
	}
}

// Bots without a photo get theirs, each its own image; a bot with a photo
// keeps it unless -replace; a bot that cannot be read or is not a bot does
// not stop the other.
func TestSetInlineBotPhotos(t *testing.T) {
	ctx := context.Background()
	var out bytes.Buffer

	f := newFakeInlinePhotos()
	if err := setInlineBotPhotos(ctx, f, false, &out); err != nil {
		t.Fatal(err)
	}
	if f.set[inlinebots.Gif.Id] != 500 || f.set[inlinebots.Pic.Id] != 500 {
		t.Fatalf("photos set: %v", f.set)
	}
	if f.uploadSize[inlinebots.Gif.Id] != len(inlineGifPhoto) || f.uploadSize[inlinebots.Pic.Id] != len(inlinePicPhoto) {
		t.Fatalf("uploaded %v, want the photo of each bot", f.uploadSize)
	}

	// Run again: both have photos now, nothing changes.
	f = newFakeInlinePhotos()
	f.accounts[inlinebots.Gif.Id] = botAccount(inlinebots.Gif.Id, 41)
	f.accounts[inlinebots.Pic.Id] = botAccount(inlinebots.Pic.Id, 42)
	out.Reset()
	if err := setInlineBotPhotos(ctx, f, false, &out); err != nil || len(f.set) != 0 {
		t.Fatalf("bots with photos: %v, set %v", err, f.set)
	}
	if !strings.Contains(out.String(), "has photo 41, kept") || !strings.Contains(out.String(), "has photo 42, kept") {
		t.Errorf("output %q", out.String())
	}
	if err := setInlineBotPhotos(ctx, f, true, &out); err != nil || len(f.set) != 2 {
		t.Fatalf("-replace: %v, set %v", err, f.set)
	}

	// @gif cannot be read, @pic is not a bot: both reported, nothing set.
	f = newFakeInlinePhotos()
	f.failRead = map[int64]error{inlinebots.Gif.Id: errors.New("biz is down")}
	f.accounts[inlinebots.Pic.Id] = &mtproto.ImmutableUser{User: &mtproto.UserData{Id: inlinebots.Pic.Id}}
	err := setInlineBotPhotos(ctx, f, false, &out)
	if err == nil || !strings.Contains(err.Error(), "@gif: biz: biz is down") || !strings.Contains(err.Error(), "@pic: account") || len(f.set) != 0 {
		t.Fatalf("failures: %v, set %v", err, f.set)
	}

	// @gif fails, @pic still gets its photo.
	f = newFakeInlinePhotos()
	delete(f.accounts, inlinebots.Gif.Id)
	err = setInlineBotPhotos(ctx, f, false, &out)
	if err == nil || !strings.Contains(err.Error(), "@gif") || f.set[inlinebots.Pic.Id] != 500 || len(f.set) != 1 {
		t.Fatalf("@gif absent: %v, set %v", err, f.set)
	}
}

// inline-bots takes the subcommand photos and the flag -replace only.
func TestInlineBotsUsage(t *testing.T) {
	for _, args := range [][]string{
		{"inline-bots"},
		{"inline-bots", "photo"},
		{"inline-bots", "photos", "extra"},
		{"inline-bots", "photos", "-force"},
	} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut, nil); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}
