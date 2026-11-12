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

package autodownload

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// A preset of any layer reads the same: the sizes of the older
// constructors are ints; negative values are "never", a bitrate stays a
// percentage; written back it has the sizes in both forms.
func TestFromTLAndBack(t *testing.T) {
	now := FromTL(&mtproto.AutoDownloadSettings{
		Disabled: true, AudioPreloadNext: true, StoriesPreload: true,
		PhotoSizeMax: 100, VideoSizeMax_INT64: 5 << 32, FileSizeMax_INT64: 300, VideoUploadMaxbitrate: 70,
		SmallQueueActiveOperationsMax: 4, LargeQueueActiveOperationsMax: 2,
	})
	if !now.Disabled || !now.AudioPreloadNext || now.VideoPreloadLarge || !now.StoriesPreload || now.PhotoSizeMax != 100 ||
		now.VideoSizeMax != 5<<32 || now.FileSizeMax != 300 || now.VideoUploadMaxbitrate != 70 ||
		now.SmallQueueActiveOperationsMax != 4 || now.LargeQueueActiveOperationsMax != 2 {
		t.Errorf("the current form read as %+v", now)
	}
	old := FromTL(&mtproto.AutoDownloadSettings{PhotoSizeMax: 100, VideoSizeMax_INT32: 200, FileSizeMax_INT32: 300})
	if old.VideoSizeMax != 200 || old.FileSizeMax != 300 {
		t.Errorf("the form with ints read as %+v", old)
	}
	bad := FromTL(&mtproto.AutoDownloadSettings{PhotoSizeMax: -1, VideoSizeMax_INT64: -2, FileSizeMax_INT64: -3, VideoUploadMaxbitrate: 250, SmallQueueActiveOperationsMax: -1})
	if bad.PhotoSizeMax != 0 || bad.VideoSizeMax != 0 || bad.FileSizeMax != 0 || bad.VideoUploadMaxbitrate != 100 || bad.SmallQueueActiveOperationsMax != 0 {
		t.Errorf("values out of range read as %+v", bad)
	}
	if neg := FromTL(&mtproto.AutoDownloadSettings{VideoUploadMaxbitrate: -5}); neg.VideoUploadMaxbitrate != 0 {
		t.Errorf("a negative bitrate read as %d", neg.VideoUploadMaxbitrate)
	}

	tl := now.TL()
	if tl.GetPredicateName() != mtproto.Predicate_autoDownloadSettings || tl.GetVideoSizeMax_INT64() != 5<<32 || tl.GetVideoSizeMax_INT32() != 1<<31-1 ||
		tl.GetFileSizeMax_INT64() != 300 || tl.GetFileSizeMax_INT32() != 300 {
		t.Errorf("written as %v", tl)
	}
	if again := FromTL(tl); again != now {
		t.Errorf("read back as %+v, want %+v", again, now)
	}
}

// The presets everybody chooses from grow from low to high and none is
// switched off.
func TestPresets(t *testing.T) {
	p := Presets()
	for i, s := range p {
		if s.Disabled || s.PhotoSizeMax <= 0 || s.VideoSizeMax <= 0 || s.FileSizeMax <= 0 || s.VideoUploadMaxbitrate <= 0 || s.VideoUploadMaxbitrate > 100 {
			t.Errorf("preset %d: %+v", i, s)
		}
	}
	if p[Low].VideoSizeMax >= p[Medium].VideoSizeMax || p[Medium].VideoSizeMax >= p[High].VideoSizeMax ||
		p[Low].FileSizeMax >= p[Medium].FileSizeMax || p[Medium].FileSizeMax >= p[High].FileSizeMax {
		t.Errorf("the presets do not grow: %+v", p)
	}
	if !p[Low].PhonecallsLessData || p[High].PhonecallsLessData {
		t.Errorf("less data for calls: low %v, high %v", p[Low].PhonecallsLessData, p[High].PhonecallsLessData)
	}
}
