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
	"crypto/rand"
	"encoding/binary"
	"strings"

	"github.com/teamgram/proto/mtproto"
)

const (
	// Limits of what the database and the updates will carry: a client
	// exchanges a 256-byte value and a few library names.
	maxKeyMaterial = 1024
	gAHashSize     = 32 // SHA-256 (core.telegram.org/api/end-to-end/voice-calls)
	maxVersions    = 16
	maxVersionLen  = 32
	maxSignaling   = 64 * 1024
	maxDebugLog    = 64 * 1024
	maxComment     = 1024
)

func positiveId() (int64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	return int64(binary.LittleEndian.Uint64(b[:]) & (1<<63 - 1)), nil
}

// checkProtocol checks what a client said it speaks: the errors
// core.telegram.org/method/phone.requestCall lists for it.
func checkProtocol(p *mtproto.PhoneCallProtocol) error {
	if !p.GetUdpP2P() && !p.GetUdpReflector() {
		return mtproto.ErrCallProtocolFlagsInvalid
	}
	if p.GetMinLayer() <= 0 || p.GetMaxLayer() < p.GetMinLayer() {
		return errProtocolLayer
	}
	if len(p.GetLibraryVersions()) == 0 || len(p.GetLibraryVersions()) > maxVersions {
		return errProtocolLayer
	}
	// The protocol is kept as JSON in a text column: a longer one would be
	// cut by "insert ignore", and the row could not be read back, by the
	// sweep either.
	for _, v := range p.GetLibraryVersions() {
		if v == "" || len(v) > maxVersionLen {
			return errProtocolLayer
		}
	}
	return nil
}

// clip bounds text a client sent to n bytes of valid UTF-8. A utf8mb4 column
// refuses a character cut in half or bytes that are no text, and the client
// would repeat the failed request.
func clip(s string, n int) string {
	if len(s) > n {
		s = s[:n]
	}
	return strings.ToValidUTF8(s, "")
}
