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

package core

import (
	"context"
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/autodownload/internal/svc"
	"github.com/teamgram/teamgram-server/pkg/autodownload"

	"github.com/zeromicro/go-zero/core/logx"
)

type savedKey struct {
	user   int64
	preset autodownload.Preset
}

// fakeSaved keeps the last settings saved under each name.
type fakeSaved struct {
	saved map[savedKey]autodownload.Settings
}

func (f *fakeSaved) Save(_ context.Context, userID int64, preset autodownload.Preset, settings autodownload.Settings, now int64) error {
	if now == 0 {
		return errors.New("saved without a time")
	}
	f.saved[savedKey{userID, preset}] = settings
	return nil
}

func newAutoDownloadCore(userId int64, saved *fakeSaved) *AutoDownloadCore {
	ctx := context.Background()
	return &AutoDownloadCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Saved: saved},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: userId},
	}
}

// What an account saves is kept under the name it was saved under, low,
// high or, without a flag, medium, and the presets everybody chooses from
// stay what they are for it and for everyone else.
func TestSaveAutoDownloadSettings(t *testing.T) {
	saved := &fakeSaved{saved: map[savedKey]autodownload.Settings{}}
	c := newAutoDownloadCore(7, saved)
	settings := func(video int64, disabled bool) *mtproto.AutoDownloadSettings {
		return mtproto.MakeTLAutoDownloadSettings(&mtproto.AutoDownloadSettings{Disabled: disabled, PhotoSizeMax: 1000, VideoSizeMax_INT64: video, FileSizeMax_INT64: 0, VideoUploadMaxbitrate: 60}).To_AutoDownloadSettings()
	}
	save := func(low, high bool, s *mtproto.AutoDownloadSettings) error {
		ok, err := c.AccountSaveAutoDownloadSettings(&mtproto.TLAccountSaveAutoDownloadSettings{Low: low, High: high, Settings: s})
		if err == nil && !mtproto.FromBool(ok) {
			t.Errorf("saved, answered %v", ok)
		}
		return err
	}
	if err := save(false, false, settings(111, true)); err != nil {
		t.Fatal(err)
	}
	if err := save(true, false, settings(222, false)); err != nil {
		t.Fatal(err)
	}
	if err := save(false, true, settings(333, false)); err != nil {
		t.Fatal(err)
	}
	if err := save(false, true, settings(444, false)); err != nil { // the last save wins
		t.Fatal(err)
	}
	for preset, video := range map[autodownload.Preset]int64{autodownload.Low: 222, autodownload.Medium: 111, autodownload.High: 444} {
		got, ok := saved.saved[savedKey{7, preset}]
		if !ok || got.VideoSizeMax != video || got.PhotoSizeMax != 1000 || got.VideoUploadMaxbitrate != 60 || got.Disabled != (preset == autodownload.Medium) {
			t.Errorf("preset %d kept as %+v (%v), want the video size %d", preset, got, ok, video)
		}
	}
	if err := save(true, true, settings(1, false)); !errors.Is(err, mtproto.ErrInputRequestInvalid) {
		t.Errorf("low and high together: %v, want INPUT_REQUEST_INVALID", err)
	}
	if err := save(false, false, nil); !errors.Is(err, mtproto.ErrInputRequestInvalid) {
		t.Errorf("no settings: %v, want INPUT_REQUEST_INVALID", err)
	}
	if len(saved.saved) != 3 {
		t.Errorf("%d rows kept, want 3", len(saved.saved))
	}

	// The presets do not follow what was saved.
	want := autodownload.Presets()
	for _, user := range []int64{7, 8} {
		r, err := newAutoDownloadCore(user, saved).AccountGetAutoDownloadSettings(&mtproto.TLAccountGetAutoDownloadSettings{})
		if err != nil || r.GetPredicateName() != mtproto.Predicate_account_autoDownloadSettings {
			t.Fatalf("the presets of user %d: %v, %v", user, r, err)
		}
		for name, pair := range map[string][2]autodownload.Settings{
			"low":    {autodownload.FromTL(r.GetLow()), want[autodownload.Low]},
			"medium": {autodownload.FromTL(r.GetMedium()), want[autodownload.Medium]},
			"high":   {autodownload.FromTL(r.GetHigh()), want[autodownload.High]},
		} {
			if pair[0] != pair[1] {
				t.Errorf("user %d, %s: %+v, want %+v", user, name, pair[0], pair[1])
			}
		}
		if r.GetMedium().GetVideoSizeMax_INT32() == 0 || r.GetMedium().GetFileSizeMax_INT32() == 0 {
			t.Errorf("user %d: the sizes of the older layers are missing: %v", user, r.GetMedium())
		}
	}
}
