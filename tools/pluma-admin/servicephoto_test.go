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
	"crypto/md5"
	"encoding/hex"
	"errors"
	"image"
	_ "image/jpeg"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/conf"
)

type fakePhotoServices struct {
	parts       map[int32][]byte
	partOwners  map[int64]bool
	partFiles   map[int64]bool
	uploadOwner int64
	file        *mtproto.InputFile
	userId      int64
	photoId     int64
	photo       *mtproto.Photo
	failUpload  error
	failSet     error
}

func (f *fakePhotoServices) WritePart(_ context.Context, owner, fileId int64, part int32, b []byte) error {
	if f.parts == nil {
		f.parts, f.partOwners, f.partFiles = map[int32][]byte{}, map[int64]bool{}, map[int64]bool{}
	}
	f.parts[part] = append([]byte(nil), b...)
	f.partOwners[owner] = true
	f.partFiles[fileId] = true
	return nil
}

func (f *fakePhotoServices) UploadProfilePhoto(_ context.Context, owner int64, file *mtproto.InputFile) (*mtproto.Photo, error) {
	f.uploadOwner, f.file = owner, file
	if f.failUpload != nil {
		return nil, f.failUpload
	}
	return f.photo, nil
}

func (f *fakePhotoServices) SetProfilePhoto(_ context.Context, userId, photoId int64) error {
	f.userId, f.photoId = userId, photoId
	return f.failSet
}

func madePhoto(id int64) *mtproto.Photo {
	return mtproto.MakeTLPhoto(&mtproto.Photo{Id: id}).To_Photo()
}

// The built-in photo is a square JPEG, which media turns into the sizes of
// a profile photo.
func TestServicePhotoImage(t *testing.T) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(servicePhoto))
	if err != nil {
		t.Fatal(err)
	}
	if format != "jpeg" || cfg.Width != 640 || cfg.Height != 640 {
		t.Fatalf("service-photo.jpg: %s %dx%d, want jpeg 640x640", format, cfg.Width, cfg.Height)
	}
}

// The photo goes in parts of 512 KB under one owner and file, the way a
// client uploads it, and becomes the photo of 777000.
func TestSetServicePhoto(t *testing.T) {
	img := append([]byte("\xff\xd8\xff\xe0"), bytes.Repeat([]byte{7}, photoPartSize+100)...)
	f := &fakePhotoServices{photo: madePhoto(42)}
	id, err := setServicePhoto(context.Background(), f, img, "pluma.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if id != 42 || f.userId != serviceUserId || f.photoId != 42 {
		t.Fatalf("photo %d set as %d on user %d, want 42 on %d", id, f.photoId, f.userId, serviceUserId)
	}
	if len(f.parts) != 2 || len(f.parts[0]) != photoPartSize || len(f.parts[1]) != len(img)-photoPartSize {
		t.Fatalf("parts: %d (sizes %d, %d)", len(f.parts), len(f.parts[0]), len(f.parts[1]))
	}
	if !bytes.Equal(append(f.parts[0], f.parts[1]...), img) {
		t.Fatal("the parts are not the image")
	}
	if len(f.partOwners) != 1 || !f.partOwners[f.uploadOwner] || f.uploadOwner <= 0 {
		t.Fatalf("parts written as %v, the photo asked for as %d", f.partOwners, f.uploadOwner)
	}
	sum := md5.Sum(img)
	file := f.file
	if file.GetPredicateName() != mtproto.Predicate_inputFile || len(f.partFiles) != 1 || !f.partFiles[file.GetId_INT64()] ||
		file.GetParts() != 2 || file.GetName() != "pluma.jpg" || file.GetMd5Checksum() != hex.EncodeToString(sum[:]) {
		t.Fatalf("input file %+v, parts of files %v", file, f.partFiles)
	}
}

// What is not an image stops before anything is uploaded; a failure of a
// service is reported and nothing is set after it.
func TestSetServicePhotoFails(t *testing.T) {
	f := &fakePhotoServices{photo: madePhoto(1)}
	if _, err := setServicePhoto(context.Background(), f, []byte("not an image at all"), "x"); err == nil || len(f.parts) != 0 {
		t.Fatalf("a text file: err %v, %d parts uploaded", err, len(f.parts))
	}

	f = &fakePhotoServices{failUpload: errors.New("PHOTO_INVALID_DIMENSIONS")}
	if _, err := setServicePhoto(context.Background(), f, servicePhoto, "pluma.jpg"); err == nil ||
		!strings.Contains(err.Error(), "PHOTO_INVALID_DIMENSIONS") || f.userId != 0 {
		t.Fatalf("media failing: err %v, photo set on %d", err, f.userId)
	}

	f = &fakePhotoServices{photo: mtproto.MakeTLPhotoEmpty(&mtproto.Photo{Id: 5}).To_Photo()}
	if _, err := setServicePhoto(context.Background(), f, servicePhoto, "pluma.jpg"); err == nil || f.userId != 0 {
		t.Fatalf("photoEmpty from media: err %v, photo set on %d", err, f.userId)
	}

	f = &fakePhotoServices{photo: madePhoto(9), failSet: errors.New("USER_ID_INVALID")}
	if _, err := setServicePhoto(context.Background(), f, servicePhoto, "pluma.jpg"); err == nil || !strings.Contains(err.Error(), "USER_ID_INVALID") {
		t.Fatalf("biz failing: err %v", err)
	}
}

// service-photo finds the services through the bff config, and takes one
// image at most.
func TestServicePhotoConfig(t *testing.T) {
	t.Setenv("MYSQL_PASSWORD", "pw")
	t.Setenv("REDIS_PASSWORD", "rpw")
	t.Setenv("LOGIN_CODE_HMAC_KEY", testHmacKey)
	t.Setenv("PUSH_SECRET_ENCRYPTION_KEY", testHmacKey)
	for _, f := range []string{"../../teamgramd/etc/bff.yaml", "../../teamgramd/etc2/bff.yaml"} {
		var c photoConfig
		if err := conf.Load(f, &c); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if c.BizServiceClient.Etcd.Key != "service.biz_service" || c.MediaClient.Etcd.Key != "service.media" ||
			c.DfsClient.Etcd.Key != "service.dfs" {
			t.Fatalf("%s: clients %q %q %q", f, c.BizServiceClient.Etcd.Key, c.MediaClient.Etcd.Key, c.DfsClient.Etcd.Key)
		}
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"service-photo", "a.jpg", "b.jpg"}, &out, &errOut, nil); code != 2 {
		t.Fatalf("two images: exit %d, want 2", code)
	}
}
