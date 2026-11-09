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

package model

import (
	"github.com/teamgram/proto/mtproto"
)

// MaxFileParts is upload_max_fileparts_premium of help.getAppConfig
// (teamgramd/bin/app_config.json): 4 GB in parts of 512 KB, as Telegram
// (core.telegram.org/api/files). The limit without Premium, 4000, is
// checked by bff, which knows the user (upload.saveBigFilePart).
const MaxFileParts = 8000

// MaxFilePartSize: a part holds at most 512 KB (FILE_PART_TOO_BIG).
const MaxFilePartSize = 512 << 10

// CheckFileParts
// FILE_PARTS_INVALID: Invalid number of parts. The value is not between
// 1..upload_max_fileparts_* (core.telegram.org/api/files).
func CheckFileParts(fileParts int32) (err error) {
	if fileParts < 1 || fileParts > MaxFileParts {
		err = mtproto.ErrFilePartsInvalid
	}
	return
}

// CheckFilePart
// FILE_PART_INVALID: The file part number is invalid. The value is not
// between 0 and upload_max_fileparts_*-1.
func CheckFilePart(filePart int32) (err error) {
	if filePart < 0 || filePart >= MaxFileParts {
		err = mtproto.ErrFilePartInvalid
	}
	return
}

// CheckFilePartData checks the content of one part: not empty
// (FILE_PART_EMPTY), at most 512 KB (FILE_PART_TOO_BIG). Its size against
// the other parts is checked when the file is used (dao.OpenFile).
func CheckFilePartData(b []byte) error {
	switch {
	case len(b) == 0:
		return mtproto.ErrFilePartEmpty
	case len(b) > MaxFilePartSize:
		return mtproto.ErrFilePartTooBig
	}
	return nil
}
