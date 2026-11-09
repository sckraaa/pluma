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
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"google.golang.org/grpc/status"

	"github.com/teamgram/marmota/pkg/stores/kv"
	"github.com/teamgram/teamgram-server/app/service/dfs/internal/model"
)

func newTestDao(t *testing.T) (*Dao, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	d := &Dao{
		ssdb: kv.NewStore(kv.KvConf{cache.NodeConf{
			RedisConf: redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType, DisableIdentity: true},
			Weight:    100,
		}}),
		parts:   newPartStore(model.PartsConf{Dir: t.TempDir()}),
		persist: newPersister(nil),
	}
	return d, mr
}

// upload writes parts of the given sizes, filled with their offsets, as a
// client does (part 0 records the file info), and returns the whole file.
func upload(t *testing.T, d *Dao, owner, id int64, big bool, sizes ...int) []byte {
	t.Helper()
	ctx := context.Background()
	var file []byte
	for i, n := range sizes {
		part := make([]byte, n)
		for j := range part {
			part[j] = byte(len(file) + j)
		}
		file = append(file, part...)
		if err := d.WriteFilePartData(ctx, owner, id, int32(i), part); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			info := &model.DfsFileInfo{Creator: owner, FileId: id, Big: big, Mtime: time.Now().Unix()}
			if big {
				info.FileTotalParts = len(sizes)
			}
			if err := d.SetFileInfo(ctx, info); err != nil {
				t.Fatal(err)
			}
		}
	}
	return file
}

func rpcMessage(err error) string {
	if s, ok := status.FromError(err); ok {
		return s.Message()
	}
	return ""
}

