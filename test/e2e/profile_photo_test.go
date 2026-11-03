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

//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"image"
	"image/jpeg"
	"os"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// A profile video sent alone, without a still, as Telegram Android sends
// it: the photo comes back with the video's size and the stills made from
// it, and it is the account's photo.
func TestProfileVideo(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	u := login(ctx, t, testPhones(t, 1)[0], "Alice")

	// One second of ffmpeg's testsrc2, 160x160:
	// ffmpeg -f lavfi -i testsrc2=size=160x160:rate=15:duration=1 -c:v libx264 -pix_fmt yuv420p -movflags +faststart
	video, err := os.ReadFile("testdata/profile-video.mp4")
	if err != nil {
		t.Fatal(err)
	}
	fileID := randomID()
	if _, err := u.c.Invoke(ctx, &mtproto.TLUploadSaveFilePart{
		Constructor: u.c.Constructor(mtproto.Predicate_upload_saveFilePart),
		FileId:      fileID,
		FilePart:    0,
		Bytes:       video,
	}); err != nil {
		t.Fatalf("upload.saveFilePart: %v", err)
	}
	sum := md5.Sum(video)
	o, err := u.c.Invoke(ctx, &mtproto.TLPhotosUploadProfilePhoto{
		Constructor: u.c.Constructor(mtproto.Predicate_photos_uploadProfilePhoto),
		Video: mtproto.MakeTLInputFile(&mtproto.InputFile{
			Id_INT64: fileID, Parts: 1, Name: "video.mp4", Md5Checksum: hex.EncodeToString(sum[:]),
		}).To_InputFile(),
	})
	if err != nil {
		t.Fatalf("photos.uploadProfilePhoto with a video: %v", err)
	}
	photo := o.(interface{ To_Photos_Photo() *mtproto.Photos_Photo }).To_Photos_Photo().GetPhoto()
	if photo.GetPredicateName() != mtproto.Predicate_photo || len(photo.GetVideoSizes()) == 0 || len(photo.GetSizes()) == 0 {
		t.Fatalf("the profile video: %v", photo)
	}

	o, err = u.c.Invoke(ctx, &mtproto.TLPhotosGetUserPhotos{
		Constructor: u.c.Constructor(mtproto.Predicate_photos_getUserPhotos),
		UserId:      mtproto.MakeTLInputUserSelf(nil).To_InputUser(),
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("photos.getUserPhotos: %v", err)
	}
	photos := o.(interface{ To_Photos_Photos() *mtproto.Photos_Photos }).To_Photos_Photos().GetPhotos()
	if len(photos) != 1 || photos[0].GetId() != photo.GetId() || len(photos[0].GetVideoSizes()) == 0 {
		t.Fatalf("the account's photos: %v", photos)
	}

	// Each still is a picture of the video's first frame, not an empty one.
	for _, size := range photo.GetSizes() {
		if size.GetPredicateName() != mtproto.Predicate_photoSize {
			continue
		}
		o, err := u.c.Invoke(ctx, &mtproto.TLUploadGetFile{
			Constructor: u.c.Constructor(mtproto.Predicate_upload_getFile),
			Location: mtproto.MakeTLInputPhotoFileLocation(&mtproto.InputFileLocation{
				Id: photo.GetId(), AccessHash: photo.GetAccessHash(), FileReference: photo.GetFileReference(), ThumbSize: size.GetType(),
			}).To_InputFileLocation(),
			Limit: 1 << 20,
		})
		if err != nil {
			t.Fatalf("upload.getFile of still %s: %v", size.GetType(), err)
		}
		b := o.(interface{ To_Upload_File() *mtproto.Upload_File }).To_Upload_File().GetBytes()
		img, err := jpeg.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatalf("still %s: %d bytes, not a JPEG: %v", size.GetType(), len(b), err)
		}
		if blank(img) {
			t.Errorf("still %s of %dx%d is one colour", size.GetType(), img.Bounds().Dx(), img.Bounds().Dy())
		}
	}
}

// blank tells a picture of one colour: the first frame of the test video
// has many.
func blank(img image.Image) bool {
	b := img.Bounds()
	r0, g0, b0, _ := img.At(b.Min.X, b.Min.Y).RGBA()
	for y := b.Min.Y; y < b.Max.Y; y += 4 {
		for x := b.Min.X; x < b.Max.X; x += 4 {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r != r0 || g != g0 || bl != b0 {
				return false
			}
		}
	}
	return true
}
