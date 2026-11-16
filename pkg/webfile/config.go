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

package webfile

import (
	"github.com/teamgram/proto/mtproto"
)

// Config is WebFile of bff.yaml.
type Config struct {
	// HMACKey signs the addresses of webDocument, ${WEBFILE_HMAC_KEY}
	// (Key). Empty: no address is proxied, inline bots' thumbnails go out
	// as webDocumentNoProxy, which clients fetch themselves.
	HMACKey string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// CacheDir is where the files fetched are kept, DefaultCacheDir if
	// empty.
	CacheDir string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// CacheBytes bounds the files kept, DefaultCacheBytes if 0.
	CacheBytes int64 `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// New returns the Server of c with the sources of this package: the
// addresses inline bots name (inputWebFileLocation) when c has a key. Other
// sources (the maps of task 100) are registered on it after.
func New(c Config) (*Server, error) {
	key, err := Key(c.HMACKey)
	if err != nil {
		return nil, err
	}
	if key == nil {
		return NewServer(nil), nil
	}
	dir, size := c.CacheDir, c.CacheBytes
	if dir == "" {
		dir = DefaultCacheDir
	}
	if size == 0 {
		size = DefaultCacheBytes
	}
	cache, err := NewDiskCache(dir, size, DefaultCacheTTL)
	if err != nil {
		return nil, err
	}
	s := NewServer(cache)
	s.Register(mtproto.Predicate_inputWebFileLocation, NewURLSource(key, NewFetcher()))
	return s, nil
}
