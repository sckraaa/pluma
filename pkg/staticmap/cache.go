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

package staticmap

import (
	"bytes"
	"context"
	"encoding/binary"
	"time"

	"github.com/zeromicro/go-zero/core/stores/kv"
)

const cacheKeyPrefix = "webmap:"

// pngSignature starts every PNG file.
var pngSignature = []byte("\x89PNG\r\n\x1a\n")

// mapCache keeps rendered maps in Redis, so that every part of one file
// (upload.getWebFile with offset/limit) and every viewer of one message get
// the same bytes: tdesktop cancels a download whose parts report different
// sizes (storage/file_download_mtproto.cpp:170-181). An entry is the mtime
// (4 bytes, big endian) followed by the PNG, so the mtime is the same in
// every part as well.
type mapCache struct {
	kv  kv.Store
	ttl time.Duration
}

// get returns the map under key; ok is false when there is none. A damaged
// entry counts as none.
func (c *mapCache) get(ctx context.Context, key string) (b *Blob, ok bool, err error) {
	v, err := c.kv.GetCtx(ctx, cacheKeyPrefix+key)
	if err != nil {
		return nil, false, err
	}
	raw := []byte(v)
	if len(raw) < 4+len(pngSignature) || !bytes.HasPrefix(raw[4:], pngSignature) {
		return nil, false, nil
	}
	return &Blob{Bytes: raw[4:], Mtime: int32(binary.BigEndian.Uint32(raw[:4]))}, true, nil
}

// put stores b under key for the cache TTL.
func (c *mapCache) put(ctx context.Context, key string, b *Blob) error {
	raw := make([]byte, 4+len(b.Bytes))
	binary.BigEndian.PutUint32(raw, uint32(b.Mtime))
	copy(raw[4:], b.Bytes)
	return c.kv.SetexCtx(ctx, cacheKeyPrefix+key, string(raw), int(c.ttl/time.Second))
}
