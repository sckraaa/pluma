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

// pluma: the Go memory limit follows the container's (docker-compose.yaml).

import (
	"os"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/zeromicro/go-zero/core/logx"
)

// memLimitRatio is the share of the container's memory limit given to the
// Go runtime: the rest is for what the runtime does not count (ffmpeg and
// other children of dfs share the cgroup) and for the collector to catch up.
const memLimitRatio = 0.8

// cgroup files with the memory limit of the process's own cgroup: v2, then v1.
var cgroupMemoryFiles = []string{
	"/sys/fs/cgroup/memory.max",
	"/sys/fs/cgroup/memory/memory.limit_in_bytes",
}

// unlimitedV1 is about what cgroup v1 reports for no limit (a page-aligned
// maximum int64); a real limit is far below it.
const unlimitedV1 = int64(1) << 60

// cgroupMemoryLimit returns the memory limit of the cgroup the process runs
// in, read through readFile, and whether there is one.
func cgroupMemoryLimit(readFile func(string) ([]byte, error)) (int64, bool) {
	for _, name := range cgroupMemoryFiles {
		b, err := readFile(name)
		if err != nil {
			continue
		}
		s := strings.TrimSpace(string(b))
		if s == "max" {
			return 0, false
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n <= 0 || n >= unlimitedV1 {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// setMemoryLimit gives the Go runtime a soft memory limit below the
// container's, so that the collector works harder as the process nears it
// instead of the kernel killing it there. GOMEMLIMIT, when set, wins: the
// runtime has read it already.
func setMemoryLimit() {
	if os.Getenv("GOMEMLIMIT") != "" {
		return
	}
	limit, ok := cgroupMemoryLimit(os.ReadFile)
	if !ok {
		return
	}
	soft := int64(float64(limit) * memLimitRatio)
	debug.SetMemoryLimit(soft)
	logx.Infof("memory limit %d MiB of the container's %d MiB", soft>>20, limit>>20)
}
