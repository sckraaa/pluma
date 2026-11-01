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

package langpack

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
)

// ErrNotFound is returned for a pack or language that is not stored.
var ErrNotFound = errors.New("langpack: not found")

// batchSize bounds the rows of one INSERT or UPDATE of an import: a
// pluralized string has 12 texts, a pack has up to ~15000 strings.
const batchSize = 500

// Store reads and imports the language packs in MySQL.
type Store struct {
	db  *sqlx.DB
	now func() time.Time
}

// NewStore returns a Store on db.
func NewStore(db *sqlx.DB) *Store {
	return &Store{db: db, now: time.Now}
}

const languageColumns = "lang_pack, lang_code, name, native_name, plural_code, base_lang_code, official, rtl, beta," +
	" strings_count, translated_count, translations_url, sort_order, version, upstream_version, updated_at"

type languageRow struct {
	LangPack        string         `db:"lang_pack"`
	LangCode        string         `db:"lang_code"`
	Name            string         `db:"name"`
	NativeName      string         `db:"native_name"`
	PluralCode      string         `db:"plural_code"`
	BaseLangCode    sql.NullString `db:"base_lang_code"`
	Official        bool           `db:"official"`
	RTL             bool           `db:"rtl"`
	Beta            bool           `db:"beta"`
	StringsCount    int32          `db:"strings_count"`
	TranslatedCount int32          `db:"translated_count"`
	TranslationsURL string         `db:"translations_url"`
	SortOrder       int32          `db:"sort_order"`
	Version         int32          `db:"version"`
	UpstreamVersion int32          `db:"upstream_version"`
	UpdatedAt       int64          `db:"updated_at"`
}

func (r *languageRow) language() Language {
	return Language{
		LangPack:        r.LangPack,
		LangCode:        r.LangCode,
		Name:            r.Name,
		NativeName:      r.NativeName,
		PluralCode:      r.PluralCode,
		BaseLangCode:    r.BaseLangCode.String,
		Official:        r.Official,
		RTL:             r.RTL,
		Beta:            r.Beta,
		StringsCount:    r.StringsCount,
		TranslatedCount: r.TranslatedCount,
		TranslationsURL: r.TranslationsURL,
		SortOrder:       r.SortOrder,
		Version:         r.Version,
		UpstreamVersion: r.UpstreamVersion,
		UpdatedAt:       r.UpdatedAt,
	}
}

const stringColumns = "str_key, pluralized, value, zero_value, one_value, two_value, few_value, many_value, deleted, version"

const upstreamColumns = "upstream_value, upstream_zero_value, upstream_one_value, upstream_two_value, upstream_few_value, upstream_many_value"

type stringRow struct {
	Key        string         `db:"str_key"`
	Pluralized bool           `db:"pluralized"`
	Value      string         `db:"value"`
	Zero       sql.NullString `db:"zero_value"`
	One        sql.NullString `db:"one_value"`
	Two        sql.NullString `db:"two_value"`
	Few        sql.NullString `db:"few_value"`
	Many       sql.NullString `db:"many_value"`
	Deleted    bool           `db:"deleted"`
	Version    int32          `db:"version"`
}

func (r *stringRow) string() String {
	return String{
		Key:        r.Key,
		Pluralized: r.Pluralized,
		Value:      r.Value,
		Zero:       fromNull(r.Zero),
		One:        fromNull(r.One),
		Two:        fromNull(r.Two),
		Few:        fromNull(r.Few),
		Many:       fromNull(r.Many),
		Deleted:    r.Deleted,
		Version:    r.Version,
	}
}

type importRow struct {
	stringRow
	UpValue string         `db:"upstream_value"`
	UpZero  sql.NullString `db:"upstream_zero_value"`
	UpOne   sql.NullString `db:"upstream_one_value"`
	UpTwo   sql.NullString `db:"upstream_two_value"`
	UpFew   sql.NullString `db:"upstream_few_value"`
	UpMany  sql.NullString `db:"upstream_many_value"`
}

