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

package dao

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/dfs/internal/model"
)

// MaxInMemoryFile is the largest upload a handler may read into memory
// whole (photos, wallpapers, themes, GIFs, profile videos). The official
// clients send far less: they compress photos, turn GIFs into MP4 and keep
// profile videos to seconds. Everything else is streamed.
const MaxInMemoryFile = 64 << 20

// ErrTooLargeForMemory: the upload is larger than MaxInMemoryFile.
var ErrTooLargeForMemory = errors.New("dfs: upload too large to process in memory")

// PartsReader reads an uploaded file from its parts on disk (task 057). Its
// size and the offset of every part come from the parts themselves, checked
// when it is opened. ReadAt is safe for concurrent use (object storage
// uploads read several parts at once); Read and Seek are not.
type PartsReader struct {
	*model.DfsFileInfo
	store  *partStore
	starts []int64 // starts[i]: offset of part i; starts[len-1]: the size
	pos    int64
}

// openParts checks the parts of info and returns their reader: every part
// from 0 to FileTotalParts-1 present (FILE_PART_X_MISSING, as Telegram
// answers), the middle ones of one size, the first and the last no larger
// (FILE_PART_SIZE_CHANGED). The first may be smaller: Android uploads a file
// still being written with a 1 KB part 0, sent last (FileUploadOperation,
// uploadFirstPartLater). info is filled from the parts and marked verified.
func (s *partStore) openParts(info *model.DfsFileInfo) (*PartsReader, error) {
	total := info.FileTotalParts
	if total <= 0 {
		return nil, model.ErrorDfsFileNotFound
	}
	if total > model.MaxFileParts {
		return nil, mtproto.ErrFilePartsInvalid
	}
	sizes := make([]int64, total)
	for i := range sizes {
		fi, err := os.Stat(s.partPath(info.Creator, info.FileId, i))
		if errors.Is(err, os.ErrNotExist) {
			return nil, mtproto.NewFilePartXMissing(int32(i))
		}
		if err != nil {
			return nil, err
		}
		if sizes[i] = fi.Size(); sizes[i] == 0 {
			return nil, mtproto.ErrFilePartEmpty
		}
	}
	if total > 1 {
		ref := sizes[1] // the size of the middle parts
		for i := 1; i < total-1; i++ {
			if sizes[i] != ref {
				return nil, mtproto.ErrFilePartSizeChanged
			}
		}
		if total > 2 && (sizes[0] > ref || sizes[total-1] > ref) {
			return nil, mtproto.ErrFilePartSizeChanged
		}
	}
	info.FirstFilePartSize = int(sizes[0])
	info.FilePartSize, info.LastFilePartSize = 0, 0
	switch {
	case total == 2:
		// GetFileSize counts FilePartSize as the second (and last) part.
		info.FilePartSize = int(sizes[1])
	case total > 2:
		info.FilePartSize = int(sizes[1])
		info.LastFilePartSize = int(sizes[total-1])
	}
	info.Verified = true
	return newPartsReader(s, info), nil
}

// newPartsReader reads the parts of a verified info, whose sizes give the
// offsets (GetFileSize's layout).
func newPartsReader(s *partStore, info *model.DfsFileInfo) *PartsReader {
	total := info.FileTotalParts
	starts := make([]int64, total+1)
	for i := 0; i < total; i++ {
		size := int64(info.FilePartSize)
		switch {
		case i == 0:
			size = int64(info.FirstFilePartSize)
		case i == total-1 && total > 2:
			size = int64(info.LastFilePartSize)
		}
		starts[i+1] = starts[i] + size
	}
	return &PartsReader{DfsFileInfo: info, store: s, starts: starts}
}

// Size is the size of the file.
func (r *PartsReader) Size() int64 { return r.starts[len(r.starts)-1] }

// ReadAt implements io.ReaderAt.
func (r *PartsReader) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("dfs: negative offset %d", off)
	}
	size := r.Size()
	if off >= size {
		return 0, io.EOF
	}
	n := 0
	// The part holding off: the last start not after it.
	i := sort.Search(len(r.starts)-1, func(i int) bool { return r.starts[i+1] > off })
	for n < len(p) && i < len(r.starts)-1 {
		m, err := r.readPart(i, p[n:], off+int64(n)-r.starts[i])
		n += m
		if err != nil {
			return n, err
		}
		i++
	}
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// readPart reads from part i at offset off within it, up to the part's end.
func (r *PartsReader) readPart(i int, p []byte, off int64) (int, error) {
	if left := r.starts[i+1] - r.starts[i] - off; int64(len(p)) > left {
		p = p[:left]
	}
	f, err := os.Open(r.store.partPath(r.Creator, r.FileId, i))
	if errors.Is(err, os.ErrNotExist) {
		return 0, mtproto.NewFilePartXMissing(int32(i))
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()
	n, err := f.ReadAt(p, off)
	if err == io.EOF && n == len(p) {
		err = nil
	}
	if err == io.EOF {
		// Shorter than when it was opened: replaced by a different part.
		err = mtproto.ErrFilePartSizeChanged
	}
	return n, err
}

// Read implements io.Reader.
func (r *PartsReader) Read(p []byte) (int, error) {
	n, err := r.ReadAt(p, r.pos)
	r.pos += int64(n)
	if err == io.EOF && n > 0 {
		err = nil
	}
	return n, err
}

// Seek implements io.Seeker.
func (r *PartsReader) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = r.pos + offset
	case io.SeekEnd:
		abs = r.Size() + offset
	default:
		return 0, fmt.Errorf("dfs: invalid whence %d", whence)
	}
	if abs < 0 {
		return 0, fmt.Errorf("dfs: negative position %d", abs)
	}
	r.pos = abs
	return abs, nil
}

// ReadAll returns the whole file, if it is at most MaxInMemoryFile.
func (r *PartsReader) ReadAll(ctx context.Context) ([]byte, error) {
	size := r.Size()
	if size > MaxInMemoryFile {
		return nil, ErrTooLargeForMemory
	}
	b := make([]byte, size)
	for off := int64(0); off < size; {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := min(off+1<<20, size)
		n, err := r.ReadAt(b[off:end], off)
		off += int64(n)
		if err != nil && (err != io.EOF || off != size) {
			return nil, err
		}
	}
	return b, nil
}
