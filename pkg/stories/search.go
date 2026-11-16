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

package stories

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
)

// The search of stories (task 104, ADR 0069, p. 4-5): the hashtags and
// cashtags of a caption and the places of the areas of a story go to
// story_tags and story_places in the transaction that writes the story, and
// stories.searchPosts reads its candidates from there.

// The kinds of a tag (story_tags.kind).
const (
	TagHashtag int8 = 1
	TagCashtag int8 = 2
)

// The kinds of a place (story_places.kind).
const (
	PlaceVenue int8 = 1
	PlaceGeo   int8 = 2
)

const (
	// MaxTag is the longest tag, in characters, without its sign; a longer
	// one is not indexed. The web client links at most 64 (tweb
	// richTextProcessor/index.ts:64, TAG_REG_EXP).
	MaxTag = 64
	// MaxSearchScan bounds the candidates one search reads.
	MaxSearchScan = 2000
)

// The widths of the columns of story_places, in characters.
const (
	maxVenueKey = 255
	maxCountry  = 8
	maxState    = 128
	maxCity     = 128
	maxStreet   = 255
)

// Tag is one hashtag or cashtag: lower case, without its sign.
type Tag struct {
	Kind int8
	Tag  string
}

// Place is one venue or geo point with an address of a story. Seq is the
// index of its area among the areas of the story; the search does not use
// it.
type Place struct {
	Seq  int32
	Kind int8
	// VenueKey is provider ":" venue_id of a venue.
	VenueKey string
	// Country, State, City and Street are the address of a geo point,
	// lower case; empty for a level the address does not name.
	Country, State, City, Street string
}

// Ref names one story.
type Ref struct {
	Peer Peer
	Id   int32
}

// SearchCursor is the place of a story in the results of a search, which
// go newest first: by date, then by the type and id of the peer and the id
// of the story, all descending. The zero cursor is before the newest.
type SearchCursor struct {
	Date int32
	Peer Peer
	Id   int32
}

// CursorOf is the cursor of st: a search after it goes on with the story
// that follows st.
func CursorOf(st *Story) SearchCursor {
	return SearchCursor{Date: st.Date, Peer: st.Peer, Id: st.Id}
}

// tagRune tells the letters of a tag: letters, digits and the underscore,
// as the web client links them (tweb richTextProcessor/index.ts:36,64), and
// the combining marks some scripts need inside a word.
func tagRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r)
}

// NormalizeTag is the tag of kind as the index keeps it, from body, the
// text after the sign: its letters up to the first other character (the
// "@channel" of "#tag@channel", or the punctuation after a word), lower
// case. ok is false when nothing is left, when the tag is longer than
// MaxTag, or for a cashtag that does not start with a letter ("$100" is a
// price, not a tag; the clients link a cashtag of letters only, tweb
// richTextProcessor/index.ts:64, Android MessageObject.java:7965).
func NormalizeTag(kind int8, body string) (string, bool) {
	end := len(body)
	for i, r := range body {
		if !tagRune(r) {
			end = i
			break
		}
	}
	tag := []rune(body[:end])
	if len(tag) == 0 || len(tag) > MaxTag {
		return "", false
	}
	if kind == TagCashtag && !unicode.IsLetter(tag[0]) {
		return "", false
	}
	return strings.ToLower(string(tag)), true
}

// tagStart tells whether a sign after prev (0 at the start of the text)
// starts a tag: after a space, "(" or "]", as the web client links them
// (tweb richTextProcessor/index.ts:65, FULL_REG_EXP; Android
// MessageObject.java:7965 allows "(" too), or after the same sign, so that
// "##tag" is the tag "tag" (iOS GenerateTextEntities.swift:344-349 starts
// a hashtag at every "#").
func tagStart(prev, sign rune) bool {
	return prev == 0 || prev == sign || prev == '(' || prev == ']' || unicode.IsSpace(prev) || !unicode.IsPrint(prev)
}

