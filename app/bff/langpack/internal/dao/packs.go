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
	"container/list"
	"context"
	"fmt"
	"sync"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/langpack"

	"github.com/zeromicro/go-zero/core/syncx"
)

// maxCachedPacks bounds the whole packs kept built in memory: one is
// 0.3-1.5 MB (an Android pack has ~14000 strings), and the clients of a
// server use a few packs and languages.
const maxCachedPacks = 32

// FullPack returns the whole pack of lang (langpack.getLangPack,
// langpack.getDifference from 0) at the version stored now. The answer is
// built once per pack, language and version and shared: it must not be
// changed.
func (d *Dao) FullPack(ctx context.Context, pack, lang string) (*mtproto.LangPackDifference, error) {
	l, err := d.Language(ctx, pack, lang)
	if err != nil {
		return nil, err
	}
	if p := d.packs.Get(pack, lang, l.Version); p != nil {
		return p, nil
	}
	return d.packs.Build(pack, lang, func() (*mtproto.LangPackDifference, int32, error) {
		l, strs, err := d.Read(ctx, pack, lang, 0)
		if err != nil {
			return nil, 0, err
		}
		return langpack.Difference(l, strs, 0, true), l.Version, nil
	})
}

// PackCache keeps the latest built whole pack of each pack and language,
// the least recently used ones going first beyond its size.
type PackCache struct {
	mu      sync.Mutex
	max     int
	order   *list.List // of *cachedPack, the most recently used first
	entries map[string]*list.Element
	builds  syncx.SingleFlight
}

type cachedPack struct {
	key     string
	version int32
	diff    *mtproto.LangPackDifference
}

// NewPackCache returns a cache of up to max packs.
func NewPackCache(max int) *PackCache {
	return &PackCache{max: max, order: list.New(), entries: map[string]*list.Element{}, builds: syncx.NewSingleFlight()}
}

func cacheKey(pack, lang string) string {
	return pack + "/" + lang
}

// Get returns the cached pack of lang at version, or nil.
func (c *PackCache) Get(pack, lang string, version int32) *mtproto.LangPackDifference {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entries[cacheKey(pack, lang)]
	if e == nil || e.Value.(*cachedPack).version != version {
		return nil
	}
	c.order.MoveToFront(e)
	return e.Value.(*cachedPack).diff
}

// Build builds the pack of lang with build, once for concurrent callers,
// and keeps it unless a newer version is cached already.
func (c *PackCache) Build(pack, lang string, build func() (*mtproto.LangPackDifference, int32, error)) (*mtproto.LangPackDifference, error) {
	key := cacheKey(pack, lang)
	v, err := c.builds.Do(key, func() (any, error) {
		diff, version, err := build()
		if err != nil {
			return nil, err
		}
		c.put(key, version, diff)
		return diff, nil
	})
	if err != nil {
		return nil, err
	}
	diff, ok := v.(*mtproto.LangPackDifference)
	if !ok {
		return nil, fmt.Errorf("langpack: cached %T", v)
	}
	return diff, nil
}

func (c *PackCache) put(key string, version int32, diff *mtproto.LangPackDifference) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.entries[key]; e != nil {
		p := e.Value.(*cachedPack)
		if p.version <= version {
			p.version, p.diff = version, diff
		}
		c.order.MoveToFront(e)
		return
	}
	c.entries[key] = c.order.PushFront(&cachedPack{key: key, version: version, diff: diff})
	for c.order.Len() > c.max {
		last := c.order.Back()
		c.order.Remove(last)
		delete(c.entries, last.Value.(*cachedPack).key)
	}
}

// Len is the number of cached packs.
func (c *PackCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}
