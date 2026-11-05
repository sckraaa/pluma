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
	"fmt"
	"io"
	"strconv"
	"time"
)

var (
	ErrorDfsFileNotFound = fmt.Errorf("dfs file not found")
)

// PartsConf is where the parts of uploads wait until their file is stored
// (task 057): on dfs's own disk, not in Redis, whose memory one 2 GB upload
// (4000 parts of 512 KB, core.telegram.org/api/files) would fill.
type PartsConf struct {
	// Dir holds <owner>/<file id>/<part number>.
	Dir string `json:",default=/data/parts"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// MinFreeBytes: below this much free space new parts are refused, so
	// that uploads never fill the disk the other services share.
	MinFreeBytes int64 `json:",default=1073741824"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

type DfsFileInfo struct {
	Creator           int64  `json:"creator"`
	FileId            int64  `json:"file_id"`
	Big               bool   `json:"big"`
	FileName          string `json:"file_name"`
	FileTotalParts    int    `json:"file_total_parts"`
	FirstFilePartSize int    `json:"first_file_part_size"`
	FilePartSize      int    `json:"file_part_size"`
	LastFilePartSize  int    `json:"last_file_part_size"`
	MimeType          string `json:"mime_type"`
	Mtime             int64  `json:"mtime"`
	// Verified: the sizes above were read from the parts on disk, all of
	// them present (dao.OpenFile), so readers need not check them again.
	Verified bool `json:"verified"`
}

func (m *DfsFileInfo) ToArgs() map[string]string {
	values := make(map[string]string, 8)
	if m.Big {
		values["big"] = "true"
	}
	if m.FileName != "" {
		values["file_name"] = m.FileName
	}
	if m.FileTotalParts > 0 {
		values["file_total_parts"] = strconv.Itoa(m.FileTotalParts)
	}
	if m.FirstFilePartSize > 0 {
		values["first_file_part_size"] = strconv.Itoa(m.FirstFilePartSize)
	}
	if m.FilePartSize > 0 {
		values["file_part_size"] = strconv.Itoa(m.FilePartSize)
	}
	if m.LastFilePartSize > 0 {
		values["last_file_part_size"] = strconv.Itoa(m.LastFilePartSize)
	}
	if m.Mtime > 0 {
		values["mtime"] = strconv.Itoa(int(m.Mtime))
	}
	if m.Verified {
		values["verified"] = "true"
	}
	return values
}

func (m *DfsFileInfo) GetFileSize() int64 {
	if m.FileTotalParts <= 0 {
		return 0
	} else if m.FileTotalParts == 1 {
		return int64(m.FirstFilePartSize)
	} else if m.FileTotalParts == 2 {
		return int64(m.FirstFilePartSize + m.FilePartSize)
	} else {
		return int64(m.FirstFilePartSize + (m.FileTotalParts-2)*m.FilePartSize + m.LastFilePartSize)
	}
}

type DfsHttpFileInfo struct {
	io.ReadSeeker
	Name    string
	Modtime time.Time
}
