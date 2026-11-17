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
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// PathHMACSigner signs URLs in their path, for a CDN whose own token cannot
// be used (ADR 0071 §7, task 107 "Подписи"); the home origin checks the
// signature (Verify), so the CDN only caches by the full path:
//
//	bucket = ceil((now + ttl) / ttl) * ttl          (unix seconds)
//	sig    = base64url(HMAC-SHA256(key, object + "|" + bucket))[:22]
//	url    = BaseURL + "/t/" + bucket + "/" + sig + "/" + object
//
// object is the path of the object without its leading slash ("o/<hex>",
// "c/v1"). Every client in one window of ttl gets the same URL and shares
// the cached object; a URL works from ttl to 2·ttl. 22 characters of the
// signature are 132 bits.
type PathHMACSigner struct {
	base string
	key  []byte
	ttl  int64 // seconds
}

var _ Signer = (*PathHMACSigner)(nil)

const (
	// PathHMACKeySize is the size of a path-hmac key.
	PathHMACKeySize = 32
	// pathSigLen is the length of the signature in the path.
	pathSigLen = 22
	// maxBucketDigits bounds the time in the path: unix seconds fit in 10
	// digits until 2286.
	maxBucketDigits = 10
)

// NewPathHMACSigner returns the signer of baseURL (a checked scheme and
// host, no trailing slash) with key (PathHMACKeySize bytes) whose URLs work
// from ttl to 2·ttl.
func NewPathHMACSigner(baseURL string, key []byte, ttl time.Duration) (*PathHMACSigner, error) {
	if len(key) != PathHMACKeySize {
		return nil, fmt.Errorf("cdn: a %s key of %d bytes, want %d", SignerPathHMAC, len(key), PathHMACKeySize)
	}
	return &PathHMACSigner{base: baseURL, key: append([]byte(nil), key...), ttl: ttlSeconds(ttl)}, nil
}

// ParsePathHMACKey decodes a path-hmac key from the config: standard
// base64, padded or not (openssl rand -base64 32).
func ParsePathHMACKey(s string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		key, err = base64.RawStdEncoding.DecodeString(s)
	}
	if err != nil || len(key) != PathHMACKeySize {
		return nil, fmt.Errorf("cdn: a %s key must be %d bytes in base64", SignerPathHMAC, PathHMACKeySize)
	}
	return key, nil
}

func (s *PathHMACSigner) Name() string { return SignerPathHMAC }

func (s *PathHMACSigner) SignURL(path string, now time.Time) (string, int64, error) {
	object, ok := strings.CutPrefix(path, "/")
	if !ok || object == "" {
		return "", 0, fmt.Errorf("cdn: object path %q does not start with a slash", path)
	}
	bucket := s.bucket(now)
	b := strconv.FormatInt(bucket, 10)
	return s.base + "/t/" + b + "/" + s.sig(object, b) + "/" + object, bucket, nil
}

// bucket is the end of the window of now: the first multiple of ttl at or
// after now+ttl.
func (s *PathHMACSigner) bucket(now time.Time) int64 {
	t := now.Unix() + s.ttl
	return (t + s.ttl - 1) / s.ttl * s.ttl
}

func (s *PathHMACSigner) sig(object, bucket string) string {
	m := hmac.New(sha256.New, s.key)
	_, _ = m.Write([]byte(object + "|" + bucket)) // never fails
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))[:pathSigLen]
}

// Verify checks the path of a request to the origin made by SignURL
// ("/t/<bucket>/<sig>/<object>") at now, and returns the path of the object
// ("/" + object) and the bucket. The signature is compared in constant time
// and checked first: ErrURLExpired (410) only for a URL the home made whose
// time passed, ErrURLInvalid (403) for anything else, a bucket more than
// 2·ttl ahead among them. Which objects exist is the origin's business.
func (s *PathHMACSigner) Verify(path string, now time.Time) (object string, bucket int64, err error) {
	rest, ok := strings.CutPrefix(path, "/t/")
	if !ok {
		return "", 0, ErrURLInvalid
	}
	b, rest, ok := strings.Cut(rest, "/")
	if !ok || b == "" || len(b) > maxBucketDigits {
		return "", 0, ErrURLInvalid
	}
	bucket, err = strconv.ParseInt(b, 10, 64)
	// The decimal form is the one signed: no leading zeros or signs.
	if err != nil || bucket <= 0 || strconv.FormatInt(bucket, 10) != b {
		return "", 0, ErrURLInvalid
	}
	sig, obj, ok := strings.Cut(rest, "/")
	if !ok || len(sig) != pathSigLen || obj == "" {
		return "", 0, ErrURLInvalid
	}
	if subtle.ConstantTimeCompare([]byte(sig), []byte(s.sig(obj, b))) != 1 {
		return "", 0, ErrURLInvalid
	}
	n := now.Unix()
	switch {
	case bucket < n:
		return "", 0, ErrURLExpired
	case bucket-n > 2*s.ttl:
		return "", 0, ErrURLInvalid
	}
	return "/" + obj, bucket, nil
}