// Tags are the hashtags and cashtags of a caption, each once: those the
// client marked with messageEntityHashtag and messageEntityCashtag (the
// offsets count UTF-16 units) and those the text itself has (tagStart):
// the clients that send a story mostly leave these entities out (tdesktop
// api_text_entities.cpp:300, ConvertOption::SkipLocal) and link the tags of
// the text themselves.
func Tags(caption string, entities []*mtproto.MessageEntity) []Tag {
	var out []Tag
	seen := make(map[Tag]bool)
	add := func(kind int8, body string) {
		tag, ok := NormalizeTag(kind, body)
		if !ok {
			return
		}
		t := Tag{Kind: kind, Tag: tag}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	var units []uint16
	for _, e := range entities {
		var kind int8
		var sign rune
		switch e.GetPredicateName() {
		case mtproto.Predicate_messageEntityHashtag:
			kind, sign = TagHashtag, '#'
		case mtproto.Predicate_messageEntityCashtag:
			kind, sign = TagCashtag, '$'
		default:
			continue
		}
		if units == nil {
			units = utf16.Encode([]rune(caption))
		}
		from, n := int(e.GetOffset()), int(e.GetLength())
		if from < 0 || n < 2 || from+n > len(units) {
			continue
		}
		text := string(utf16.Decode(units[from : from+n]))
		if body, ok := strings.CutPrefix(text, string(sign)); ok {
			add(kind, body)
		}
	}
	var prev rune
	for i, r := range caption {
		switch {
		case r == '#' && tagStart(prev, r):
			add(TagHashtag, caption[i+1:])
		case r == '$' && tagStart(prev, r):
			add(TagCashtag, caption[i+1:])
		}
		prev = r
	}
	return out
}

// level is one level of an address as the index keeps it: trimmed, lower
// case, cut to the width of its column.
func level(v string, width int) string {
	r := []rune(strings.ToLower(strings.TrimSpace(v)))
	if len(r) > width {
		r = r[:width]
	}
	return string(r)
}

// PlaceOf is the place of an area: a mediaAreaVenue with a venue id, or a
// mediaAreaGeoPoint with an address that names at least one level. ok is
// false for any other area, which the search does not find by.
func PlaceOf(area *mtproto.MediaArea) (Place, bool) {
	switch area.GetPredicateName() {
	case mtproto.Predicate_mediaAreaVenue:
		if area.GetVenueId() == "" {
			return Place{}, false
		}
		key := area.GetProvider() + ":" + area.GetVenueId()
		if len([]rune(key)) > maxVenueKey {
			return Place{}, false
		}
		return Place{Kind: PlaceVenue, VenueKey: key}, true
	case mtproto.Predicate_mediaAreaGeoPoint:
		a := area.GetAddress_FLAGGEOPOINTADDRESS()
		if a == nil {
			return Place{}, false
		}
		p := Place{
			Kind:    PlaceGeo,
			Country: level(a.GetCountryIso2(), maxCountry),
			State:   level(a.GetState().GetValue(), maxState),
			City:    level(a.GetCity().GetValue(), maxCity),
			Street:  level(a.GetStreet().GetValue(), maxStreet),
		}
		if p.Country == "" && p.State == "" && p.City == "" && p.Street == "" {
			return Place{}, false
		}
		return p, true
	}
	return Place{}, false
}

// Places are the places of the areas of a story, each with the index of
// its area.
func Places(areas []*mtproto.MediaArea) []Place {
	var out []Place
	for i, a := range areas {
		if p, ok := PlaceOf(a); ok {
			p.Seq = int32(i)
			out = append(out, p)
		}
	}
	return out
}

// searchable tells whether st is found by everyone the search lets in: a
// user's story for everyone (exceptions allowed, the search checks them for
// each viewer), and every story of a channel.
func searchable(st *Story) bool {
	return !st.Peer.IsUser() || mtproto.StoryAudienceOf(st.Privacy) == mtproto.StoryAudiencePublic
}

// indexTx writes the tags and places of st as they are now, in place of
// those it had, and marks it indexed. It runs in the transaction that
// writes st, under the lock of its peer (lockPeerTx); a repeat changes
// nothing.
func indexTx(tx *sqlx.Tx, st *Story) error {
	key := []any{st.Peer.Type, st.Peer.Id, st.Id}
	for _, table := range []string{"story_tags", "story_places"} {
		if _, err := tx.Exec("DELETE FROM "+table+" WHERE peer_type = ? AND peer_id = ? AND story_id = ?", key...); err != nil {
			return err
		}
	}
	everyone := searchable(st)
	if tags := Tags(st.Caption, st.Entities); len(tags) > 0 {
		values := make([]string, 0, len(tags))
		args := make([]any, 0, len(tags)*7)
		for _, t := range tags {
			values = append(values, "("+in(7)+")")
			args = append(args, t.Kind, t.Tag, st.Peer.Type, st.Peer.Id, st.Id, everyone, st.Date)
		}
		if _, err := tx.Exec("INSERT INTO story_tags (kind, tag, peer_type, peer_id, story_id, everyone, `date`) VALUES "+strings.Join(values, ", "), args...); err != nil {
			return err
		}
	}
	if places := Places(st.MediaAreas); len(places) > 0 {
		values := make([]string, 0, len(places))
		args := make([]any, 0, len(places)*12)
		for _, p := range places {
			values = append(values, "("+in(12)+")")
			args = append(args, st.Peer.Type, st.Peer.Id, st.Id, p.Seq, p.Kind, p.VenueKey, p.Country, p.State, p.City, p.Street, everyone, st.Date)
		}
		if _, err := tx.Exec("INSERT INTO story_places (peer_type, peer_id, story_id, seq, kind, venue_key, country, state, city, street, everyone, `date`) VALUES "+
			strings.Join(values, ", "), args...); err != nil {
			return err
		}
	}
	_, err := tx.Exec("UPDATE stories SET search_indexed = 1 WHERE peer_type = ? AND peer_id = ? AND story_id = ?", key...)
	return err
}

// Unindexed returns stories whose tags and places are not in the index yet,
// limit at most: those stored before the index existed.
func (s *Store) Unindexed(ctx context.Context, limit int) ([]Ref, error) {
	var rows []struct {
		PeerType int32 `db:"peer_type"`
		PeerId   int64 `db:"peer_id"`
		StoryId  int32 `db:"story_id"`
	}
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT peer_type, peer_id, story_id FROM stories WHERE search_indexed = 0 LIMIT ?", limit); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	out := make([]Ref, len(rows))
	for i, r := range rows {
		out[i] = Ref{Peer: Peer{Type: r.PeerType, Id: r.PeerId}, Id: r.StoryId}
	}
	return out, nil
}

// Reindex writes the tags and places of the story id of peer anew under
// the lock of the peer and marks it indexed; a story that is gone needs
// nothing. Two workers at once do it one after the other, and the second
// changes nothing.
func (s *Store) Reindex(ctx context.Context, peer Peer, id int32) error {
	return s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		if _, err := lockPeerTx(tx, peer); err != nil {
			return err
		}
		var rows []storyRow
		if err := tx.QueryRowsPartial(&rows, "SELECT "+storyColumns+" FROM stories WHERE peer_type = ? AND peer_id = ? AND story_id = ?", peer.Type, peer.Id, id); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		st, err := rows[0].story()
		if err != nil {
			return err
		}
		return indexTx(tx, st)
	})
}

