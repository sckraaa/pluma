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
	"github.com/teamgram/teamgram-server/pkg/webfile"
)

// ErrWebfileNotAvailable is a web file the server cannot get.
var ErrWebfileNotAvailable = webfile.ErrNotAvailable

// UploadGetWebFile
// upload.getWebFile#24e6818d location:InputWebFileLocation offset:int limit:int = upload.WebFile;
//
// The files of inline bots at addresses the server signed
// (inputWebFileLocation) and, with task 100, maps
// (inputWebFileGeoPointLocation) are served by pkg/webfile; a kind with no
// source registered there is LOCATION_INVALID. The album covers of music
// files are not served.
func (c *FilesCore) UploadGetWebFile(in *mtproto.TLUploadGetWebFile) (*mtproto.Upload_WebFile, error) {
	if in.GetLocation().GetPredicateName() == mtproto.Predicate_inputWebFileAudioAlbumThumbLocation {
		return nil, ErrWebfileNotAvailable
	}
	return c.svcCtx.WebFiles.Get(c.ctx, webfile.Request{UserId: c.MD.GetUserId(), LangCode: c.MD.GetLangCode()},
		in.GetLocation(), in.GetOffset(), in.GetLimit())
}
