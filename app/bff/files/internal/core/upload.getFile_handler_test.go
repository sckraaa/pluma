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

package core

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// TestCheckFileRange: the offsets and limits of upload.getFile, as
// core.telegram.org/api/files sets them.
func TestCheckFileRange(t *testing.T) {
	for _, c := range []struct {
		offset  int64
		limit   int32
		precise bool
		want    error
	}{
		{0, 1 << 20, false, nil},
		{1 << 20, 512 << 10, false, nil},
		{512 << 10, 512 << 10, false, nil},
		{3 << 20, 4096, false, nil},
		{0, 0, false, mtproto.ErrLimitInvalid},
		{0, -1, false, mtproto.ErrLimitInvalid},
		{0, 2 << 20, false, mtproto.ErrLimitInvalid},
		{0, 1<<31 - 1, false, mtproto.ErrLimitInvalid},
		{0, 12288, false, mtproto.ErrLimitInvalid}, // 1 MB is not divisible by 12 KB
		{0, 5000, false, mtproto.ErrLimitInvalid},
		{1024, 4096, false, mtproto.ErrOffsetInvalid},
		{-4096, 4096, false, mtproto.ErrOffsetInvalid},
		{768 << 10, 512 << 10, false, mtproto.ErrLimitInvalid}, // crosses the 1 MB chunk
		{1024, 3072, true, nil},
		{(1 << 20) - 3072, 3072, true, nil},
		{(1 << 20) - 1024, 3072, true, mtproto.ErrLimitInvalid},
		{512, 1024, true, mtproto.ErrOffsetInvalid},
		{0, 1000, true, mtproto.ErrLimitInvalid},
		{0, (1 << 20) + 1024, true, mtproto.ErrLimitInvalid},
	} {
		if got := checkFileRange(c.offset, c.limit, c.precise); got != c.want {
			t.Errorf("offset %d, limit %d, precise %v: %v, want %v", c.offset, c.limit, c.precise, got, c.want)
		}
	}
}
