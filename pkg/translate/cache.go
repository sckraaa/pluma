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

package translate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/teamgram/proto/mtproto"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/proto"
)

// The cache of translations (ADR 0064, section 5): one row of
// translation_cache per translated text, keyed by its content, so that a
// channel post read by many and every forward of a message are translated
// once per language, and an edited message (another text) is another key.
// Changing the model or the profile changes every key: the old rows are
// not served any more and go when their time is up.

const (
	// cacheKeyDomain separates the keys of this cache from any other
	// SHA-256 and versions their layout: a change of what goes into a key
	// is a new domain, never the same domain read differently.
	cacheKeyDomain = "pluma.translate.cache/1"
	// cacheTouchEvery: used_at of a row that is read is moved forward at
	// most this often, so a row read by every subscriber of a channel is
	// not written on every read. The sweep may thus see a row as unused for
	// up to this long more than it was.
	cacheTouchEvery = time.Hour
	// cacheReadBatch bounds the keys of one SELECT ... IN; a request of a
	// client has at most MaxItems (100) items.
	cacheReadBatch = 100
	// cacheSweepBatch bounds the rows one DELETE of the sweep removes, so
	// a large expiry never holds the table in one long statement.
	cacheSweepBatch = 1000
	// CacheSweepEvery is how often the sweeper runs.
	CacheSweepEvery = 10 * time.Minute
	// cacheSweepTimeout bounds one pass of the sweeper.
	cacheSweepTimeout = 5 * time.Minute
	// The widths of translation_cache.to_lang and .model.
	cacheMaxLang  = 16
	cacheMaxModel = 64
)

// Key is translation_cache.cache_key: the SHA-256 of a KeySource.
type Key [sha256.Size]byte

// KeySource is what a translation depends on, and so what its key is
// made of.
type KeySource struct {
	// Profile and ProfileVersion name the prompt templates and sampling
	// parameters (profile.go): a new version of a profile is new keys.
	Profile        string
	ProfileVersion int
	// Model is the name the engine answers to.
	Model string
	// ToLang is the target language as the profile spells it, not as the
	// client sent it: pt-BR and pt_br are one key.
	ToLang string
	Text   string
	// Entities are the entities of Text as they are marked up for the
	// engine: an inputMessageEntityMentionName is already turned into
	// messageEntityMentionName with its user_id, as the answer will carry
	// it. Their order does not matter.
	Entities []*mtproto.MessageEntity
}

// CacheKey returns the key of the translation of s. Every field is written
// with its length, so that no two different sources give one byte string,
// and the entities are sorted, so that their order does not change the key.
func CacheKey(s KeySource) Key {
	var b []byte
	b = appendKeyString(b, cacheKeyDomain)
	b = appendKeyString(b, s.Profile)
	b = binary.AppendVarint(b, int64(s.ProfileVersion))
	b = appendKeyString(b, s.Model)
	b = appendKeyString(b, s.ToLang)
	b = appendKeyString(b, s.Text)
	recs := make([][]byte, 0, len(s.Entities))
	for _, e := range s.Entities {
		if e != nil {
			recs = append(recs, appendKeyEntity(nil, e))
		}
	}
	slices.SortFunc(recs, bytes.Compare)
	b = binary.AppendUvarint(b, uint64(len(recs)))
	for _, r := range recs {
		b = binary.AppendUvarint(b, uint64(len(r)))
		b = append(b, r...)
	}
	return sha256.Sum256(b)
}

func appendKeyString(b []byte, s string) []byte {
	b = binary.AppendUvarint(b, uint64(len(s)))
	return append(b, s...)
}

func appendKeyBool(b []byte, v bool) []byte {
	if v {
		return append(b, 1)
	}
	return append(b, 0)
}

