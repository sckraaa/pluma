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

package cdn

import (
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

func TestKey(t *testing.T) {
	loc := func(pred string, id int64, thumb string) *mtproto.InputFileLocation {
		return &mtproto.InputFileLocation{PredicateName: pred, Id: id, AccessHash: 9, ThumbSize: thumb}
	}
	for _, c := range []struct {
		name string
		loc  *mtproto.InputFileLocation
		want FileKey
		ok   bool
	}{
		{"document", loc(mtproto.Predicate_inputDocumentFileLocation, 11, ""), FileKey{KindDocument, 11, ""}, true},
		{"document thumbnail", loc(mtproto.Predicate_inputDocumentFileLocation, 11, "m"), FileKey{}, false},
		{"photo size", loc(mtproto.Predicate_inputPhotoFileLocation, 12, "y"), FileKey{KindPhoto, 12, "y"}, true},
		{"photo without a size", loc(mtproto.Predicate_inputPhotoFileLocation, 12, ""), FileKey{}, false},
		{"photo with a size too long", loc(mtproto.Predicate_inputPhotoFileLocation, 12, "123456789"), FileKey{}, false},
		{"secret chat file", loc(mtproto.Predicate_inputEncryptedFileLocation, 13, ""), FileKey{KindEncrypted, 13, ""}, true},
		{"avatar", loc(mtproto.Predicate_inputPeerPhotoFileLocation, 14, ""), FileKey{}, false},
		{"sticker set thumbnail", loc(mtproto.Predicate_inputStickerSetThumb, 15, ""), FileKey{}, false},
		{"group call stream", loc(mtproto.Predicate_inputGroupCallStream, 16, ""), FileKey{}, false},
		{"secure", loc(mtproto.Predicate_inputSecureFileLocation, 17, ""), FileKey{}, false},
		{"takeout", loc(mtproto.Predicate_inputTakeoutFileLocation, 0, ""), FileKey{}, false},
		{"legacy", loc(mtproto.Predicate_inputFileLocation, 18, ""), FileKey{}, false},
		{"no id", loc(mtproto.Predicate_inputDocumentFileLocation, 0, ""), FileKey{}, false},
		{"nil", nil, FileKey{}, false},
	} {
		got, ok := Key(c.loc)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: %+v %v, want %+v %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestWants(t *testing.T) {
	for _, c := range []struct {
		cdn, precise, want bool
	}{
		{true, false, true},
		{true, true, false},
		{false, false, false},
		{false, true, false},
	} {
		req := &mtproto.TLUploadGetFile{CdnSupported: c.cdn, Precise: c.precise}
		if got := Wants(req); got != c.want {
			t.Errorf("cdn_supported %v precise %v: %v, want %v", c.cdn, c.precise, got, c.want)
		}
	}
}

func TestMinFileSize(t *testing.T) {
	for n, ok := range map[int64]bool{1 << 20: true, 8 << 20: true, 0: false, -1 << 20: false, 1<<20 + 4096: false, 512 << 10: false} {
		if err := ValidMinFileSize(n); (err == nil) != ok {
			t.Errorf("ValidMinFileSize(%d): %v", n, err)
		}
	}
	if err := ValidMinFileSize(DefaultMinFileSize); err != nil {
		t.Error(err)
	}
	if Eligible(DefaultMinFileSize-1, DefaultMinFileSize) || !Eligible(DefaultMinFileSize, DefaultMinFileSize) {
		t.Error("the threshold is not inclusive at 1 MiB")
	}
}

func TestRetryAt(t *testing.T) {
	const now = 1_000_000
	want := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 2 * time.Hour, 2 * time.Hour, 2 * time.Hour}
	for i, d := range want {
		at, ok := RetryAt(int32(i+1), now)
		if !ok || at != now+d.Milliseconds() {
			t.Errorf("after attempt %d: %d %v, want +%v", i+1, at-now, ok, d)
		}
	}
	if _, ok := RetryAt(MaxPushAttempts, now); ok {
		t.Errorf("attempt %d is retried", MaxPushAttempts)
	}
}