// TestPartsReader: the parts read back as one file, from any offset and
// across their boundaries, sequentially, by seeking and concurrently.
func TestPartsReader(t *testing.T) {
	d, _ := newTestDao(t)
	ctx := context.Background()
	want := upload(t, d, 1, 7, false, 1024, 1024, 500)
	r, err := d.OpenFile(ctx, 1, 7, 3)
	if err != nil {
		t.Fatal(err)
	}
	if r.Size() != 2548 || r.GetFileSize() != 2548 {
		t.Fatalf("size %d, file info %d", r.Size(), r.GetFileSize())
	}
	for _, c := range []struct{ off, n int }{{0, 2548}, {1000, 100}, {1023, 2}, {2047, 501}, {2500, 100}, {0, 1}} {
		p := make([]byte, c.n)
		n, err := r.ReadAt(p, int64(c.off))
		end := min(c.off+c.n, len(want))
		if n != end-c.off || !bytes.Equal(p[:n], want[c.off:end]) {
			t.Errorf("ReadAt(%d, %d): %d bytes, %v", c.off, c.n, n, err)
		}
		if (end < c.off+c.n) != errors.Is(err, io.EOF) {
			t.Errorf("ReadAt(%d, %d): error %v", c.off, c.n, err)
		}
	}
	if got, err := io.ReadAll(r); err != nil || !bytes.Equal(got, want) {
		t.Errorf("Read: %d bytes, %v", len(got), err)
	}
	if _, err := r.Seek(-48, io.SeekEnd); err != nil {
		t.Fatal(err)
	}
	if got, _ := io.ReadAll(r); !bytes.Equal(got, want[2500:]) {
		t.Errorf("after Seek: %d bytes", len(got))
	}
	if got, err := r.ReadAll(ctx); err != nil || !bytes.Equal(got, want) {
		t.Errorf("ReadAll: %d bytes, %v", len(got), err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(off int64) {
			defer wg.Done()
			p := make([]byte, 700)
			if n, _ := r.ReadAt(p, off); !bytes.Equal(p[:n], want[off:off+int64(n)]) {
				t.Errorf("concurrent ReadAt(%d)", off)
			}
		}(int64(i * 250))
	}
	wg.Wait()
}

// TestOpenFileChecksParts: a missing part, a part of another size or a
// count other than the big file's is refused as Telegram refuses it.
func TestOpenFileChecksParts(t *testing.T) {
	d, _ := newTestDao(t)
	ctx := context.Background()
	upload(t, d, 1, 1, false, 1024, 1024)
	if _, err := d.OpenFile(ctx, 1, 1, 3); rpcMessage(err) != "FILE_PART_2_MISSING" {
		t.Errorf("a missing part: %v", err)
	}
	upload(t, d, 1, 2, false, 1024, 512, 1024)
	if _, err := d.OpenFile(ctx, 1, 2, 3); rpcMessage(err) != "FILE_PART_SIZE_CHANGED" {
		t.Errorf("a part of another size: %v", err)
	}
	upload(t, d, 1, 3, false, 1024, 2048, 4096)
	if _, err := d.OpenFile(ctx, 1, 3, 3); rpcMessage(err) != "FILE_PART_SIZE_CHANGED" {
		t.Errorf("a last part larger than the others: %v", err)
	}
	upload(t, d, 1, 6, false, 5000, 4096, 4096)
	if _, err := d.OpenFile(ctx, 1, 6, 3); rpcMessage(err) != "FILE_PART_SIZE_CHANGED" {
		t.Errorf("a first part larger than the others: %v", err)
	}
	for _, parts := range []int32{-1, 8001, 1<<31 - 1} {
		if _, err := d.OpenFile(ctx, 1, 5, parts); rpcMessage(err) != "FILE_PARTS_INVALID" {
			t.Errorf("%d parts: %v", parts, err)
		}
	}
	upload(t, d, 1, 4, true, 1024, 1024, 10)
	if _, err := d.OpenFile(ctx, 1, 4, 2); rpcMessage(err) != "FILE_PARTS_INVALID" {
		t.Errorf("parts other than the big file's: %v", err)
	}
	if r, err := d.OpenFile(ctx, 1, 4, 3); err != nil || r.Size() != 2058 {
		t.Errorf("the big file: %v", err)
	}
	// Without a count (the loopback server, the download cache): the
	// parts there are.
	upload(t, d, 1, 5, false, 1024, 1024, 7)
	if r, err := d.OpenFile(ctx, 1, 5, 0); err != nil || r.Size() != 2055 {
		t.Errorf("counted parts: %v", err)
	}
}

// TestDropFile: a file stored for good leaves no parts and no info behind;
// another file's stay.
func TestDropFile(t *testing.T) {
	d, mr := newTestDao(t)
	upload(t, d, 1, 7, false, 4)
	upload(t, d, 1, 8, false, 4)
	if err := d.DropFile(context.Background(), 1, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(d.parts.uploadDir(1, 7)); !os.IsNotExist(err) {
		t.Errorf("the parts stayed: %v", err)
	}
	if mr.Exists(getFileInfoKey(1, 7)) {
		t.Error("the info stayed")
	}
	if _, err := os.Stat(d.parts.partPath(1, 8, 0)); err != nil || !mr.Exists(getFileInfoKey(1, 8)) {
		t.Errorf("another file's went: %v", err)
	}
}

// TestSweep: uploads idle longer than the time to live go, with an owner
// left without uploads; a recent one stays.
func TestSweep(t *testing.T) {
	d, _ := newTestDao(t)
	upload(t, d, 1, 7, false, 4)
	upload(t, d, 2, 8, false, 4)
	old := time.Now().Add(-partsTTL - time.Minute)
	if err := os.Chtimes(d.parts.uploadDir(1, 7), old, old); err != nil {
		t.Fatal(err)
	}
	d.parts.sweep(time.Now(), partsTTL)
	if _, err := os.Stat(filepath.Dir(d.parts.uploadDir(1, 7))); !os.IsNotExist(err) {
		t.Errorf("the idle upload stayed: %v", err)
	}
	if _, err := os.Stat(d.parts.partPath(2, 8, 0)); err != nil {
		t.Errorf("the recent upload went: %v", err)
	}
}

// TestPartsDiskFull: below the free space kept, a part is refused and
// nothing is written.
func TestPartsDiskFull(t *testing.T) {
	d, _ := newTestDao(t)
	d.parts.minFree = 1 << 62
	if err := d.WriteFilePartData(context.Background(), 1, 7, 0, []byte("part")); !errors.Is(err, errStorageFull) {
		t.Fatalf("error %v", err)
	}
	if _, err := os.Stat(d.parts.uploadDir(1, 7)); !os.IsNotExist(err) {
		t.Errorf("written anyway: %v", err)
	}
}

// TestReadAllLimit: a file larger than MaxInMemoryFile is never read into
// memory whole.
func TestReadAllLimit(t *testing.T) {
	r := &PartsReader{DfsFileInfo: &model.DfsFileInfo{}, starts: []int64{0, MaxInMemoryFile + 1}}
	if _, err := r.ReadAll(context.Background()); !errors.Is(err, ErrTooLargeForMemory) {
		t.Fatalf("error %v", err)
	}
}

// TestGetCacheFile: a download of a file still being stored comes from its
// parts and never reaches beyond the end of the file, whatever the limit.
func TestGetCacheFile(t *testing.T) {
	d, _ := newTestDao(t)
	ctx := context.Background()
	want := upload(t, d, 1, 7, false, 1024, 100)
	if err := d.SetCacheFileInfo(ctx, 42, &model.DfsFileInfo{Creator: 1, FileId: 7}); err != nil {
		t.Fatal(err)
	}
	fromStorage := func(context.Context, string, int64, int32) ([]byte, error) {
		t.Error("read from object storage")
		return nil, nil
	}
	got, err := d.GetCacheFile(ctx, 42, 1000, 1<<20, fromStorage)
	if err != nil || !bytes.Equal(got, want[1000:]) || cap(got) > len(want) {
		t.Errorf("%d bytes (capacity %d), %v", len(got), cap(got), err)
	}
	if got, err := d.GetCacheFile(ctx, 42, 4096, 4096, fromStorage); err != nil || len(got) != 0 {
		t.Errorf("past the end: %d bytes, %v", len(got), err)
	}
}

// TestFirstPartLater: Android uploads a file still being written with a
// 1 KB part 0, sent last (FileUploadOperation, uploadFirstPartLater): the
// file reads back whole, its size right.
func TestFirstPartLater(t *testing.T) {
	d, _ := newTestDao(t)
	ctx := context.Background()
	for _, sizes := range [][]int{{1024, 4096, 4096, 100}, {1024, 4096}, {1024, 4096, 4096}} {
		id := int64(len(sizes)*10 + sizes[len(sizes)-1])
		want := upload(t, d, 1, id, false, sizes...)
		r, err := d.OpenFile(ctx, 1, id, int32(len(sizes)))
		if err != nil {
			t.Fatalf("%v: %v", sizes, err)
		}
		got, err := io.ReadAll(r)
		if err != nil || !bytes.Equal(got, want) || r.GetFileSize() != int64(len(want)) {
			t.Errorf("%v: %d bytes (file info %d), %v", sizes, len(got), r.GetFileSize(), err)
		}
	}
}

// TestVerifiedLayout: once a send has checked the parts, later readers
// (downloads, the loopback server) take their layout as recorded, without
// looking at every part again; a part sent anew makes them look again.
func TestVerifiedLayout(t *testing.T) {
	d, _ := newTestDao(t)
	ctx := context.Background()
	want := upload(t, d, 1, 7, false, 1024, 1024, 500)
	if _, err := d.OpenFile(ctx, 1, 7, 3); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(d.parts.partPath(1, 7, 2)); err != nil {
		t.Fatal(err)
	}
	r, err := d.OpenFile(ctx, 1, 7, 0)
	if err != nil || r.Size() != int64(len(want)) {
		t.Fatalf("the recorded layout: %v", err)
	}
	if _, err := r.ReadAt(make([]byte, 10), 2100); rpcMessage(err) != "FILE_PART_2_MISSING" {
		t.Errorf("reading a part gone: %v", err)
	}
	if err := d.WriteFilePartData(ctx, 1, 7, 2, want[2048:]); err != nil {
		t.Fatal(err)
	}
	if info, _ := d.GetFileInfo(ctx, 1, 7); info.Verified {
		t.Error("still verified after a part sent anew")
	}
}

type recordedPut struct {
	mu   sync.Mutex
	got  map[string][]byte
	fail bool
}

func (p *recordedPut) put(_ context.Context, kind StoreKind, path string, r io.Reader, size int64) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if int64(len(b)) != size {
		return errors.New("size differs")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fail {
		return errors.New("storage down")
	}
	if p.got == nil {
		p.got = map[string][]byte{}
	}
	p.got[string(kind)+"/"+path] = b
	return nil
}

// TestPersistParts: an upload is stored whole with its size; then
// downloads go to object storage and its record goes. A store asked for
// during shutdown stays recorded and is done at the next start.
func TestPersistParts(t *testing.T) {
	d, _ := newTestDao(t)
	ctx := context.Background()
	puts := &recordedPut{}
	d.persist = newPersister(puts.put)
	want := upload(t, d, 1, 7, false, 1024, 1024, 500)
	r, err := d.OpenFile(ctx, 1, 7, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetCacheFileInfo(ctx, 42, r.DfsFileInfo); err != nil {
		t.Fatal(err)
	}
	d.PersistParts(ctx, StoreDocument, "42.dat", 42, r)
	d.WaitPersisted(ctx)
	if !bytes.Equal(puts.got["documents/42.dat"], want) {
		t.Fatalf("stored %d bytes", len(puts.got["documents/42.dat"]))
	}
	if _, _, err := d.getCacheFileInfo(ctx, 42); err == nil {
		t.Error("downloads still go to the parts")
	}
	if p, _ := d.parts.pendingStores(); len(p) != 0 {
		t.Errorf("records left: %v", p)
	}

	// Shutting down: recorded, not stored; the next start stores it.
	d.PersistParts(ctx, StoreEncrypted, "43.dat", 43, r)
	if _, ok := puts.got["encrypted/43.dat"]; ok {
		t.Fatal("stored after shutdown")
	}
	next := &Dao{ssdb: d.ssdb, parts: d.parts}
	next.persist = newPersister(puts.put)
	next.ResumeStores(ctx)
	next.WaitPersisted(ctx)
	if !bytes.Equal(puts.got["encrypted/43.dat"], want) {
		t.Fatalf("resumed store: %d bytes", len(puts.got["encrypted/43.dat"]))
	}
	if p, _ := next.parts.pendingStores(); len(p) != 0 {
		t.Errorf("records left: %v", p)
	}
}

// TestSweepKeepsPendingStores: the parts of a store not done yet outlive
// the usual time to live, up to pendingTTL.
func TestSweepKeepsPendingStores(t *testing.T) {
	d, _ := newTestDao(t)
	upload(t, d, 1, 7, false, 4)
	if err := d.parts.markStore(pendingStore{Kind: StoreDocument, Path: "42.dat", CacheId: 42, Owner: 1, FileId: 7, Parts: 1}); err != nil {
		t.Fatal(err)
	}
	dir := d.parts.uploadDir(1, 7)
	old := time.Now().Add(-partsTTL - time.Minute)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatal(err)
	}
	d.parts.sweep(time.Now(), partsTTL)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("a pending store's parts went: %v", err)
	}
	older := time.Now().Add(-pendingTTL - time.Minute)
	if err := os.Chtimes(dir, older, older); err != nil {
		t.Fatal(err)
	}
	d.parts.sweep(time.Now(), partsTTL)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("kept past pendingTTL: %v", err)
	}
}
