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

// Package autodownload holds the auto-download presets (task 084).
//
// account.getAutoDownloadSettings gives three presets, low, medium and
// high. The clients take them as the presets every account chooses from,
// not as the account's own choice: Android replaces its three base presets
// with the answer (DownloadController.java:362-366) and keeps what the
// user chose for each network apart; iOS replaces its three presets with
// the answer every hour (ManagedAutodownloadSettingsUpdates.swift:8-24).
// So the answer is Presets for everyone.
//
// account.saveAutoDownloadSettings tells the server what the account uses
// on a kind of network. The clients send their custom settings there, not
// an edited preset: Android sends the current settings of mobile data, of
// Wi-Fi and of roaming, each without a flag
// (DownloadController.java:1067-1109); iOS sends the custom settings of
// mobile data as the medium preset and of Wi-Fi as the high one
// (AutodownloadMediaCategoryController.swift:480-496). Given back as
// presets they would overwrite the presets of both clients with one
// network's custom settings, so they are kept apart: Store holds, for each
// account, the last settings saved under each of the three names.
package autodownload

import (
	"context"
	"fmt"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/proto"
)

// Preset names one of the three presets.
type Preset int8

const (
	Low    Preset = 0
	Medium Preset = 1
	High   Preset = 2
)

// Settings is one preset (autoDownloadSettings).
type Settings struct {
	Disabled           bool
	VideoPreloadLarge  bool
	AudioPreloadNext   bool
	PhonecallsLessData bool
	StoriesPreload     bool
	// The largest photo, video and file loaded by itself, in bytes; 0 is
	// "never" (Android DownloadController.java:1104-1106 sends 0 for a
	// kind switched off).
	PhotoSizeMax int32
	VideoSizeMax int64
	FileSizeMax  int64
	// VideoUploadMaxbitrate is a percentage.
	VideoUploadMaxbitrate int32
	// The parallel operations of the clients' download queues; 0 leaves
	// them to the client.
	SmallQueueActiveOperationsMax int32
	LargeQueueActiveOperationsMax int32
}

// Presets are the presets every account chooses from: low, medium and
// high. Their video_upload_maxbitrate is what the clients compress a video
// by: 85 is 720p on mobile data and 100 is 1080p on Wi-Fi, and it is never
// 0, which iOS turns into its 480p preset.
func Presets() [3]Settings {
	return [3]Settings{
		Low: {
			VideoPreloadLarge: true, AudioPreloadNext: true, PhonecallsLessData: true,
			PhotoSizeMax: 1048576, VideoSizeMax: 512000, FileSizeMax: 512000, VideoUploadMaxbitrate: 50,
		},
		Medium: {
			VideoPreloadLarge: true, AudioPreloadNext: true,
			PhotoSizeMax: 1048576, VideoSizeMax: 10485760, FileSizeMax: 1048576, VideoUploadMaxbitrate: 85,
		},
		High: {
			VideoPreloadLarge: true, AudioPreloadNext: true,
			PhotoSizeMax: 1048576, VideoSizeMax: 15728640, FileSizeMax: 3145728, VideoUploadMaxbitrate: 100,
		},
	}
}

// FromTL reads a preset a client sent, of any layer: the sizes of the
// older constructors are ints. A negative size is "never", a bitrate is
// kept within a percentage.
func FromTL(in *mtproto.AutoDownloadSettings) Settings {
	s := Settings{
		Disabled:                      in.GetDisabled(),
		VideoPreloadLarge:             in.GetVideoPreloadLarge(),
		AudioPreloadNext:              in.GetAudioPreloadNext(),
		PhonecallsLessData:            in.GetPhonecallsLessData(),
		StoriesPreload:                in.GetStoriesPreload(),
		PhotoSizeMax:                  max(in.GetPhotoSizeMax(), 0),
		VideoSizeMax:                  max(in.GetVideoSizeMax_INT64(), int64(in.GetVideoSizeMax_INT32()), 0),
		FileSizeMax:                   max(in.GetFileSizeMax_INT64(), int64(in.GetFileSizeMax_INT32()), 0),
		VideoUploadMaxbitrate:         min(max(in.GetVideoUploadMaxbitrate(), 0), 100),
		SmallQueueActiveOperationsMax: max(in.GetSmallQueueActiveOperationsMax(), 0),
		LargeQueueActiveOperationsMax: max(in.GetLargeQueueActiveOperationsMax(), 0),
	}
	return s
}

// TL is the preset as autoDownloadSettings, for every layer: the sizes in
// both of their forms, the ints cut to what an int holds.
func (s Settings) TL() *mtproto.AutoDownloadSettings {
	return mtproto.MakeTLAutoDownloadSettings(&mtproto.AutoDownloadSettings{
		Disabled:                      s.Disabled,
		VideoPreloadLarge:             s.VideoPreloadLarge,
		AudioPreloadNext:              s.AudioPreloadNext,
		PhonecallsLessData:            s.PhonecallsLessData,
		StoriesPreload:                s.StoriesPreload,
		PhotoSizeMax:                  s.PhotoSizeMax,
		VideoSizeMax_INT64:            s.VideoSizeMax,
		VideoSizeMax_INT32:            int32(min(s.VideoSizeMax, 1<<31-1)),
		FileSizeMax_INT64:             s.FileSizeMax,
		FileSizeMax_INT32:             int32(min(s.FileSizeMax, 1<<31-1)),
		VideoUploadMaxbitrate:         s.VideoUploadMaxbitrate,
		SmallQueueActiveOperationsMax: s.SmallQueueActiveOperationsMax,
		LargeQueueActiveOperationsMax: s.LargeQueueActiveOperationsMax,
	}).To_AutoDownloadSettings()
}

// Store reads and writes what the accounts saved.
type Store struct {
	db *sqlx.DB
}

// NewStore returns a Store on db.
func NewStore(db *sqlx.DB) *Store {
	return &Store{db: db}
}

type row struct {
	Preset   int8   `db:"preset"`
	Settings []byte `db:"settings"`
}

// Saved returns what the account saved, by the name it saved it under;
// a name it never saved under is not in the map.
func (s *Store) Saved(ctx context.Context, userID int64) (map[Preset]Settings, error) {
	var rows []row
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT preset, settings FROM user_autodownload_settings WHERE user_id = ?", userID); err != nil {
		return nil, err
	}
	saved := make(map[Preset]Settings, len(rows))
	for _, r := range rows {
		tl := &mtproto.AutoDownloadSettings{}
		if err := proto.Unmarshal(r.Settings, tl); err != nil {
			return nil, fmt.Errorf("autodownload: preset %d of user %d: %w", r.Preset, userID, err)
		}
		saved[Preset(r.Preset)] = FromTL(tl)
	}
	return saved, nil
}

// Save keeps settings as what the account uses under the name preset, in
// place of what it saved there before: the last save wins, and the same
// save twice changes nothing.
func (s *Store) Save(ctx context.Context, userID int64, preset Preset, settings Settings, now int64) error {
	if preset < Low || preset > High {
		return fmt.Errorf("autodownload: preset %d", preset)
	}
	data, err := proto.Marshal(settings.TL())
	if err != nil {
		return fmt.Errorf("autodownload: marshal: %w", err)
	}
	_, err = s.db.Exec(ctx, "INSERT INTO user_autodownload_settings (user_id, preset, settings, updated_at) VALUES (?, ?, ?, ?)"+
		" AS new ON DUPLICATE KEY UPDATE settings = new.settings, updated_at = new.updated_at", userID, int8(preset), data, now)
	return err
}
