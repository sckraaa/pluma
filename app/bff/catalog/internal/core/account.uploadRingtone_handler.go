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
	"errors"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/ringtones"
	"github.com/teamgram/teamgram-server/pkg/ringtones/saved"
)

// AccountUploadRingtone
// account.uploadRingtone#831a83a2 file:InputFile file_name:string mime_type:string = Document;
//
// A notification sound from a file: the document, already in the list of
// the account. The clients add the answer to their list without asking to
// save it (tdesktop api_ringtones.cpp:109-118, iOS
// NotificationSoundList.swift:313-330, Android
// RingtoneUploader.java:39-58), so the upload saves. dfs checks the sound
// and keeps it as MP3 (RINGTONE_SIZE_TOO_BIG, RINGTONE_DURATION_TOO_LONG,
// RINGTONE_MIME_INVALID). A full list refuses the sound before anything is
// stored.
func (c *CatalogCore) AccountUploadRingtone(in *mtproto.TLAccountUploadRingtone) (*mtproto.Document, error) {
	if in.GetFile() == nil {
		err := ringtones.ErrInvalid
		c.Logger.Errorf("account.uploadRingtone - error: %v", err)
		return nil, err
	}
	entries, err := c.svcCtx.Ringtones.Saved(c.ctx, c.MD.UserId)
	if err != nil {
		c.Logger.Errorf("account.uploadRingtone - error: %v", err)
		return nil, err
	}
	if len(entries) >= ringtones.SavedCountMax {
		err = ringtones.ErrInvalid
		c.Logger.Errorf("account.uploadRingtone - the list is full: %v", err)
		return nil, err
	}
	// The parts of the file are kept under the key that uploaded them
	// (upload.saveFilePart).
	d, err := c.svcCtx.Media.MediaUploadRingtoneFile(c.ctx, &media.TLMediaUploadRingtoneFile{
		OwnerId:  c.MD.PermAuthKeyId,
		File:     in.GetFile(),
		MimeType: in.GetMimeType(),
		FileName: in.GetFileName(),
	})
	if err != nil {
		c.Logger.Errorf("account.uploadRingtone - error: %v", err)
		return nil, err
	}
	if err = c.svcCtx.Ringtones.Save(c.ctx, c.MD.UserId, d.GetId()); err != nil {
		c.Logger.Errorf("account.uploadRingtone - error: %v", err)
		if errors.Is(err, saved.ErrTooMany) {
			err = ringtones.ErrInvalid
		}
		return nil, err
	}
	return d, nil
}
