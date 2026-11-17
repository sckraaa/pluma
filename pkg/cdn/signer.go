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
	"encoding/hex"
	"errors"
	"time"
)

// Signer makes the URLs a delivery serves objects by (ADR 0071 §7): the
// address of the object with what proves the home handed it out, valid
// for a while. Implementations are safe for concurrent use.
type Signer interface {
	// Name is the name of the signer in the config (Signer of a delivery).
	Name() string
	// SignURL returns the URL of the object at path (ObjectPath or
	// CanaryPath) and the unix second it stops working.
	SignURL(path string, now time.Time) (url string, exp int64, err error)
}

// The signers of the deliveries.
const (
	// SignerGcoreMD5 is the Secure Token of Gcore (signer_gcore.go).
	SignerGcoreMD5 = "gcore-md5"
	// SignerPathHMAC is a signature in the path the home origin checks
	// itself (signer_pathhmac.go), for a CDN without a token of its own.
	SignerPathHMAC = "path-hmac"
)

// CanaryPath is the path of the canary of the origin: a fixed object every
// delivery serves, which the checks of its health fetch.
const CanaryPath = "/c/v1"

// ObjectPath is the path of the cipher of the file whose cdn_files.file_token
// is id: 64 lowercase hex digits under /o/.
func ObjectPath(id []byte) string { return "/o/" + hex.EncodeToString(id) }

// Errors of a signature the origin checks (PathHMACSigner.Verify).
var (
	// ErrURLExpired is a valid signature whose time is past: 410.
	ErrURLExpired = errors.New("cdn: signed URL expired")
	// ErrURLInvalid is any other refused signature: 403.
	ErrURLInvalid = errors.New("cdn: signed URL invalid")
)

// ttlSeconds is ttl in whole seconds, at least one.
func ttlSeconds(ttl time.Duration) int64 { return max(int64(ttl/time.Second), 1) }
