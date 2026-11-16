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
	"context"
	"testing"

	"github.com/teamgram/proto/mtproto"
	dfs_client "github.com/teamgram/teamgram-server/app/service/dfs/client"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/app/service/media/media"
)

// partsDfs keeps the parts written to it.
type partsDfs struct {
	dfs_client.DfsClient
	parts []*dfs.TLDfsWriteFilePartData
}

func (d *partsDfs) DfsWriteFilePartData(_ context.Context, in *dfs.TLDfsWriteFilePartData) (*mtproto.Bool, error) {
	d.parts = append(d.parts, in)
	return mtproto.BoolTrue, nil
}

// documentMedia makes the document of what it is given.
type documentMedia struct {
	media_client.MediaClient
	got   *media.TLMediaUploadedDocumentMedia
	photo *media.TLMediaUploadPhotoFile
}

func (m *documentMedia) MediaUploadedDocumentMedia(_ context.Context, in *media.TLMediaUploadedDocumentMedia) (*mtproto.MessageMedia, error) {
	m.got = in
	return mtproto.MakeTLMessageMediaDocument(&mtproto.MessageMedia{
		Document: mtproto.MakeTLDocument(&mtproto.Document{Id: 9, MimeType: in.Media.MimeType, Attributes: in.Media.Attributes}).To_Document(),
	}).To_MessageMedia(), nil
}

func (m *documentMedia) MediaUploadPhotoFile(_ context.Context, in *media.TLMediaUploadPhotoFile) (*mtproto.Photo, error) {
	m.photo = in
	return mtproto.MakeTLPhoto(&mtproto.Photo{Id: 8}).To_Photo(), nil
}

// An animation is written to dfs in parts as the owner's and becomes a
// document the clients take for a GIF: video/mp4, animated, with its size
// and length (Android MessageObject.isNewGifDocument).
func TestUploadAnimation(t *testing.T) {
	d, m := &partsDfs{}, &documentMedia{}
	p := RPCPhotos{Dfs: d, Media: m}
	mp4 := bytes.Repeat([]byte{7}, photoPart+10)
	doc, err := p.UploadAnimation(context.Background(), 1000101, mp4, 320, 240, 2.5)
	if err != nil || doc.GetId() != 9 {
		t.Fatalf("%v, %v", doc, err)
	}
	if len(d.parts) != 2 || d.parts[0].Creator != 1000101 || d.parts[1].FilePart != 1 || d.parts[0].FileId != d.parts[1].FileId {
		t.Fatalf("parts %v", d.parts)
	}
	in := m.got
	if in.OwnerId != 1000101 || in.Media.GetPredicateName() != mtproto.Predicate_inputMediaUploadedDocument || in.Media.MimeType != "video/mp4" {
		t.Fatalf("media %v", in)
	}
	if f := in.Media.File; f.GetId_INT64() != d.parts[0].FileId || f.GetParts() != 2 || f.GetName() != "animation.mp4" || len(f.GetMd5Checksum()) != 32 {
		t.Fatalf("file %v", f)
	}
	var animated, video, name bool
	for _, a := range in.Media.Attributes {
		switch a.GetPredicateName() {
		case mtproto.Predicate_documentAttributeAnimated:
			animated = true
		case mtproto.Predicate_documentAttributeVideo:
			video = a.W == 320 && a.H == 240 && a.GetFixedDuration() == 2.5 && a.Nosound
		case mtproto.Predicate_documentAttributeFilename:
			name = a.FileName == "animation.mp4"
		}
	}
	if !animated || !video || !name {
		t.Fatalf("attributes %v", in.Media.Attributes)
	}

	for _, size := range [][2]int32{{0, 240}, {320, 0}, {MaxSide + 1, 240}, {320, MaxSide + 1}} {
		if _, err := p.UploadAnimation(context.Background(), 1000101, mp4, size[0], size[1], 1); err == nil {
			t.Errorf("an animation of %v taken", size)
		}
	}
}

// A photo of an owner is written under that owner.
func TestUploadPhotoOf(t *testing.T) {
	d, m := &partsDfs{}, &documentMedia{}
	p := RPCPhotos{Dfs: d, Media: m}
	photo, err := p.UploadPhotoOf(context.Background(), 1000102, []byte{0xff, 0xd8, 1}, "photo.jpg")
	if err != nil || photo.GetId() != 8 {
		t.Fatalf("%v, %v", photo, err)
	}
	if m.photo.OwnerId != 1000102 || d.parts[0].Creator != 1000102 || m.photo.File.GetName() != "photo.jpg" {
		t.Fatalf("owner %d, creator %d", m.photo.OwnerId, d.parts[0].Creator)
	}
}
