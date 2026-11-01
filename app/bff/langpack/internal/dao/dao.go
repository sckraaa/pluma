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

package dao

import (
	"context"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/bff/langpack/internal/config"
	"github.com/teamgram/teamgram-server/pkg/langpack"
)

// Store is what the service reads of the language packs (langpack.Store).
type Store interface {
	PackExists(ctx context.Context, pack string) (bool, error)
	Languages(ctx context.Context, pack string) ([]langpack.Language, error)
	Language(ctx context.Context, pack, lang string) (*langpack.Language, error)
	Read(ctx context.Context, pack, lang string, from int32) (*langpack.Language, []langpack.String, error)
	StringsByKey(ctx context.Context, pack, lang string, keys []string) (map[string]*langpack.String, error)
}

type Dao struct {
	Store
	packs *PackCache
}

func New(c config.Config) *Dao {
	return NewWithStore(langpack.NewStore(sqlx.NewMySQL(&c.Mysql)))
}

// NewWithStore returns a Dao on store.
func NewWithStore(store Store) *Dao {
	return &Dao{Store: store, packs: NewPackCache(maxCachedPacks)}
}
