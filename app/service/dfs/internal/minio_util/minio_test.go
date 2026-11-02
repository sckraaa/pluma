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

package minio_util

import (
	"testing"

	"github.com/minio/minio-go/v7"
)

// A put of unknown size holds one part in memory: 16 MiB, not the part of a
// 5 TiB object.
func TestPutPartSize(t *testing.T) {
	_, part, _, err := minio.OptimalPartInfo(-1, s3PutOptions(false, "binary/octet-stream").PartSize)
	if err != nil || part != 16<<20 {
		t.Fatalf("a part of %d bytes, %v", part, err)
	}
}
