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

//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"

	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// TestUploadParts (task 057): a big file uploaded in parts, kept on dfs's
// disk, is sent and downloaded back byte for byte across the parts and the
// 1 MB chunks of upload.getFile; the parts and ranges Telegram refuses
// (core.telegram.org/api/files) are refused.
func TestUploadParts(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	u := login(ctx, t, testPhones(t, 1)[0], "Alice")

	const part = 512 << 10
	file := make([]byte, 2*part+100<<10) // three parts, past the first 1 MB
	for i := range file {
		file[i] = byte(i*7 + i>>16)
	}
	fileId := randomID()
	save := func(n int32, b []byte, total int32) error {
		_, err := u.c.Invoke(ctx, &mtproto.TLUploadSaveBigFilePart{
			Constructor:    u.c.Constructor(mtproto.Predicate_upload_saveBigFilePart),
			FileId:         fileId,
			FilePart:       n,
			FileTotalParts: total,
			Bytes:          b,
		})
		return err
	}
	for n := int32(0); n < 3; n++ {
		end := min(int(n+1)*part, len(file))
		if err := save(n, file[int(n)*part:end], 3); err != nil {
			t.Fatalf("upload.saveBigFilePart %d: %v", n, err)
		}
	}
	for _, c := range []struct {
		name  string
		part  int32
		b     []byte
		total int32
		want  string
	}{
		{"a part past the parts", 3, file[:part], 3, "FILE_PART_INVALID"},
		{"more parts than allowed", 0, file[:part], 4001, "FILE_PARTS_INVALID"},
		{"a part over 512 KB", 0, make([]byte, part+1024), 3, "FILE_PART_TOO_BIG"},
		{"an empty part", 1, nil, 3, "FILE_PART_EMPTY"},
	} {
		if err := save(c.part, c.b, c.total); !mtclient.IsRPCError(err, c.want) {
			t.Errorf("%s: %v, want %s", c.name, err, c.want)
		}
	}

	send := func(id int64, parts int32) (mtproto.TLObject, error) {
		return u.c.Invoke(ctx, &mtproto.TLMessagesSendMedia{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_sendMedia),
			Peer:        &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerSelf)},
			Media: mtproto.MakeTLInputMediaUploadedDocument(&mtproto.InputMedia{
				File:     mtproto.MakeTLInputFileBig(&mtproto.InputFile{Id_INT64: id, Parts: parts, Name: "data.bin"}).To_InputFile(),
				MimeType: "application/octet-stream",
				Attributes: []*mtproto.DocumentAttribute{
					mtproto.MakeTLDocumentAttributeFilename(&mtproto.DocumentAttribute{FileName: "data.bin"}).To_DocumentAttribute(),
				},
			}).To_InputMedia(),
			RandomId: randomID(),
		})
	}
	o, err := send(fileId, 3)
	if err != nil {
		t.Fatalf("messages.sendMedia: %v", err)
	}
	var doc *mtproto.Document
	for _, m := range sentMessages(t, o) {
		if d := m.GetMedia().GetDocument(); d.GetId() != 0 {
			doc = d
		}
	}
	if doc == nil || doc.GetSize2_INT64() != int64(len(file)) {
		t.Fatalf("document %v, want %d bytes", doc, len(file))
	}

	get := func(offset int64, limit int32) ([]byte, error) {
		o, err := u.c.Invoke(ctx, &mtproto.TLUploadGetFile{
			Constructor: u.c.Constructor(mtproto.Predicate_upload_getFile),
			Location: mtproto.MakeTLInputDocumentFileLocation(&mtproto.InputFileLocation{
				Id: doc.GetId(), AccessHash: doc.GetAccessHash(), FileReference: doc.GetFileReference(),
			}).To_InputFileLocation(),
			Offset_INT64: offset,
			Limit:        limit,
		})
		if err != nil {
			return nil, err
		}
		return o.(interface{ To_Upload_File() *mtproto.Upload_File }).To_Upload_File().GetBytes(), nil
	}
	var got []byte
	for off := int64(0); off < int64(len(file)); off += 1 << 20 {
		b, err := get(off, 1<<20)
		if err != nil {
			t.Fatalf("upload.getFile at %d: %v", off, err)
		}
		got = append(got, b...)
	}
	if !bytes.Equal(got, file) {
		t.Fatalf("downloaded %d bytes, not the %d uploaded", len(got), len(file))
	}
	if b, err := get(512<<10+4096, 8192); err != nil || !bytes.Equal(b, file[512<<10+4096:512<<10+4096+8192]) {
		t.Errorf("a range across a part: %d bytes, %v", len(b), err)
	}
	for _, c := range []struct {
		offset int64
		limit  int32
		want   string
	}{
		{0, 2 << 20, "LIMIT_INVALID"},
		{0, 1<<31 - 1, "LIMIT_INVALID"},
		{1000, 4096, "OFFSET_INVALID"},
		{768 << 10, 512 << 10, "LIMIT_INVALID"},
	} {
		if _, err := get(c.offset, c.limit); !mtclient.IsRPCError(err, c.want) {
			t.Errorf("upload.getFile(%d, %d): %v, want %s", c.offset, c.limit, err, c.want)
		}
	}

	// As Android uploads a file still being written (FileUploadOperation,
	// uploadFirstPartLater and estimatedSize): the parts with a total of -1,
	// then a 1 KB part 0 with the real total.
	streamed := randomID()
	fileId = streamed
	rest := file[1024:]
	for n := int32(1); int(n-1)*part < len(rest); n++ {
		end := min(int(n)*part, len(rest))
		if err := save(n, rest[int(n-1)*part:end], -1); err != nil {
			t.Fatalf("streamed part %d: %v", n, err)
		}
	}
	if err := save(0, file[:1024], 4); err != nil {
		t.Fatalf("streamed part 0: %v", err)
	}
	if o, err := send(streamed, 4); err != nil {
		t.Errorf("messages.sendMedia of the streamed file: %v", err)
	} else {
		for _, m := range sentMessages(t, o) {
			if d := m.GetMedia().GetDocument(); d.GetId() != 0 && d.GetSize2_INT64() != int64(len(file)) {
				t.Errorf("streamed file: %d bytes, want %d", d.GetSize2_INT64(), len(file))
			}
		}
	}

	// A file sent with a part never uploaded: Telegram names the part.
	gap := randomID()
	fileId = gap
	for _, n := range []int32{0, 2} {
		end := min(int(n+1)*part, len(file))
		if err := save(n, file[int(n)*part:end], 3); err != nil {
			t.Fatalf("upload.saveBigFilePart %d: %v", n, err)
		}
	}
	if _, err := send(gap, 3); !mtclient.IsRPCError(err, "FILE_PART_1_MISSING") {
		t.Errorf("a missing part: %v, want FILE_PART_1_MISSING", err)
	}
}
