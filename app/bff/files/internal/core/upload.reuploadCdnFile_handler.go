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
// The CDN node lost a file (task 102, ADR 0065 §4): it is pushed again
// first, and the hashes from offset 0 answer once the copy is ready, within
// CDN_REUPLOAD_WAIT; otherwise FILE_TOKEN_INVALID or REQUEST_TOKEN_INVALID
// send the client back to upload.getFile.
func (c *FilesCore) UploadReuploadCdnFile(in *mtproto.TLUploadReuploadCdnFile) (*mtproto.Vector_FileHash, error) {
	if c.svcCtx.CDN == nil {
		return nil, mtproto.ErrFileTokenInvalid
	}
	hashes, err := c.svcCtx.CDN.Reupload(c.ctx, c.MD.GetUserId(), in.GetFileToken(), in.GetRequestToken())
	if err != nil {
		return nil, err
	}
	return &mtproto.Vector_FileHash{Datas: hashes}, nil
}
