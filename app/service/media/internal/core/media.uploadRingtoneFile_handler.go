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
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
	"github.com/teamgram/teamgram-server/app/service/media/media"
)

// MediaUploadRingtoneFile
// media.uploadRingtoneFile owner_id:long file:InputFile mime_type:string file_name:string = Document;
//
// The document of a notification sound from an uploaded file
// (account.uploadRingtone): dfs checks and stores the sound, always as
// MP3; the document is kept as any other, so it is found by its id later.
func (c *MediaCore) MediaUploadRingtoneFile(in *media.TLMediaUploadRingtoneFile) (*mtproto.Document, error) {
	document, err := c.svcCtx.Dao.DfsClient.DfsUploadRingtoneFile(c.ctx, &dfs.TLDfsUploadRingtoneFile{
		Creator:  in.GetOwnerId(),
		File:     in.GetFile(),
		MimeType: in.GetMimeType(),
		FileName: in.GetFileName(),
	})
	if err != nil {
		c.Logger.Errorf("media.uploadRingtoneFile - error: %v", err)
		return nil, err
	}
	fileName := ""
	for _, a := range document.GetAttributes() {
		if a.GetPredicateName() == mtproto.Predicate_documentAttributeFilename {
			fileName = a.GetFileName()
		}
	}
	if err = saveDocument(c, fileName, document); err != nil {
		c.Logger.Errorf("media.uploadRingtoneFile - error: %v", err)
		return nil, err
	}
	return document, nil
}
