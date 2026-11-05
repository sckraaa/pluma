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
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/teamgram/teamgram-server/app/service/dfs/internal/model"

	"github.com/zeromicro/go-zero/core/logx"
)

// partsTTL is how long the parts of an upload are kept after its last part:
// the file info in Redis lives as long (ssdbExpire), and so do downloads
// served from the parts before the file is in object storage.
const partsTTL = ssdbExpire * time.Second

// pendingTTL is how long the parts of an upload whose store has not
// succeeded are kept for it (PersistParts): stores are retried at every
// start meanwhile.
const pendingTTL = 24 * time.Hour

// storeMarkPrefix names the record of a store in the directory of the
// parts it reads.
const storeMarkPrefix = ".store-"

// sweepEvery is how often the parts older than partsTTL are removed.
const sweepEvery = 10 * time.Minute

// errStorageFull refuses a part when the disk of the parts is short of
// space: the upload fails instead of the disk filling.
var errStorageFull = errors.New("dfs: no space left for upload parts")

type partStore struct {
	dir     string
	minFree int64
}

func newPartStore(c model.PartsConf) *partStore {
	if c.Dir == "" {
		c.Dir = "/data/parts"
	}
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		logx.Must(fmt.Errorf("dfs: parts directory: %w", err))
	}
	return &partStore{dir: c.Dir, minFree: c.MinFreeBytes}
}

func (s *partStore) uploadDir(ownerId, fileId int64) string {
	return filepath.Join(s.dir, strconv.FormatInt(ownerId, 10), strconv.FormatInt(fileId, 10))
}

func (s *partStore) partPath(ownerId, fileId int64, part int) string {
	return filepath.Join(s.uploadDir(ownerId, fileId), strconv.Itoa(part))
}

// write stores one part: in a temporary file renamed into place, so that a
// reader never sees half a part, and a part sent again replaces the old one.
func (s *partStore) write(ownerId, fileId int64, part int32, b []byte) error {
	if err := s.checkFree(); err != nil {
		return err
	}
	dir := s.uploadDir(ownerId, fileId)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return err
	}
	tmp := filepath.Join(dir, "."+strconv.Itoa(int(part))+"."+hex.EncodeToString(suffix[:]))
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, strconv.Itoa(int(part)))); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// The directory's time is the upload's last activity (sweep).
	now := time.Now()
	return os.Chtimes(dir, now, now)
}

func (s *partStore) checkFree() error {
	if s.minFree <= 0 {
		return nil
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(s.dir, &st); err != nil {
		return err
	}
	if free := int64(st.Bavail) * int64(st.Bsize); free < s.minFree {
		return errStorageFull
	}
	return nil
}

// count returns the number of parts present from 0 on, and the size of the
// last of them.
func (s *partStore) count(ownerId, fileId int64) (int, int, error) {
	n, last := 0, 0
	for {
		fi, err := os.Stat(s.partPath(ownerId, fileId, n))
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			return 0, 0, err
		}
		n, last = n+1, int(fi.Size())
	}
	return n, last, nil
}

func (s *partStore) drop(ownerId, fileId int64) error {
	return os.RemoveAll(s.uploadDir(ownerId, fileId))
}

func (s *partStore) markPath(p pendingStore) string {
	return filepath.Join(s.uploadDir(p.Owner, p.FileId), storeMarkPrefix+strconv.FormatInt(p.CacheId, 10))
}

// markStore records the store p next to its parts.
func (s *partStore) markStore(p pendingStore) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	path := s.markPath(p)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// unmarkStore removes the record of p.
func (s *partStore) unmarkStore(p pendingStore) error {
	if err := os.Remove(s.markPath(p)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// pendingStores returns every store recorded.
func (s *partStore) pendingStores() ([]pendingStore, error) {
	marks, err := filepath.Glob(filepath.Join(s.dir, "*", "*", storeMarkPrefix+"*"))
	if err != nil {
		return nil, err
	}
	var out []pendingStore
	for _, m := range marks {
		if strings.HasSuffix(m, ".tmp") {
			continue
		}
		b, err := os.ReadFile(m)
		if err != nil {
			logx.Errorf("dfs: read store record: %v", err)
			continue
		}
		var p pendingStore
		if err := json.Unmarshal(b, &p); err != nil {
			logx.Errorf("dfs: store record %s: %v", filepath.Base(m), err)
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// sweep removes the uploads idle for longer than ttl, those with a store
// not done after pendingTTL, and the owners left without uploads.
func (s *partStore) sweep(now time.Time, ttl time.Duration) {
	owners, err := os.ReadDir(s.dir)
	if err != nil {
		logx.Errorf("dfs: sweep parts: %v", err)
		return
	}
	removed := 0
	for _, o := range owners {
		if !o.IsDir() {
			continue
		}
		odir := filepath.Join(s.dir, o.Name())
		uploads, err := os.ReadDir(odir)
		if err != nil {
			logx.Errorf("dfs: sweep parts: %v", err)
			continue
		}
		left := 0
		for _, u := range uploads {
			fi, err := u.Info()
			if err != nil {
				continue
			}
			udir := filepath.Join(odir, u.Name())
			keep := ttl
			if marks, _ := filepath.Glob(filepath.Join(udir, storeMarkPrefix+"*")); len(marks) > 0 {
				keep = pendingTTL
			}
			if now.Sub(fi.ModTime()) < keep {
				left++
				continue
			}
			if keep == pendingTTL {
				logx.Errorf("dfs: the parts of %s/%s go with their store never done", o.Name(), u.Name())
			}
			if err := os.RemoveAll(udir); err != nil {
				logx.Errorf("dfs: sweep parts: %v", err)
				left++
				continue
			}
			removed++
		}
		if left == 0 {
			// Fails harmlessly if a part arrived in the meantime.
			_ = os.Remove(odir)
		}
	}
	if removed > 0 {
		logx.Infof("dfs: removed the parts of %d idle uploads", removed)
	}
}

// runSweeper sweeps at start and then every sweepEvery until ctx ends.
// Stores are resumed (ResumeStores) before it starts.
func (s *partStore) runSweeper(ctx context.Context) {
	s.sweep(time.Now(), partsTTL)
	t := time.NewTicker(sweepEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			s.sweep(now, partsTTL)
		}
	}
}