func (r *importRow) upstream() String {
	return String{
		Key:        r.Key,
		Pluralized: r.Pluralized,
		Value:      r.UpValue,
		Zero:       fromNull(r.UpZero),
		One:        fromNull(r.UpOne),
		Two:        fromNull(r.UpTwo),
		Few:        fromNull(r.UpFew),
		Many:       fromNull(r.UpMany),
	}
}

func fromNull(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func toNull(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// PackExists reports whether any language of pack is stored.
func (s *Store) PackExists(ctx context.Context, pack string) (bool, error) {
	var n int64
	if err := s.db.QueryRow(ctx, &n, "SELECT COUNT(*) FROM langpack_languages WHERE lang_pack = ? AND version > 0", pack); err != nil {
		return false, err
	}
	return n > 0, nil
}

// Languages returns the languages of pack in Telegram's order; with pack
// empty, those of every pack.
func (s *Store) Languages(ctx context.Context, pack string) ([]Language, error) {
	var rows []languageRow
	var err error
	if pack == "" {
		err = s.db.QueryRowsPartial(ctx, &rows,
			"SELECT "+languageColumns+" FROM langpack_languages WHERE version > 0 ORDER BY lang_pack, sort_order, lang_code")
	} else {
		err = s.db.QueryRowsPartial(ctx, &rows,
			"SELECT "+languageColumns+" FROM langpack_languages WHERE lang_pack = ? AND version > 0 ORDER BY sort_order, lang_code", pack)
	}
	if err != nil {
		return nil, err
	}
	out := make([]Language, len(rows))
	for i := range rows {
		out[i] = rows[i].language()
	}
	return out, nil
}

// Language returns one language of pack, or ErrNotFound.
func (s *Store) Language(ctx context.Context, pack, lang string) (*Language, error) {
	l, err := s.findLanguage(ctx, pack, lang)
	if err == nil && l == nil {
		err = ErrNotFound
	}
	return l, err
}

// findLanguage returns one language of pack, nil when it is not stored.
func (s *Store) findLanguage(ctx context.Context, pack, lang string) (*Language, error) {
	var rows []languageRow
	if err := s.db.QueryRowsPartial(ctx, &rows,
		"SELECT "+languageColumns+" FROM langpack_languages WHERE lang_pack = ? AND lang_code = ? AND version > 0", pack, lang); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	l := rows[0].language()
	return &l, nil
}

// Strings returns the strings of pack in lang with a version above from,
// tombstones included, ordered by key; with from <= 0 every string that is
// not deleted.
func (s *Store) Strings(ctx context.Context, pack, lang string, from int32) ([]String, error) {
	var rows []stringRow
	q, args := stringsQuery(pack, lang, from)
	if err := s.db.QueryRowsPartial(ctx, &rows, q, args...); err != nil {
		return nil, err
	}
	return toStrings(rows), nil
}

// stringsQuery is the query of Strings.
func stringsQuery(pack, lang string, from int32) (string, []any) {
	if from <= 0 {
		return "SELECT " + stringColumns + " FROM langpack_strings WHERE lang_pack = ? AND lang_code = ? AND deleted = 0 ORDER BY str_key",
			[]any{pack, lang}
	}
	return "SELECT " + stringColumns + " FROM langpack_strings WHERE lang_pack = ? AND lang_code = ? AND version > ? ORDER BY str_key",
		[]any{pack, lang, from}
}

// Read returns a language of pack and its strings with a version above
// from (Strings), read in one transaction so that the strings belong to the
// version returned; ErrNotFound when the language is not stored.
func (s *Store) Read(ctx context.Context, pack, lang string, from int32) (*Language, []String, error) {
	var (
		l    *Language
		strs []String
	)
	err := s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		var langs []languageRow
		if err := tx.QueryRowsPartial(&langs,
			"SELECT "+languageColumns+" FROM langpack_languages WHERE lang_pack = ? AND lang_code = ? AND version > 0", pack, lang); err != nil {
			return err
		}
		if len(langs) == 0 {
			return ErrNotFound
		}
		v := langs[0].language()
		l = &v
		var rows []stringRow
		q, args := stringsQuery(pack, lang, from)
		err := tx.QueryRowsPartial(&rows, q, args...)
		strs = toStrings(rows)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return l, strs, nil
}

// ConfigVersions is what help.getConfig says of a session's language pack
// (flags.2 of config): the client's language and the versions
// langpack.getDifference answers with for it and for its base language.
type ConfigVersions struct {
	SuggestedLangCode   string
	LangPackVersion     int32
	BaseLangPackVersion int32 // 0 without a base language
}

// ConfigVersions returns the config fields of language lang of pack, or nil
// when the language is not stored: then config leaves flags.2 out and the
// clients keep their bundled strings.
func (s *Store) ConfigVersions(ctx context.Context, pack, lang string) (*ConfigVersions, error) {
	if pack == "" || lang == "" {
		return nil, nil
	}
	l, err := s.findLanguage(ctx, pack, lang)
	if err != nil || l == nil {
		return nil, err
	}
	v := &ConfigVersions{SuggestedLangCode: l.LangCode, LangPackVersion: l.Version}
	if l.BaseLangCode != "" {
		base, err := s.findLanguage(ctx, pack, l.BaseLangCode)
		if err != nil {
			return nil, err
		}
		if base != nil {
			v.BaseLangPackVersion = base.Version
		}
	}
	return v, nil
}

// StringsByKey returns the stored strings of keys (tombstones included) by
// key.
func (s *Store) StringsByKey(ctx context.Context, pack, lang string, keys []string) (map[string]*String, error) {
	found := make(map[string]*String, len(keys))
	for start := 0; start < len(keys); start += batchSize {
		part := keys[start:min(start+batchSize, len(keys))]
		args := []any{pack, lang}
		for _, k := range part {
			args = append(args, k)
		}
		var rows []stringRow
		if err := s.db.QueryRowsPartial(ctx, &rows,
			"SELECT "+stringColumns+" FROM langpack_strings WHERE lang_pack = ? AND lang_code = ? AND str_key IN ("+placeholders(len(part))+")",
			args...); err != nil {
			return nil, err
		}
		for i := range rows {
			v := rows[i].string()
			found[v.Key] = &v
		}
	}
	return found, nil
}

func toStrings(rows []stringRow) []String {
	out := make([]String, len(rows))
	for i := range rows {
		out[i] = rows[i].string()
	}
	return out
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// ImportResult says what an import changed in one pack and language.
type ImportResult struct {
	LangPack, LangCode string
	OldVersion         int32 // 0: the language was new
	Version            int32 // equal to OldVersion when nothing changed
	Added              int   // new keys and keys back from a tombstone
	Changed            int   // keys with another text
	Deleted            int   // keys gone from the snapshot
	Unchanged          int
	// Leftovers are the strings that still name Telegram after the
	// replacement: key -> the words (Leftovers). Keys kept on purpose
	// (keepKeys) are not listed.
	Leftovers map[string][]string
}

// Changes is the number of strings the import changed.
func (r *ImportResult) Changes() int { return r.Added + r.Changed + r.Deleted }

// Import puts snapshot snap into the store in one transaction, with
// "Telegram" replaced (Rebrand): strings with another text or back from a
// tombstone get the next version of the language, keys missing from snap
// become tombstones with it. Without changes no string is written and the
// version stays; the language's description is updated either way. The next
// version is above the stored one and at least Telegram's (snap.Version).
func (s *Store) Import(ctx context.Context, snap *Snapshot) (*ImportResult, error) {
	if err := snap.Validate(); err != nil {
		return nil, err
	}
	pack, lang := snap.LangPack, snap.LangCode
	res := &ImportResult{LangPack: pack, LangCode: lang, Leftovers: map[string][]string{}}

	incoming := make([]pendingString, len(snap.Strings))
	for i := range snap.Strings {
		up := &snap.Strings[i]
		rendered := Rebrand(lang, *up)
		incoming[i] = pendingString{s: &rendered, upstream: up}
		if words := leftoverWords(&rendered); len(words) > 0 {
			res.Leftovers[up.Key] = words
		}
	}

	err := s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		// The language row serializes imports of the same pack and language:
		// the upsert takes its exclusive lock (INSERT IGNORE would take a
		// shared one, and two imports would deadlock on FOR UPDATE).
		if _, err := tx.Exec("INSERT INTO langpack_languages (lang_pack, lang_code) VALUES (?, ?)"+
			" ON DUPLICATE KEY UPDATE lang_pack = lang_pack", pack, lang); err != nil {
			return err
		}
		var cur struct {
			Version int32 `db:"version"`
		}
		if err := tx.QueryRowPartial(&cur, "SELECT version FROM langpack_languages WHERE lang_pack = ? AND lang_code = ? FOR UPDATE", pack, lang); err != nil {
			return err
		}
		res.OldVersion, res.Version = cur.Version, cur.Version

		var stored []importRow
		if err := tx.QueryRowsPartial(&stored,
			"SELECT "+stringColumns+", "+upstreamColumns+" FROM langpack_strings WHERE lang_pack = ? AND lang_code = ?", pack, lang); err != nil {
			return err
		}
		p := planImport(stored, incoming)
		res.Added, res.Changed, res.Deleted, res.Unchanged = p.added, p.changed, p.deleted, p.unchanged

		if len(p.write)+len(p.remove) > 0 {
			res.Version = max(cur.Version+1, snap.Version)
		}
		for start := 0; start < len(p.write); start += batchSize {
			if err := upsertStrings(tx, pack, lang, p.write[start:min(start+batchSize, len(p.write))], res.Version); err != nil {
				return err
			}
		}
		for start := 0; start < len(p.refresh); start += batchSize {
			if err := upsertStrings(tx, pack, lang, p.refresh[start:min(start+batchSize, len(p.refresh))], -1); err != nil {
				return err
			}
		}
		for start := 0; start < len(p.remove); start += batchSize {
			part := p.remove[start:min(start+batchSize, len(p.remove))]
			args := []any{res.Version, pack, lang}
			for _, k := range part {
				args = append(args, k)
			}
			if _, err := tx.Exec("UPDATE langpack_strings SET deleted = 1, version = ? WHERE lang_pack = ? AND lang_code = ? AND str_key IN ("+
				placeholders(len(part))+")", args...); err != nil {
				return err
			}
		}

		l := snap.Language
		updatedAt := "updated_at"
		var updatedArg []any
		if res.Version != res.OldVersion {
			updatedAt = "?"
			updatedArg = []any{s.now().Unix()}
		}
		args := []any{l.Name, l.NativeName, l.PluralCode, nullIfEmpty(l.BaseLangCode), l.Official, l.RTL, l.Beta,
			l.StringsCount, l.TranslatedCount, l.TranslationsURL, l.SortOrder, res.Version, snap.Version}
		args = append(append(args, updatedArg...), pack, lang)
		_, err := tx.Exec("UPDATE langpack_languages SET name = ?, native_name = ?, plural_code = ?, base_lang_code = ?,"+
			" official = ?, rtl = ?, beta = ?, strings_count = ?, translated_count = ?, translations_url = ?, sort_order = ?,"+
			" version = ?, upstream_version = ?, updated_at = "+updatedAt+" WHERE lang_pack = ? AND lang_code = ?", args...)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("langpack: import %s/%s: %w", pack, lang, err)
	}
	return res, nil
}

// pendingString is a string to write: its text after the replacement and
// Telegram's.
type pendingString struct {
	s, upstream *String
}

type importPlan struct {
	write   []pendingString // new, changed or back from a tombstone: the new version
	refresh []pendingString // the same text for clients, another upstream text: version kept
	remove  []string        // gone: tombstones with the new version

	added, changed, deleted, unchanged int
}

// planImport compares the stored strings with a snapshot's: each incoming
// string as Telegram sent it and after the replacement.
func planImport(stored []importRow, incoming []pendingString) importPlan {
	var p importPlan
	byKey := make(map[string]*importRow, len(stored))
	for i := range stored {
		byKey[stored[i].Key] = &stored[i]
	}
	seen := make(map[string]bool, len(incoming))
	for _, in := range incoming {
		seen[in.s.Key] = true
		old := byKey[in.s.Key]
		switch {
		case old == nil || old.Deleted:
			p.added++
			p.write = append(p.write, in)
		case !sameText(in.s, ptr(old.string())):
			p.changed++
			p.write = append(p.write, in)
		default:
			p.unchanged++
			if !sameText(in.upstream, ptr(old.upstream())) {
				p.refresh = append(p.refresh, in)
			}
		}
	}
	for i := range stored {
		if !stored[i].Deleted && !seen[stored[i].Key] {
			p.deleted++
			p.remove = append(p.remove, stored[i].Key)
		}
	}
	return p
}

// upsertStrings writes rows with version, or, with version < 0, only their
// texts (the version and the tombstone flag of the stored rows stay).
func upsertStrings(tx *sqlx.Tx, pack, lang string, rows []pendingString, version int32) error {
	var b strings.Builder
	b.WriteString("INSERT INTO langpack_strings (lang_pack, lang_code, " + stringColumns + ", " + upstreamColumns + ") VALUES ")
	args := make([]any, 0, len(rows)*18)
	for i, r := range rows {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString("(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)")
		args = append(args, pack, lang, r.s.Key, r.s.Pluralized, r.s.Value,
			toNull(r.s.Zero), toNull(r.s.One), toNull(r.s.Two), toNull(r.s.Few), toNull(r.s.Many),
			false, max(version, 0),
			r.upstream.Value, toNull(r.upstream.Zero), toNull(r.upstream.One), toNull(r.upstream.Two),
			toNull(r.upstream.Few), toNull(r.upstream.Many))
	}
	b.WriteString(" ON DUPLICATE KEY UPDATE pluralized = VALUES(pluralized), value = VALUES(value), zero_value = VALUES(zero_value)," +
		" one_value = VALUES(one_value), two_value = VALUES(two_value), few_value = VALUES(few_value), many_value = VALUES(many_value)," +
		" upstream_value = VALUES(upstream_value), upstream_zero_value = VALUES(upstream_zero_value)," +
		" upstream_one_value = VALUES(upstream_one_value), upstream_two_value = VALUES(upstream_two_value)," +
		" upstream_few_value = VALUES(upstream_few_value), upstream_many_value = VALUES(upstream_many_value)")
	if version >= 0 {
		b.WriteString(", deleted = 0, version = VALUES(version)")
	}
	_, err := tx.Exec(b.String(), args...)
	return err
}

// texts returns the texts of s.
// leftoverWords returns the words that still name Telegram in the texts of
// rendered string s, or nil when s is kept as Telegram wrote it on purpose
// (keepKeys): the report lists only what the rules missed.
func leftoverWords(s *String) []string {
	if isKept(s.Key) {
		return nil
	}
	var words []string
	for _, t := range texts(s) {
		for _, w := range Leftovers(t) {
			if !slices.Contains(words, w) {
				words = append(words, w)
			}
		}
	}
	return words
}

func texts(s *String) []string {
	out := []string{s.Value}
	for _, f := range s.forms() {
		if *f != nil {
			out = append(out, **f)
		}
	}
	return out
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func ptr[T any](v T) *T { return &v }