// appendKeyEntity writes every field of an entity of layer 233
// (messageEntity* of third_party/teamgram-proto): two entities that differ
// in anything a client shows are two records. The offset and the length
// come first, so sorted records follow the text.
func appendKeyEntity(b []byte, e *mtproto.MessageEntity) []byte {
	b = binary.AppendVarint(b, int64(e.GetOffset()))
	b = binary.AppendVarint(b, int64(e.GetLength()))
	b = appendKeyString(b, e.GetPredicateName())
	b = appendKeyString(b, e.GetLanguage())
	b = appendKeyString(b, e.GetUrl())
	b = binary.AppendVarint(b, e.GetUserId_INT64())
	if u := e.GetUserId_INPUTUSER(); u != nil {
		b = append(b, 1)
		b = appendKeyString(b, u.GetPredicateName())
		b = binary.AppendVarint(b, u.GetUserId())
	} else {
		b = append(b, 0)
	}
	b = binary.AppendVarint(b, e.GetDocumentId())
	b = appendKeyBool(b, e.GetCollapsed())
	b = appendKeyBool(b, e.GetRelative())
	b = appendKeyBool(b, e.GetShortTime())
	b = appendKeyBool(b, e.GetLongTime())
	b = appendKeyBool(b, e.GetShortDate())
	b = appendKeyBool(b, e.GetLongDate())
	b = appendKeyBool(b, e.GetDayOfWeek())
	b = binary.AppendVarint(b, int64(e.GetDate()))
	return appendKeyString(b, e.GetOldText())
}

// CachedTranslation is a row of translation_cache: the translation of a
// text and the entities of the translation.
type CachedTranslation struct {
	Text     string
	Entities []*mtproto.MessageEntity
	// Markup is translation_cache.markup: MarkupLost when the engine did
	// not keep the tags and the formatting of the text is lost (markup.go).
	Markup Markup
}

func marshalCacheEntities(entities []*mtproto.MessageEntity) ([]byte, error) {
	b, err := proto.Marshal(&mtproto.TextWithEntities{Entities: entities})
	if err != nil {
		return nil, err
	}
	if b == nil {
		// entities is NOT NULL, and the driver writes a nil slice as NULL.
		b = []byte{}
	}
	return b, nil
}

func unmarshalCacheEntities(b []byte) ([]*mtproto.MessageEntity, error) {
	var t mtproto.TextWithEntities
	if err := proto.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	return t.Entities, nil
}

// CachedTranslations returns the translations of keys that are in the
// cache; a key that is not in the map is not. The rows read whose used_at
// is older than cacheTouchEvery are touched, so that the sweep keeps what
// is still read.
func (s *Store) CachedTranslations(ctx context.Context, keys []Key) (map[Key]*CachedTranslation, error) {
	found := make(map[Key]*CachedTranslation, len(keys))
	uniq := make([]Key, 0, len(keys))
	seen := make(map[Key]bool, len(keys))
	for _, k := range keys {
		if !seen[k] {
			seen[k] = true
			uniq = append(uniq, k)
		}
	}
	now := s.now().UnixMilli()
	stale := now - cacheTouchEvery.Milliseconds()
	var touch []Key
	for part := range slices.Chunk(uniq, cacheReadBatch) {
		t, err := s.readCache(ctx, part, stale, found)
		if err != nil {
			return nil, err
		}
		touch = append(touch, t...)
	}
	for part := range slices.Chunk(touch, cacheReadBatch) {
		args := make([]any, 0, 2+len(part))
		args = append(args, now)
		for _, k := range part {
			args = append(args, k[:])
		}
		args = append(args, stale)
		query := "UPDATE translation_cache SET used_at = ? WHERE cache_key IN (" + sqlPlaceholders(len(part)) + ") AND used_at < ?"
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			return nil, fmt.Errorf("translate: touch cached translations: %w", err)
		}
	}
	return found, nil
}

// readCache reads the rows of keys into found and returns the keys of the
// rows last used before stale.
func (s *Store) readCache(ctx context.Context, keys []Key, stale int64, found map[Key]*CachedTranslation) ([]Key, error) {
	args := make([]any, len(keys))
	for i, k := range keys {
		args[i] = k[:]
	}
	query := "SELECT cache_key, `text`, entities, markup, used_at FROM translation_cache WHERE cache_key IN (" + sqlPlaceholders(len(keys)) + ")"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("translate: read cached translations: %w", err)
	}
	defer rows.Close()
	var touch []Key
	for rows.Next() {
		var (
			key      []byte
			text     string
			entities []byte
			markup   int8
			usedAt   int64
		)
		if err = rows.Scan(&key, &text, &entities, &markup, &usedAt); err != nil {
			return nil, fmt.Errorf("translate: read cached translations: %w", err)
		}
		var k Key
		if len(key) != len(k) {
			return nil, fmt.Errorf("translate: cached translation with a key of %d bytes", len(key))
		}
		copy(k[:], key)
		t := &CachedTranslation{Text: text, Markup: Markup(markup)}
		if !t.Markup.valid() {
			return nil, fmt.Errorf("translate: cached translation %x: markup %d", key, markup)
		}
		if t.Entities, err = unmarshalCacheEntities(entities); err != nil {
			return nil, fmt.Errorf("translate: cached translation %x: entities: %w", key, err)
		}
		found[k] = t
		if usedAt < stale {
			touch = append(touch, k)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("translate: read cached translations: %w", err)
	}
	return touch, nil
}