// prefixed is storyColumns of the table alias a.
func prefixed(a string) string {
	cols := strings.Split(storyColumns, ", ")
	for i, c := range cols {
		cols[i] = a + "." + c
	}
	return strings.Join(cols, ", ")
}

// scanLimit is scan within 1..MaxSearchScan.
func scanLimit(scan int) int {
	return max(1, min(scan, MaxSearchScan))
}

// candidateWhere is what every candidate of a search meets, on the index
// table i joined with the stories s: found by everyone, of peer when it is
// named, after the cursor, and shown outside the feeds too: in them at now,
// or in the profile (pinned) after its period.
func candidateWhere(i string, peer *Peer, after SearchCursor, now int32) (string, []any) {
	q := " AND " + i + ".everyone = 1 AND (s.expire_date > ? OR s.pinned = 1)"
	args := []any{now}
	if peer != nil {
		q += " AND " + i + ".peer_type = ? AND " + i + ".peer_id = ?"
		args = append(args, peer.Type, peer.Id)
	}
	if after != (SearchCursor{}) {
		q += " AND (" + i + ".`date`, " + i + ".peer_type, " + i + ".peer_id, " + i + ".story_id) < (?, ?, ?, ?)"
		args = append(args, after.Date, after.Peer.Type, after.Peer.Id, after.Id)
	}
	return q, args
}

