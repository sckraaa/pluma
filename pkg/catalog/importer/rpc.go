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

package importer

import (
	"context"
	"crypto/md5" //nolint:gosec // MTProto's md5_checksum of an uploaded file, a check of the upload, not security
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/teamgram/proto/mtproto"
	dfs_client "github.com/teamgram/teamgram-server/app/service/dfs/client"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/app/service/media/media"
)

// RPCDocuments stores documents through dfs and media, as a client's
// upload goes: the parts to dfs, then dfs.importDocument, which stores
// them as they are, and media.importDocument, which keeps the rows.
type RPCDocuments struct {
	Dfs   dfs_client.DfsClient
	Media media_client.MediaClient
}

const partSize = 512 << 10

// Import implements Documents.
func (r RPCDocuments) Import(ctx context.Context, doc *mtproto.Document, file []byte, thumbs map[string][]byte, id int64) (*mtproto.Document, error) {
	owner, err := newIDHash()
	if err != nil {
		return nil, err
	}
	name := fileName(doc)
	main, err := r.upload(ctx, owner.ID, name, file)
	if err != nil {
		return nil, err
	}
	types := make([]string, 0, len(thumbs))
	for t := range thumbs {
		types = append(types, t)
	}
	sort.Strings(types)
	var thumbFiles []*mtproto.InputFile
	for _, t := range types {
		f, err := r.upload(ctx, owner.ID, t, thumbs[t])
		if err != nil {
			return nil, err
		}
		thumbFiles = append(thumbFiles, f)
	}
	stored, err := r.Dfs.DfsImportDocument(ctx, &dfs.TLDfsImportDocument{
		Creator:     owner.ID,
		Id:          id,
		Date:        doc.GetDate(),
		MimeType:    doc.GetMimeType(),
		File:        main,
		Thumbs:      doc.GetThumbs(),
		ThumbFiles:  thumbFiles,
		VideoThumbs: doc.GetVideoThumbs(),
		Attributes:  doc.GetAttributes(),
	})
	if err != nil {
		return nil, fmt.Errorf("dfs: %w", err)
	}
	if _, err := r.Media.MediaImportDocument(ctx, &media.TLMediaImportDocument{FileName: name, Document: stored}); err != nil {
		return nil, fmt.Errorf("media: %w", err)
	}
	return stored, nil
}

// upload writes data to dfs in parts, as a file named name.
func (r RPCDocuments) upload(ctx context.Context, owner int64, name string, data []byte) (*mtproto.InputFile, error) {
	id, err := newIDHash()
	if err != nil {
		return nil, err
	}
	parts := int32(0)
	for off := 0; off < len(data) || parts == 0; off += partSize {
		end := min(off+partSize, len(data))
		if _, err := r.Dfs.DfsWriteFilePartData(ctx, &dfs.TLDfsWriteFilePartData{
			Creator:  owner,
			FileId:   id.ID,
			FilePart: parts,
			Bytes:    data[off:end],
		}); err != nil {
			return nil, fmt.Errorf("dfs: part %d of %s: %w", parts, name, err)
		}
		parts++
	}
	sum := md5.Sum(data) //nolint:gosec // see the import
	return mtproto.MakeTLInputFile(&mtproto.InputFile{
		Id_INT64:    id.ID,
		Parts:       parts,
		Name:        name,
		Md5Checksum: hex.EncodeToString(sum[:]),
	}).To_InputFile(), nil
}

// Get implements Documents.
func (r RPCDocuments) Get(ctx context.Context, ids []int64) ([]*mtproto.Document, error) {
	var out []*mtproto.Document
	for i := 0; i < len(ids); i += 100 {
		got, err := r.Media.MediaGetDocumentList(ctx, &media.TLMediaGetDocumentList{IdList: ids[i:min(i+100, len(ids))]})
		if err != nil {
			return nil, err
		}
		out = append(out, got.GetDatas()...)
	}
	return out, nil
}

// fileName is the name a document is stored under: its own, or one whose
// extension dfs takes the storage type from (dfs model.GetStorageFileTypeConstructor).
func fileName(doc *mtproto.Document) string {
	for _, a := range doc.GetAttributes() {
		if a.GetPredicateName() == mtproto.Predicate_documentAttributeFilename && a.GetFileName() != "" {
			return a.GetFileName()
		}
	}
	switch doc.GetMimeType() {
	case "application/x-tgsticker":
		return "sticker.tgs"
	case "image/webp":
		return "sticker.webp"
	case "video/webm":
		return "sticker.webm"
	case "video/mp4":
		return "file.mp4"
	case "image/png":
		return "file.png"
	case "image/jpeg":
		return "file.jpg"
	case "application/x-tgwallpattern":
		return "pattern.tgv"
	default:
		return "file"
	}
}
