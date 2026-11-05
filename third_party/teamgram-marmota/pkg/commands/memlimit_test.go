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

package commands

import (
	"io/fs"
	"testing"
)

func TestCgroupMemoryLimit(t *testing.T) {
	for _, c := range []struct {
		name  string
		files map[string]string
		want  int64
		ok    bool
	}{
		{"v2 limit", map[string]string{"/sys/fs/cgroup/memory.max": "805306368\n"}, 805306368, true},
		{"v2 none", map[string]string{"/sys/fs/cgroup/memory.max": "max\n"}, 0, false},
		{"v1 limit", map[string]string{"/sys/fs/cgroup/memory/memory.limit_in_bytes": "268435456\n"}, 268435456, true},
		{"v1 none", map[string]string{"/sys/fs/cgroup/memory/memory.limit_in_bytes": "9223372036854771712\n"}, 0, false},
		{"no cgroup", map[string]string{}, 0, false},
		{"garbage", map[string]string{"/sys/fs/cgroup/memory.max": "lots\n"}, 0, false},
	} {
		got, ok := cgroupMemoryLimit(func(name string) ([]byte, error) {
			if s, ok := c.files[name]; ok {
				return []byte(s), nil
			}
			return nil, fs.ErrNotExist
		})
		if got != c.want || ok != c.ok {
			t.Errorf("%s: %d, %v; want %d, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}
