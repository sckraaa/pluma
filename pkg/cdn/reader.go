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

package cdn

import (
	"context"

	"github.com/teamgram/proto/mtproto"
)

// ReadChunkSize is how much of a file is read from dfs at a time: the
// largest part of upload.getFile.
const ReadChunkSize = 1 << 20

// FileReader reads a file the way upload.getFile does: dfs.downloadFile.
type FileReader interface {
	// ReadPart returns the bytes of the file of loc from offset, a multiple
	// of limit, at most limit (ReadChunkSize). dfs answers a part it
	// failed to read with no bytes, not an error
	// (app/service/dfs/internal/core/dfs.downloadFile_handler.go): the
	// caller checks every length.
	ReadPart(ctx context.Context, loc *mtproto.InputFileLocation, offset int64, limit int32) ([]byte, error)
}

// Location is the location dfs reads the file of key by. dfs finds a
// document and a secret chat file by id and a photo by id and size; the
// access hash only names the storage type of its answer, which the reader
// does not use (dfs.downloadFile_handler.go).
func Location(key FileKey) *mtproto.InputFileLocation {
	switch key.Kind {
	case KindDocument:
		return mtproto.MakeTLInputDocumentFileLocation(&mtproto.InputFileLocation{Id: key.Id, FileReference: []byte{}}).To_InputFileLocation()
	case KindPhoto:
		return mtproto.MakeTLInputPhotoFileLocation(&mtproto.InputFileLocation{Id: key.Id, FileReference: []byte{}, ThumbSize: key.Thumb}).To_InputFileLocation()
	case KindEncrypted:
		return mtproto.MakeTLInputEncryptedFileLocation(&mtproto.InputFileLocation{Id: key.Id}).To_InputFileLocation()
	}
	return nil
}
