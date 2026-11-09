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

// Package appearance keeps what users upload and choose for the look of their
// chats (task 065): uploaded wallpapers and cloud themes, the saved list of an
// account, and the wallpaper of a chat. The wallpapers and themes everybody
// has come from the catalog (pkg/catalog); this package holds what is on top.
package appearance

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/proto"
)

// Store reads and writes the tables of the package. boxDB and topic serve the
// changes the devices of an account are told of (updatePeerWallpaper,
// updateTheme); a Store that only reads needs neither.
type Store struct {
	db    *sqlx.DB
	boxDB *sql.DB
	topic string
}

// NewStore returns a Store on db, the box connection and the push topic.
func NewStore(db *sqlx.DB, boxDB *sql.DB, topic string) *Store {
	return &Store{db: db, boxDB: boxDB, topic: topic}
}

// NewID is a random positive id of a wallpaper or a theme. Ids of the catalog
// are numbers of this server too, and a random 63-bit one cannot be expected
// to meet them.
func NewID() (int64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	id := int64(binary.BigEndian.Uint64(b[:]) >> 1)
	if id == 0 {
		return NewID()
	}
	return id, nil
}

// NewSlug is a random slug for the deep links of an uploaded wallpaper or of
// a theme whose creator left it to the server: 24 characters of 18 random
// bytes, tried again until they pass the rules of a theme slug (ValidThemeSlug).
func NewSlug() (string, error) {
	for {
		var b [18]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		slug := base64.RawURLEncoding.EncodeToString(b[:])
		if ValidThemeSlug(slug) {
			return slug, nil
		}
	}
}

// ValidThemeSlug follows the rules of the Android theme link editor
// (ThemeSetUrlActivity.checkUrl): 5 to 64 characters of a-z, A-Z, 0-9 and
// '_', not starting or ending with '_', not starting with a digit.
func ValidThemeSlug(slug string) bool {
	if len(slug) < 5 || len(slug) > 64 || strings.HasPrefix(slug, "_") || strings.HasSuffix(slug, "_") {
		return false
	}
	for i, ch := range []byte(slug) {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch == '_':
		case ch >= '0' && ch <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// CalcHash folds v into hash as core.telegram.org/api/offsets#hash-generation
// describes; Android counts the hash of a wallpaper list the same way
// (MediaDataController.calcHash, WallpapersListActivity).
func CalcHash(hash, v int64) int64 {
	h := uint64(hash)
	h ^= h >> 21
	h ^= h << 35
	h ^= h >> 4
	return int64(h + uint64(v))
}

func marshal(m proto.Message) ([]byte, error) {
	data, err := proto.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("appearance: marshal: %w", err)
	}
	return data, nil
}

func unmarshalWallPaper(data []byte) (*mtproto.WallPaper, error) {
	if len(data) == 0 {
		return nil, nil
	}
	w := &mtproto.WallPaper{}
	if err := proto.Unmarshal(data, w); err != nil {
		return nil, fmt.Errorf("appearance: unmarshal a wallpaper: %w", err)
	}
	return w, nil
}
