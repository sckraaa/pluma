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

// Package mediaerr is the answer to a client whose uploaded media could
// not be stored.
package mediaerr

import (
	"strings"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/grpc/status"
)

// ForClient is what a client is answered for err of storing an upload: an
// error of its parts as dfs found it (FILE_PART_X_MISSING,
// FILE_PART_SIZE_CHANGED, FILE_PARTS_INVALID: core.telegram.org/api/files),
// which tells the client what to send again; anything else MEDIA_INVALID.
func ForClient(err error) error {
	if s, ok := status.FromError(err); ok && s.Code() == mtproto.ErrBadRequest && strings.HasPrefix(s.Message(), "FILE_PART") {
		return err
	}
	return mtproto.ErrMediaInvalid
}
