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
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/teamgram/marmota/pkg/stores/kv"
	"github.com/teamgram/teamgram-server/app/service/dfs/internal/model"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// A file stored for good leaves no parts and no info behind; another
// file's stay.
func TestDropFile(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	d := &Dao{ssdb: kv.NewStore(kv.KvConf{cache.NodeConf{
		RedisConf: redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType, DisableIdentity: true},
		Weight:    100,
	}})}
	for _, id := range []int64{7, 8} {
		if err := d.WriteFilePartData(ctx, 1, id, 0, []byte("part")); err != nil {
			t.Fatal(err)
		}
		if err := d.SetFileInfo(ctx, &model.DfsFileInfo{Creator: 1, FileId: id, FileTotalParts: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.DropFile(ctx, 1, 7); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{getFileKey(1, 7), getFileInfoKey(1, 7)} {
		if mr.Exists(k) {
			t.Errorf("%s stayed", k)
		}
	}
	for _, k := range []string{getFileKey(1, 8), getFileInfoKey(1, 8)} {
		if !mr.Exists(k) {
			t.Errorf("%s of another file went", k)
		}
	}
}
