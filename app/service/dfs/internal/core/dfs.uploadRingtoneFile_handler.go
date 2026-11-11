// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package core

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
	"github.com/teamgram/teamgram-server/app/service/dfs/internal/ffmpegutil"
	"github.com/teamgram/teamgram-server/app/service/dfs/internal/model"
	"github.com/teamgram/teamgram-server/pkg/ringtones"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// DfsUploadRingtoneFile
// dfs.uploadRingtoneFile creator:long file:InputFile mime_type:string file_name:string = Document;
//
// A notification sound from an uploaded file (account.uploadRingtone): an
// MP3 or Ogg sound of at most ringtones.SizeMax bytes and
// ringtones.DurationMax seconds. What the file is comes from the file, not
// from mime_type: iOS says audio/mpeg for every sound it uploads
// (NotificationSoundList.swift:313). The document is always MP3: an MP3 is
// kept as sent, an Ogg sound is converted ("if the notification sound is
// already in MP3 format ... otherwise, it will be automatically converted",
// core.telegram.org/method/account.saveRingtone), because iOS plays a cloud
// sound from a file named <id>.mp3 (NotificationService.swift:1321-1322).
//
// The errors are the ones tdesktop tells apart (ringtones_box.cpp:213-227):
// RINGTONE_SIZE_TOO_BIG, RINGTONE_DURATION_TOO_LONG and, for a file that is
// no sound of these formats, RINGTONE_MIME_INVALID.
func (c *DfsCore) DfsUploadRingtoneFile(in *dfs.TLDfsUploadRingtoneFile) (*mtproto.Document, error) {
	file := in.GetFile()
	if file == nil {
		err := mtproto.ErrInputRequestInvalid
		c.Logger.Errorf("dfs.uploadRingtoneFile - error: %v", err)
		return nil, err
	}
	if !strings.HasPrefix(in.GetMimeType(), "audio/") {
		err := ringtones.ErrMimeInvalid
		c.Logger.Errorf("dfs.uploadRingtoneFile - mime type %q: %v", in.GetMimeType(), err)
		return nil, err
	}
	if err := model.CheckFileParts(file.GetParts()); err != nil {
		c.Logger.Errorf("dfs.uploadRingtoneFile - %v", err)
		return nil, err
	}
	creatorId := in.GetCreator()
	fr, err := c.svcCtx.Dao.OpenFile(c.ctx, creatorId, file.GetId_INT64(), file.GetParts())
	if err != nil {
		c.Logger.Errorf("dfs.uploadRingtoneFile - open the file: %v", err)
		return nil, err
	}
	if fr.Size() > ringtones.SizeMax {
		err = ringtones.ErrSizeTooBig
		c.Logger.Errorf("dfs.uploadRingtoneFile - %d bytes: %v", fr.Size(), err)
		return nil, err
	}
	data, err := fr.ReadAll(c.ctx)
	if err != nil {
		c.Logger.Errorf("dfs.uploadRingtoneFile - read the file: %v", err)
		return nil, err
	}

	// ffprobe and ffmpeg read the sound from a file of ours: from a pipe
	// they cannot tell how long an Ogg sound is.
	tmp, err := os.CreateTemp("", "ringtone-*")
	if err != nil {
		c.Logger.Errorf("dfs.uploadRingtoneFile - a temporary file: %v", err)
		return nil, err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		c.Logger.Errorf("dfs.uploadRingtoneFile - a temporary file: %v", err)
		return nil, err
	}

	info, err := c.svcCtx.FFmpegUtil.ProbeAudio(c.ctx, tmp.Name())
	if err != nil {
		c.Logger.Errorf("dfs.uploadRingtoneFile - probe: %v", err)
		if errors.Is(err, ffmpegutil.ErrNotAudio) {
			err = ringtones.ErrMimeInvalid
		}
		return nil, err
	}
	// A sound of 5.05 seconds is one of 5: encoders pad the last frame.
	const padding = 0.25
	duration := int32(math.Round(info.Duration))
	if info.Duration > ringtones.DurationMax+padding {
		err = ringtones.ErrDurationTooLong
		c.Logger.Errorf("dfs.uploadRingtoneFile - %.2f seconds: %v", info.Duration, err)
		return nil, err
	}
	fileName := path.Base(strings.ReplaceAll(in.GetFileName(), "\\", "/"))
	if fileName == "." || fileName == "/" {
		fileName = ""
	}
	title := strings.TrimSuffix(fileName, path.Ext(fileName))
	if info.Format != "mp3" {
		if data, err = c.svcCtx.FFmpegUtil.ToMP3(c.ctx, tmp.Name()); err != nil {
			c.Logger.Errorf("dfs.uploadRingtoneFile - convert: %v", err)
			if errors.Is(err, ffmpegutil.ErrNotAudio) {
				err = ringtones.ErrMimeInvalid
			}
			return nil, err
		}
		if len(data) > ringtones.SizeMax {
			err = ringtones.ErrSizeTooBig
			c.Logger.Errorf("dfs.uploadRingtoneFile - %d bytes converted: %v", len(data), err)
			return nil, err
		}
		fileName = title + ".mp3"
	}

	documentId, err := c.svcCtx.Dao.IDGenClient2.NextId(c.ctx)
	if err != nil {
		c.Logger.Errorf("dfs.uploadRingtoneFile - an id: %v", err)
		return nil, err
	}
	var secret [4]byte
	if _, err = rand.Read(secret[:]); err != nil {
		return nil, err
	}
	accessHash := int64(model.GetStorageFileTypeConstructor(".mp3"))<<32 | int64(binary.LittleEndian.Uint32(secret[:]))
	size := int64(len(data))
	if _, err = c.svcCtx.Dao.PutDocumentFile(c.ctx, fmt.Sprintf("%d.dat", documentId), bytes.NewReader(data), size); err != nil {
		c.Logger.Errorf("dfs.uploadRingtoneFile - store the file: %v", err)
		return nil, err
	}
	// The sound is stored for good; parts not dropped still expire.
	if err = c.svcCtx.Dao.DropFile(c.ctx, creatorId, file.GetId_INT64()); err != nil {
		c.Logger.Errorf("dfs.uploadRingtoneFile - drop the parts of %d: %v", file.GetId_INT64(), err)
	}

	audio := &mtproto.DocumentAttribute{
		Duration:         float64(duration),
		Duration_INT32:   duration,
		Duration_FLOAT64: float64(duration),
	}
	if title != "" {
		audio.Title = wrapperspb.String(title)
	}
	attributes := []*mtproto.DocumentAttribute{mtproto.MakeTLDocumentAttributeAudio(audio).To_DocumentAttribute()}
	if fileName != "" {
		attributes = append(attributes, mtproto.MakeTLDocumentAttributeFilename(&mtproto.DocumentAttribute{
			FileName: fileName,
		}).To_DocumentAttribute())
	}
	return mtproto.MakeTLDocument(&mtproto.Document{
		Id:            documentId,
		AccessHash:    accessHash,
		FileReference: []byte{},
		Date:          int32(time.Now().Unix()),
		MimeType:      ringtones.MimeMP3,
		Size2:         size,
		Size2_INT32:   int32(size),
		Size2_INT64:   size,
		DcId:          1,
		Attributes:    attributes,
	}).To_Document(), nil
}
