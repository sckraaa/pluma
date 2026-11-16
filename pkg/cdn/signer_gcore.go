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
	"crypto/md5" //nolint:gosec // the Secure Token of Gcore is defined over MD5
	"encoding/base64"
	"fmt"
	"strconv"
	"time"
)

// GcoreSigner signs URLs with the Secure Token of a Gcore CDN resource,
// which the edge checks before it serves or fetches anything
// (https://docs.gcore.com/cdn/cdn-resource-options/security/use-a-secure-token/configure-and-use-secure-token.md,
// read 2026-11-16):
//
//	md5 = base64url(MD5(expires + path + " " + key)), without "="
//	url = BaseURL + path + "?md5=" + md5 + "&expires=" + expires
//
// No client address is bound in (mobile networks change it). The query
// string is not part of the cache key ("Ignore query string → Ignore all"
// on the resource), so every client shares the cached object whatever its
// token.
type GcoreSigner struct {
	base string
	key  string
	ttl  int64 // seconds
}

var _ Signer = (*GcoreSigner)(nil)

// Bounds of a Secure Token key: what the field of the portal takes
// (configure-and-use-secure-token.md).
const (
	minGcoreKey = 6
	maxGcoreKey = 32
)

// NewGcoreSigner returns the signer of the resource at baseURL (a checked
// scheme and host, no trailing slash) with the Secure Token key, whose
// URLs work for ttl.
func NewGcoreSigner(baseURL, key string, ttl time.Duration) (*GcoreSigner, error) {
	if err := validGcoreKey(key); err != nil {
		return nil, err
	}
	return &GcoreSigner{base: baseURL, key: key, ttl: ttlSeconds(ttl)}, nil
}

// validGcoreKey checks a Secure Token key: 6 to 32 printable ASCII
// characters, no spaces (a space separates the key in the signed string).
func validGcoreKey(key string) error {
	if len(key) < minGcoreKey || len(key) > maxGcoreKey {
		return fmt.Errorf("cdn: a %s key of %d characters, want %d to %d", SignerGcoreMD5, len(key), minGcoreKey, maxGcoreKey)
	}
	for i := range len(key) {
		if key[i] <= ' ' || key[i] > '~' {
			return fmt.Errorf("cdn: a %s key must be printable ASCII without spaces", SignerGcoreMD5)
		}
	}
	return nil
}

func (s *GcoreSigner) Name() string { return SignerGcoreMD5 }

func (s *GcoreSigner) SignURL(path string, now time.Time) (string, int64, error) {
	exp := now.Unix() + s.ttl
	e := strconv.FormatInt(exp, 10)
	return s.base + path + "?md5=" + gcoreToken(e, path, s.key) + "&expires=" + e, exp, nil
}

// gcoreToken is the md5 parameter of path valid until exp.
func gcoreToken(exp, path, key string) string {
	sum := md5.Sum([]byte(exp + path + " " + key)) //nolint:gosec // the Secure Token of Gcore is defined over MD5
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
