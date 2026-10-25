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

// Package authkeys holds the rules of MTProto auth keys that gnetway and
// authsession share (task 008).
package authkeys

// MaxTempExpiresIn is the longest a temporary key works, in seconds. A
// longer expires_in of p_q_inner_data_temp is cut to it: perfect forward
// secrecy is only as good as the lifetime of temporary keys, and the
// official clients ask for a day.
const MaxTempExpiresIn = 7 * 24 * 60 * 60

// TempExpiresIn returns how long a temporary key a client asked expiresIn
// seconds for works: at most MaxTempExpiresIn. A non-positive expiresIn is
// returned as it is; the handshake refuses it.
func TempExpiresIn(expiresIn int32) int32 {
	if expiresIn > MaxTempExpiresIn {
		return MaxTempExpiresIn
	}
	return expiresIn
}
