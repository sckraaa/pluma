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

// UploadGetCdnFile
// upload.getCdnFile#395f69da file_token:bytes offset:long limit:int = upload.CdnFile;
//
// The CDN node serves it (app/service/cdn); a client that sends it to the
// home DC is sent back to upload.getFile with FILE_TOKEN_INVALID (Android
// FileLoadOperation.java:2583-2589, tdesktop
// download_manager_mtproto.cpp:958-980). core.telegram.org lists
// METHOD_INVALID for it, which the clients take for a failed download.
func (c *FilesCore) UploadGetCdnFile(in *mtproto.TLUploadGetCdnFile) (*mtproto.Upload_CdnFile, error) {
	return nil, mtproto.ErrFileTokenInvalid
}
