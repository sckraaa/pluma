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

// Package ringtones holds the rules of a notification sound an account may
// keep (task 084): its limits, as help.getAppConfig gives them to the
// clients, and the errors of account.uploadRingtone and
// account.saveRingtone. The list of an account is in
// pkg/ringtones/saved.
package ringtones

import (
	"github.com/teamgram/proto/mtproto"
	"google.golang.org/grpc/status"
)

// The limits of a notification sound (ringtone_size_max,
// ringtone_duration_max, ringtone_saved_count_max of help.getAppConfig,
// core.telegram.org/api/ringtones). They are the values the clients assume
// without the keys: tdesktop api_ringtones.cpp:186-203 (100 KB, 100
// sounds, 5 seconds), Android MessagesController.java:1633-1634.
const (
	// SizeMax is the largest file of a sound, in bytes: of the file sent
	// (the clients refuse a larger one before they upload it, Android
	// ChatAttachAlertDocumentLayout.java:877) and of the file kept.
	SizeMax = 100 * 1024
	// DurationMax is the longest sound, in seconds.
	DurationMax = 5
	// SavedCountMax is how many sounds an account keeps.
	SavedCountMax = 100
)

// MimeMP3 is the type of every kept sound: iOS stores a cloud sound as
// <id>.mp3 for its notifications (NotificationService.swift:1321-1322),
// and "if the notification sound is already in MP3 format" it is kept as
// sent, "otherwise, it will be automatically converted"
// (core.telegram.org/method/account.saveRingtone).
const MimeMP3 = "audio/mpeg"

// The errors of the methods. tdesktop tells the first three apart
// (ringtones_box.cpp:213-227); RINGTONE_INVALID is of account.saveRingtone
// (core.telegram.org/method/account.saveRingtone).
var (
	ErrDurationTooLong = status.Error(mtproto.ErrBadRequest, "RINGTONE_DURATION_TOO_LONG")
	ErrSizeTooBig      = status.Error(mtproto.ErrBadRequest, "RINGTONE_SIZE_TOO_BIG")
	ErrMimeInvalid     = status.Error(mtproto.ErrBadRequest, "RINGTONE_MIME_INVALID")
	ErrInvalid         = status.Error(mtproto.ErrBadRequest, "RINGTONE_INVALID")
)

// IsMP3 tells whether mime names MP3 audio: the names clients give it
// (Android RingtoneDataStore.java:41 lists audio/mpeg3 and audio/mpeg;
// browsers and Qt also say audio/mp3).
func IsMP3(mime string) bool {
	switch mime {
	case "audio/mpeg", "audio/mpeg3", "audio/mp3":
		return true
	}
	return false
}