// PutTranslation writes the translation of key into the cache. toLang
// and model are those of the key's source, kept for the operator. When
// the key is already there (two requests of one text finished together,
// or two instances), the translation that was there first stays, so that
// what a client was once shown for a text does not change under it; only
// used_at moves.
func (s *Store) PutTranslation(ctx context.Context, key Key, toLang, model string, t *CachedTranslation) error {
	if t == nil {
		return errors.New("translate: put a nil translation")
	}
	if toLang == "" || len(toLang) > cacheMaxLang {
		return fmt.Errorf("translate: put a translation to %q", toLang)
	}
	if model == "" || len(model) > cacheMaxModel {
		return fmt.Errorf("translate: put a translation of the model %q", model)
	}
	entities, err := marshalCacheEntities(t.Entities)
	if err != nil {
		return fmt.Errorf("translate: put translation %x: entities: %w", key[:], err)
	}
	if !t.Markup.valid() {
		return fmt.Errorf("translate: put translation %x: markup %d", key[:], t.Markup)
	}
	now := s.now().UnixMilli()
	if _, err = s.db.ExecContext(ctx,
		"INSERT INTO translation_cache (cache_key, to_lang, model, `text`, entities, markup, created_at, used_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)"+
			" ON DUPLICATE KEY UPDATE used_at = GREATEST(used_at, VALUES(used_at))",
		key[:], toLang, model, t.Text, entities, t.Markup, now, now); err != nil {
		return fmt.Errorf("translate: put translation %x: %w", key[:], err)
	}
	return nil
}

// SweepTranslations deletes the rows of the cache not used for ttl, in
// batches of cacheSweepBatch, and returns how many went. It stops at the
// first error, keeping what it deleted so far.
func (s *Store) SweepTranslations(ctx context.Context, ttl time.Duration) (int64, error) {
	if ttl <= 0 {
		return 0, fmt.Errorf("translate: sweep the cache with a ttl of %v", ttl)
	}
	cut := s.now().Add(-ttl).UnixMilli()
	var total int64
	for {
		res, err := s.db.ExecContext(ctx, "DELETE FROM translation_cache WHERE used_at < ? ORDER BY used_at LIMIT ?", cut, cacheSweepBatch)
		if err != nil {
			return total, fmt.Errorf("translate: sweep the cache: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, fmt.Errorf("translate: sweep the cache: %w", err)
		}
		total += n
		if n < cacheSweepBatch {
			return total, nil
		}
	}
}

// CacheRows returns the number of rows of the cache (the
// translate_cache_rows gauge).
func (s *Store) CacheRows(ctx context.Context) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM translation_cache").Scan(&n); err != nil {
		return 0, fmt.Errorf("translate: count cached translations: %w", err)
	}
	return n, nil
}

// CacheSweeper deletes the cache rows not used for its ttl, once at the
// start and then every CacheSweepEvery. It holds no state: a restart or a
// second instance only sweeps again.
type CacheSweeper struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// StartCacheSweeper starts sweeping store with ttl (Translate.CacheTTL).
func StartCacheSweeper(store *Store, ttl time.Duration) (*CacheSweeper, error) {
	if ttl <= 0 {
		return nil, fmt.Errorf("translate: cache ttl %v", ttl)
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &CacheSweeper{cancel: cancel}
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		t := time.NewTicker(CacheSweepEvery)
		defer t.Stop()
		for {
			sweepCacheOnce(ctx, store, ttl)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	return w, nil
}

func sweepCacheOnce(ctx context.Context, store *Store, ttl time.Duration) {
	ctx, cancel := context.WithTimeout(ctx, cacheSweepTimeout)
	defer cancel()
	n, err := store.SweepTranslations(ctx, ttl)
	if n > 0 {
		logx.WithContext(ctx).Infof("translate: swept %d cached translations unused for %v", n, ttl)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		// err starts with "translate: sweep the cache".
		logx.WithContext(ctx).Error(err)
	}
}

// Stop stops the sweeper and waits for the pass in progress, which a
// stop cancels.
func (w *CacheSweeper) Stop() {
	w.cancel()
	w.wg.Wait()
}