// candidateOrder is the order of the results of a search on the index
// table i: that of SearchCursor.
func candidateOrder(i string) string {
	return " ORDER BY " + i + ".`date` DESC, " + i + ".peer_type DESC, " + i + ".peer_id DESC, " + i + ".story_id DESC LIMIT ?"
}

func (s *Store) candidates(ctx context.Context, query string, args ...any) ([]*Story, error) {
	var rows []storyRow
	if err := s.db.QueryRowsPartial(ctx, &rows, query, args...); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	return stories(rows)
}

// SearchTags returns the candidates of a search by the tag of kind, the
// text after its sign (NormalizeTag): the stories that carry it and that
// everyone the search lets in may find (a user's story for everyone, a
// channel's story), of peer when it is not nil, in the feeds at now or in
// their profile, after the cursor, newest first, scan at most (up to
// MaxSearchScan). Who of them the searcher may see is for the caller to
// decide.
func (s *Store) SearchTags(ctx context.Context, kind int8, tag string, peer *Peer, after SearchCursor, scan int, now int32) ([]*Story, error) {
	tag, ok := NormalizeTag(kind, tag)
	if !ok {
		return nil, nil
	}
	where, args := candidateWhere("t", peer, after, now)
	args = append([]any{kind, tag}, args...)
	return s.candidates(ctx, "SELECT "+prefixed("s")+" FROM story_tags t JOIN stories s ON s.peer_type = t.peer_type AND s.peer_id = t.peer_id AND s.story_id = t.story_id"+
		" WHERE t.kind = ? AND t.tag = ?"+where+candidateOrder("t"), append(args, scanLimit(scan))...)
}

// SearchPlaces returns the candidates of a search by place (PlaceOf), as
// SearchTags does: a venue finds the stories with the same provider and
// venue id, a geo point those whose address matches every level the place
// names (a city finds every street of it).
func (s *Store) SearchPlaces(ctx context.Context, place Place, peer *Peer, after SearchCursor, scan int, now int32) ([]*Story, error) {
	match := " AND p.kind = ?"
	matchArgs := []any{place.Kind}
	switch place.Kind {
	case PlaceVenue:
		if place.VenueKey == "" {
			return nil, nil
		}
		match += " AND p.venue_key = ?"
		matchArgs = append(matchArgs, place.VenueKey)
	case PlaceGeo:
		levels := 0
		for _, l := range []struct{ column, value string }{
			{"country", place.Country}, {"state", place.State}, {"city", place.City}, {"street", place.Street},
		} {
			if l.value != "" {
				match += " AND p." + l.column + " = ?"
				matchArgs = append(matchArgs, l.value)
				levels++
			}
		}
		if levels == 0 {
			return nil, nil
		}
	default:
		return nil, nil
	}
	where, args := candidateWhere("p", peer, after, now)
	// A story with two areas of the same place is one result.
	return s.candidates(ctx, "SELECT "+prefixed("s")+" FROM stories s JOIN"+
		" (SELECT DISTINCT p.peer_type, p.peer_id, p.story_id, p.`date`, p.everyone FROM story_places p WHERE 1 = 1"+match+") p"+
		" ON s.peer_type = p.peer_type AND s.peer_id = p.peer_id AND s.story_id = p.story_id WHERE 1 = 1"+where+candidateOrder("p"),
		append(append(matchArgs, args...), scanLimit(scan))...)
}
