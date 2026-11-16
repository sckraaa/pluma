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

package webfile

import (
	"bytes"
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

var testKey = []byte("an-example-key-of-32-bytes-or-more")

func TestSign(t *testing.T) {
	const u = "https://bot.example/a.jpg"
	h := Sign(testKey, u)
	if h <= 0 || h != Sign(testKey, u) {
		t.Fatalf("Sign is %d, not a stable positive value", h)
	}
	if Sign([]byte("another-example-key-of-32-bytes-or-more"), u) == h {
		t.Error("the signature does not depend on the key")
	}
	if Sign(testKey, u+"?") == h {
		t.Error("the signature does not depend on the address")
	}
	if !verify(testKey, u, h) || verify(testKey, u, h^1) {
		t.Error("verify")
	}
	// Results handed out keep their signatures in the inline answer cache
	// and in clients: the construction must not change between releases.
	if h != 1983285034359256977 {
		t.Errorf("Sign changed: %d", h)
	}
}

func TestKey(t *testing.T) {
	if k, err := Key(""); k != nil || err != nil {
		t.Errorf("empty: %v, %v", k, err)
	}
	if _, err := Key("short"); err == nil {
		t.Error("a short key accepted")
	}
	if k, err := Key(string(testKey)); err != nil || !bytes.Equal(k, testKey) {
		t.Errorf("%q, %v", k, err)
	}
}

// memSource keeps its file itself (Cached), as the maps of task 100 do.
type memSource struct {
	file  File
	opens atomic.Int32
}

type keyLocation string

func (l keyLocation) Key() string { return string(l) }

func (s *memSource) Cached() bool { return true }

func (s *memSource) Resolve(_ context.Context, _ Request, loc *mtproto.InputWebFileLocation) (Location, error) {
	if loc.GetZoom() < 0 {
		return nil, mtproto.ErrLocationInvalid
	}
	return keyLocation("geo"), nil
}

func (s *memSource) Open(context.Context, Request, Location) (File, error) {
	s.opens.Add(1)
	return s.file, nil
}

func geoLocation(zoom int32) *mtproto.InputWebFileLocation {
	return mtproto.MakeTLInputWebFileGeoPointLocation(&mtproto.InputWebFileLocation{Zoom: zoom}).To_InputWebFileLocation()
}

func TestGetRange(t *testing.T) {
	file := bytes.Repeat([]byte{7}, 3<<20)
	s := NewServer(nil)
	s.Register(mtproto.Predicate_inputWebFileGeoPointLocation, &memSource{file: File{Bytes: file, MimeType: "image/png", Mtime: 123}})
	ctx := context.Background()
	for _, tc := range []struct {
		name          string
		offset, limit int32
		n             int
		err           error
	}{
		{"first part", 0, 4096, 4096, nil},
		{"odd part", 5, 1000, 1000, nil},
		{"1 MiB", 1 << 20, 1 << 20, 1 << 20, nil},
		{"to the end", 3<<20 - 10, 4096, 10, nil},
		{"exactly the end", 3 << 20, 4096, 0, nil},
		{"past the end", 4 << 20, 4096, 0, nil},
		{"over 1 MiB", 0, 1<<20 + 1, 0, mtproto.ErrLimitInvalid},
		{"no limit", 0, 0, 0, mtproto.ErrLimitInvalid},
		{"negative limit", 0, -1, 0, mtproto.ErrLimitInvalid},
		{"negative offset", -4096, 4096, 0, mtproto.ErrOffsetInvalid},
	} {
		f, err := s.Get(ctx, Request{UserId: 1}, geoLocation(15), tc.offset, tc.limit)
		if !errors.Is(err, tc.err) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.err)
			continue
		}
		if err != nil {
			continue
		}
		if len(f.GetBytes()) != tc.n || f.GetSize2() != 3<<20 || f.GetMimeType() != "image/png" || f.GetMtime() != 123 ||
			f.GetFileType().GetPredicateName() != mtproto.Predicate_storage_filePng || f.GetPredicateName() != mtproto.Predicate_upload_webFile {
			t.Errorf("%s: %d bytes of %d, %s, %v", tc.name, len(f.GetBytes()), f.GetSize2(), f.GetMimeType(), f.GetFileType())
		}
	}
	if _, err := s.Get(ctx, Request{}, geoLocation(-1), 0, 4096); !errors.Is(err, mtproto.ErrLocationInvalid) {
		t.Errorf("the source's own refusal: %v", err)
	}
}

// A kind no source serves is LOCATION_INVALID, the error of the method's
// documentation: the maps until task 100 registers them, album covers.
func TestGetUnregisteredKind(t *testing.T) {
	s := NewServer(nil)
	ctx := context.Background()
	for _, loc := range []*mtproto.InputWebFileLocation{
		geoLocation(15),
		mtproto.MakeTLInputWebFileLocation(&mtproto.InputWebFileLocation{Url: "https://a.example/", AccessHash: 1}).To_InputWebFileLocation(),
		mtproto.MakeTLInputWebFileAudioAlbumThumbLocation(&mtproto.InputWebFileLocation{}).To_InputWebFileLocation(),
		nil,
	} {
		if _, err := s.Get(ctx, Request{}, loc, 0, 4096); !errors.Is(err, mtproto.ErrLocationInvalid) {
			t.Errorf("%s: %v", loc.GetPredicateName(), err)
		}
	}
}

func TestRegisterTwice(t *testing.T) {
	s := NewServer(nil)
	s.Register(mtproto.Predicate_inputWebFileGeoPointLocation, &memSource{})
	defer func() {
		if recover() == nil {
			t.Error("a kind registered twice")
		}
	}()
	s.Register(mtproto.Predicate_inputWebFileGeoPointLocation, &memSource{})
}

func TestFileType(t *testing.T) {
	for mt, want := range map[string]string{
		"image/jpeg": mtproto.Predicate_storage_fileJpeg,
		"IMAGE/PNG":  mtproto.Predicate_storage_filePng,
		"image/gif":  mtproto.Predicate_storage_fileGif,
		"image/webp": mtproto.Predicate_storage_fileWebp,
		"video/mp4":  mtproto.Predicate_storage_fileMp4,
		"audio/mpeg": mtproto.Predicate_storage_fileMp3,
		"audio/ogg":  mtproto.Predicate_storage_fileUnknown,
		"image/avif": mtproto.Predicate_storage_fileUnknown,
		"":           mtproto.Predicate_storage_fileUnknown,
	} {
		if got := FileType(mt).GetPredicateName(); got != want {
			t.Errorf("FileType(%q) = %s, want %s", mt, got, want)
		}
	}
}

func TestLabels(t *testing.T) {
	if k := kindLabel(mtproto.Predicate_inputWebFileGeoPointLocation); k != "GeoPointLocation" {
		t.Error(k)
	}
	if k := kindLabel(""); k != "other" {
		t.Error(k)
	}
	for err, want := range map[error]string{
		nil:                            "ok",
		ErrNotAvailable:                "unavailable",
		mtproto.ErrLocationInvalid:     "invalid",
		mtproto.ErrLimitInvalid:        "invalid",
		mtproto.ErrInternalServerError: "error",
	} {
		if got := resultLabel(err); got != want {
			t.Errorf("%v: %s, want %s", err, got, want)
		}
	}
}
