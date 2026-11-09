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

package secretchat

import (
	"context"
	"fmt"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
)

// Dfs stores the uploaded parts of an encrypted file.
type Dfs interface {
	DfsUploadEncryptedFileV2(ctx context.Context, in *dfs.TLDfsUploadEncryptedFileV2) (*mtproto.EncryptedFile, error)
}

// ResolveFile turns an InputEncryptedFile into a stored file. An upload is
// stored and recorded with its key fingerprint and a fresh access hash; an
// inputEncryptedFile (a forwarded file, not stored twice,
// core.telegram.org/api/end-to-end) must name a stored file.
func (s *Store) ResolveFile(ctx context.Context, store Dfs, creator int64, in *mtproto.InputEncryptedFile) (*File, error) {
	switch in.GetPredicateName() {
	case mtproto.Predicate_inputEncryptedFileEmpty, "":
		return nil, mtproto.ErrFileEmtpy
	case mtproto.Predicate_inputEncryptedFile:
		f, err := s.File(ctx, in.GetId())
		if err != nil {
			return nil, fmt.Errorf("secretchat: file: %w", err)
		}
		if f == nil || f.AccessHash != in.GetAccessHash() {
			return nil, mtproto.ErrFileIdInvalid
		}
		return f, nil
	}
	stored, err := store.DfsUploadEncryptedFileV2(ctx, &dfs.TLDfsUploadEncryptedFileV2{Creator: creator, File: in})
	if err != nil {
		return nil, err
	}
	f := &File{
		Id:             stored.GetId(),
		AccessHash:     randomInt64(),
		Size:           stored.GetSize2_INT64(),
		DcId:           stored.GetDcId(),
		KeyFingerprint: in.GetKeyFingerprint(),
	}
	if err = s.SaveFile(ctx, f); err != nil {
		return nil, fmt.Errorf("secretchat: save file: %w", err)
	}
	return f, nil
}

// CheckFile tells whether id and accessHash name a stored file.
func (s *Store) CheckFile(ctx context.Context, id, accessHash int64) (bool, error) {
	f, err := s.File(ctx, id)
	if err != nil {
		return false, err
	}
	return f != nil && f.AccessHash == accessHash, nil
}
