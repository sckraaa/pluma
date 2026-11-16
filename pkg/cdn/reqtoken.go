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

package cdn

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"time"
)

// The request_token of upload.cdnFileReuploadNeeded (ADR 0065, p. 4): the
// node signs the file_token it was asked for and does not have, the home
// checks the signature in upload.reuploadCdnFile. A token is
//
//	version (1 byte) ‖ time (unix ms, 8 bytes big-endian) ‖
//	the first 16 bytes of HMAC-SHA256(key, version ‖ time ‖ file_token)
//
// with a key the home makes and hands to the node (cdn_nodes.rt_key).
const (
	reqTokenVersion = 1
	reqTokenMACSize = 16
	// ReqTokenSize is the size of a request_token.
	ReqTokenSize = 1 + 8 + reqTokenMACSize

	// ReqTokenTTL is how long a request_token is accepted.
	ReqTokenTTL = 10 * time.Minute
	// reqTokenSkew is how far in the future a request_token may be: the
	// clocks of the home and the node differ a little.
	reqTokenSkew = time.Minute
)

// SignRequestToken returns the request_token of fileToken made at now with
// key.
func SignRequestToken(key, fileToken []byte, now time.Time) []byte {
	t := make([]byte, ReqTokenSize)
	t[0] = reqTokenVersion
	binary.BigEndian.PutUint64(t[1:9], uint64(now.UnixMilli()))
	copy(t[9:], reqTokenMAC(key, t[:9], fileToken))
	return t
}

// VerifyRequestToken reports whether token is a request_token of fileToken
// signed with one of keys (the key in force and the one before; empty keys
// are skipped) not more than ReqTokenTTL before now and not more than a
// minute after it.
func VerifyRequestToken(keys [][]byte, fileToken, token []byte, now time.Time) bool {
	if len(token) != ReqTokenSize || token[0] != reqTokenVersion {
		return false
	}
	at := int64(binary.BigEndian.Uint64(token[1:9]))
	if at < 0 {
		return false
	}
	age := now.UnixMilli() - at
	if age > ReqTokenTTL.Milliseconds() || age < -reqTokenSkew.Milliseconds() {
		return false
	}
	ok := false
	for _, key := range keys {
		if len(key) == 0 {
			continue
		}
		// Every key is tried: the time taken does not tell which one signed.
		if hmac.Equal(token[9:], reqTokenMAC(key, token[:9], fileToken)) {
			ok = true
		}
	}
	return ok
}

func reqTokenMAC(key, head, fileToken []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(head)
	m.Write(fileToken)
	return m.Sum(nil)[:reqTokenMACSize]
}
