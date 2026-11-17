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
)

// UploadReuploadCdnFile
// upload.reuploadCdnFile#9b2754a8 file_token:bytes request_token:bytes = Vector<FileHash>;
//
// Always FILE_TOKEN_INVALID, which sends a client back to upload.getFile:
// an HTTPS delivery pulls a file it lacks from the origin by itself (task
// 107, ADR 0071 §9). With the CDN on, a user is limited to 30 calls a
// minute (cdn.Home.Reupload).
func (c *FilesCore) UploadReuploadCdnFile(in *mtproto.TLUploadReuploadCdnFile) (*mtproto.Vector_FileHash, error) {
	if c.svcCtx.CDN == nil {
		return nil, mtproto.ErrFileTokenInvalid
	}
	return nil, c.svcCtx.CDN.Reupload(c.MD.GetUserId())
}
