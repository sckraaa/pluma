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
	"cmp"
	"context"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/proto"
)

// The markup carries the entities of a text through the model (ADR 0064,
// section 3). The entities are cut into a properly nested tree, every node
// becomes a numbered tag <sN>…</sN> around its text, and the answer is read
// back into a text and entities. The content of a verbatim node (code, a
// link, a mention, a custom emoji…) is taken from the source whatever the
// model wrote in its tag. An answer that does not keep the tags is not
// used: the text is then translated by pieces between the verbatim
// entities, and the formatting is lost. Offsets are UTF-16 code units, as
// MTProto counts them; a surrogate pair is never cut.

// Markup tells what became of the formatting of an item. The values are
// those of the column translation_cache.markup.
type Markup int8

const (
	// MarkupKept: every entity is in the translation.
	MarkupKept Markup = 1
	// MarkupLost: the item went by pieces and its formatting (bold,
	// links…) is lost; the verbatim entities are in the translation.
	MarkupLost Markup = 2
)

func (m Markup) valid() bool { return m == MarkupKept || m == MarkupLost }

// MaxOutputFactor bounds a translation: longer than MaxOutputFactor times
// its source (plus outputSlack, for the shortest texts) it is not a
// translation but a model that repeats itself (ADR 0064, section 3).
const (
	MaxOutputFactor = 3
	outputSlack     = 64
)

// Result is the translation of one item.
type Result struct {
	Text     string
	Entities []*mtproto.MessageEntity
	Markup   Markup
	// Fallback: the item had entities and went by pieces
	// (translate_markup_fallback_total).
	Fallback bool
	// Calls is the number of requests made to the engine; 0 for an item
	// with nothing to translate (no letters out of verbatim entities).
	Calls int
}

// CallFunc sends one request to the engine: Engine.Translate, wrapped by
// the scheduler in a slot of the engine.
type CallFunc func(ctx context.Context, req Request) (string, error)

// UserExistence reports which of ids are users. It returns an error only
// when it cannot tell.
type UserExistence func(ctx context.Context, ids []int64) (map[int64]bool, error)

type entityKind int8

const (
	kindDrop     entityKind = iota // not translated, not kept
	kindFormat                     // its content is translated
	kindVerbatim                   // its content is kept as it is
)

func kindOf(e *mtproto.MessageEntity) entityKind {
	switch e.GetPredicateName() {
	case mtproto.Predicate_messageEntityBold,
		mtproto.Predicate_messageEntityItalic,
		mtproto.Predicate_messageEntityUnderline,
		mtproto.Predicate_messageEntityStrike,
		mtproto.Predicate_messageEntitySpoiler,
		mtproto.Predicate_messageEntityTextUrl,
		mtproto.Predicate_messageEntityBlockquote:
		return kindFormat
	case mtproto.Predicate_messageEntityCode,
		mtproto.Predicate_messageEntityPre,
		mtproto.Predicate_messageEntityUrl,
		mtproto.Predicate_messageEntityEmail,
		mtproto.Predicate_messageEntityMention,
		mtproto.Predicate_messageEntityMentionName,
		mtproto.Predicate_messageEntityHashtag,
		mtproto.Predicate_messageEntityCashtag,
		mtproto.Predicate_messageEntityBotCommand,
		mtproto.Predicate_messageEntityPhone,
		mtproto.Predicate_messageEntityBankCard,
		mtproto.Predicate_messageEntityCustomEmoji,
		mtproto.Predicate_messageEntityFormattedDate,
		mtproto.Predicate_messageEntityTonAddress,
		mtproto.Predicate_messageEntityUnknown:
		return kindVerbatim
	}
	// An input mention Normalize did not resolve, and the diff entities,
	// which only messages.composedMessageWithAI.diff_text carries and
	// which mean nothing in a translation.
	return kindDrop
}

// rank orders entities of the same range from the outside in: a quote
// holds formatting, formatting holds verbatim entities.
func rank(e *mtproto.MessageEntity) int {
	switch {
	case e.GetPredicateName() == mtproto.Predicate_messageEntityBlockquote:
		return 0
	case kindOf(e) == kindFormat:
		return 1
	}
	return 2
}

// utext is a text as runes with their UTF-16 offsets.
type utext struct {
	runes []rune
	// pos[i] is the UTF-16 offset of runes[i]; pos[len(runes)] is the
	// length of the text.
	pos []int
}

func newUText(s string) utext {
	runes := []rune(s)
	pos := make([]int, len(runes)+1)
	for i, r := range runes {
		n := utf16.RuneLen(r)
		if n < 0 {
			n = 1
		}
		pos[i+1] = pos[i] + n
	}
	return utext{runes: runes, pos: pos}
}

func (t utext) units() int { return t.pos[len(t.runes)] }

// floor returns the rune that holds the UTF-16 offset u (0 ≤ u ≤ units):
// an offset in the middle of a surrogate pair moves to its start.
func (t utext) floor(u int) int {
	i := sort.SearchInts(t.pos, u)
	if i < len(t.pos) && t.pos[i] == u {
		return i
	}
	return i - 1
}

// ceil returns the first rune at or after the UTF-16 offset u: an offset
// in the middle of a surrogate pair moves to its end.
func (t utext) ceil(u int) int { return sort.SearchInts(t.pos, u) }

func trimSpace(runes []rune, s, e int) (int, int) {
	for s < e && unicode.IsSpace(runes[s]) {
		s++
	}
	for e > s && unicode.IsSpace(runes[e-1]) {
		e--
	}
	return s, e
}

func hasLetter(runes []rune) bool {
	return slices.ContainsFunc(runes, unicode.IsLetter)
}

// Normalize returns the entities of text as a translation keeps them, in
// one order, so that equal texts give equal entities (they are a part of
// the key of the cache):
//   - entities out of the text, empty or of another kind are dropped; a
//     range that cuts a surrogate pair is widened to hold it;
//   - formatting does not begin or end with spaces (ADR 0064, section 3);
//   - inputMessageEntityMentionName becomes messageEntityMentionName with
//     its user (inputUserSelf is self), and is dropped when exist does not
//     know the user, as a message sent with it would not carry it; with a
//     nil exist every input mention is dropped. exist is called once, and
//     only when there are input mentions;
//   - the entities are sorted by offset, the longer first, and duplicates
//     are dropped.
func Normalize(ctx context.Context, text string, entities []*mtproto.MessageEntity, self int64, exist UserExistence) ([]*mtproto.MessageEntity, error) {
	t := newUText(text)
	units := t.units()
	out := make([]*mtproto.MessageEntity, 0, len(entities))
	var (
		ids       []int64
		fromInput map[*mtproto.MessageEntity]bool
	)
	for _, e := range entities {
		input := false
		if e.GetPredicateName() == mtproto.Predicate_inputMessageEntityMentionName {
			user := e.GetUserId_INPUTUSER()
			id := user.GetUserId()
			if user.GetPredicateName() == mtproto.Predicate_inputUserSelf {
				id = self
			}
			if id == 0 || exist == nil {
				continue
			}
			e = mtproto.MakeTLMessageEntityMentionName(&mtproto.MessageEntity{
				Offset:       e.GetOffset(),
				Length:       e.GetLength(),
				UserId_INT64: id,
			}).To_MessageEntity()
			input = true
		}
		kind := kindOf(e)
		if kind == kindDrop {
			continue
		}
		off, n := int(e.GetOffset()), int(e.GetLength())
		if off < 0 || n <= 0 || off >= units {
			continue
		}
		s, end := t.floor(off), t.ceil(min(off+n, units))
		if kind == kindFormat {
			s, end = trimSpace(t.runes, s, end)
		}
		if s >= end {
			continue
		}
		c := proto.Clone(e).(*mtproto.MessageEntity)
		c.Offset, c.Length = int32(t.pos[s]), int32(t.pos[end]-t.pos[s])
		out = append(out, c)
		if input {
			if fromInput == nil {
				fromInput = make(map[*mtproto.MessageEntity]bool)
			}
			fromInput[c] = true
			if !slices.Contains(ids, c.GetUserId_INT64()) {
				ids = append(ids, c.GetUserId_INT64())
			}
		}
	}
	if len(ids) > 0 {
		found, err := exist(ctx, ids)
		if err != nil {
			return nil, err
		}
		out = slices.DeleteFunc(out, func(e *mtproto.MessageEntity) bool {
			return fromInput[e] && !found[e.GetUserId_INT64()]
		})
	}
	sortEntities(out)
	return slices.CompactFunc(out, func(a, b *mtproto.MessageEntity) bool { return proto.Equal(a, b) }), nil
}

func sortEntities(ents []*mtproto.MessageEntity) {
	slices.SortStableFunc(ents, func(a, b *mtproto.MessageEntity) int {
		return cmp.Or(
			cmp.Compare(a.GetOffset(), b.GetOffset()),
			cmp.Compare(b.GetLength(), a.GetLength()),
			cmp.Compare(rank(a), rank(b)),
			strings.Compare(a.GetPredicateName(), b.GetPredicateName()),
		)
	})
}

// span is a range of runes of an entity, origin its index in the
// normalized entities.
type span struct {
	s, e, origin int
}

// node is a tag of the markup.
type node struct {
	tag      int // N of <sN>; 0 for the root
	origin   int
	verbatim bool
	s, e     int // runes of the source
	parent   *node
	children []*node
	// inner are the entities inside a verbatim node, in runes from its
	// start: they come back with its content and get no tags.
	inner []span
}

type markup struct {
	t      utext
	ents   []*mtproto.MessageEntity
	root   *node
	nodes  []*node // by tag - 1
	format bool    // a formatting entity is out of every verbatim one
}

// buildMarkup cuts ents, the normalized entities of text, into the tree of
// tags. A formatting entity crossed by a verbatim one is cut at its edge,
// so that a verbatim content always stays whole; any other crossing cuts
// the entity that begins later at the end of the one it begins in. The
// pieces of one entity become one again in the translation where they
// stay side by side (finish).
func buildMarkup(text string, ents []*mtproto.MessageEntity) *markup {
	m := &markup{t: newUText(text), ents: ents}
	spans := make([]span, len(ents))
	for i, e := range ents {
		spans[i] = span{m.t.floor(int(e.GetOffset())), m.t.ceil(int(e.GetOffset() + e.GetLength())), i}
	}
	queue := make([]span, 0, len(spans))
	for _, sp := range spans {
		if kindOf(ents[sp.origin]) == kindVerbatim {
			queue = append(queue, sp)
			continue
		}
		var cuts []int
		for _, v := range spans {
			if kindOf(ents[v.origin]) != kindVerbatim {
				continue
			}
			if v.s > sp.s && v.s < sp.e && v.e > sp.e {
				cuts = append(cuts, v.s)
			}
			if v.e > sp.s && v.e < sp.e && v.s < sp.s {
				cuts = append(cuts, v.e)
			}
		}
		slices.Sort(cuts)
		at := sp.s
		for _, c := range slices.Compact(cuts) {
			queue = append(queue, span{at, c, sp.origin})
			at = c
		}
		queue = append(queue, span{at, sp.e, sp.origin})
	}
	less := func(a, b span) int {
		return cmp.Or(
			cmp.Compare(a.s, b.s),
			cmp.Compare(b.e, a.e),
			cmp.Compare(rank(ents[a.origin]), rank(ents[b.origin])),
			cmp.Compare(a.origin, b.origin),
		)
	}
	slices.SortFunc(queue, less)

	m.root = &node{origin: -1, s: 0, e: len(m.t.runes)}
	stack := []*node{m.root}
	for len(queue) > 0 {
		x := queue[0]
		queue = queue[1:]
		for len(stack) > 1 && stack[len(stack)-1].e <= x.s {
			stack = stack[:len(stack)-1]
		}
		p := stack[len(stack)-1]
		if x.e > p.e {
			rest := span{p.e, x.e, x.origin}
			i, _ := slices.BinarySearchFunc(queue, rest, less)
			queue = slices.Insert(queue, i, rest)
			x.e = p.e
		}
		if p.verbatim {
			p.inner = append(p.inner, span{x.s - p.s, x.e - p.s, x.origin})
			continue
		}
		n := &node{
			origin:   x.origin,
			verbatim: kindOf(ents[x.origin]) == kindVerbatim,
			s:        x.s,
			e:        x.e,
			parent:   p,
		}
		if !n.verbatim {
			m.format = true
		}
		p.children = append(p.children, n)
		stack = append(stack, n)
	}

	var number func(n *node)
	number = func(n *node) {
		for _, c := range n.children {
			m.nodes = append(m.nodes, c)
			c.tag = len(m.nodes)
			number(c)
		}
	}
	number(m.root)
	return m
}

func openTag(n int) string  { return "<s" + strconv.Itoa(n) + ">" }
func closeTag(n int) string { return "</s" + strconv.Itoa(n) + ">" }

// render returns the text with the tags.
func (m *markup) render() string {
	var b strings.Builder
	var walk func(n *node)
	walk = func(n *node) {
		at := n.s
		for _, c := range n.children {
			b.WriteString(string(m.t.runes[at:c.s]))
			b.WriteString(openTag(c.tag))
			if c.verbatim {
				b.WriteString(string(m.t.runes[c.s:c.e]))
			} else {
				walk(c)
			}
			b.WriteString(closeTag(c.tag))
			at = c.e
		}
		b.WriteString(string(m.t.runes[at:n.e]))
	}
	walk(m.root)
	return b.String()
}

// translatable reports whether there is anything to translate: a letter
// out of the verbatim entities.
func (m *markup) translatable() bool {
	at := 0
	for _, v := range m.verbatims() {
		if hasLetter(m.t.runes[at:v.s]) {
			return true
		}
		at = v.e
	}
	return hasLetter(m.t.runes[at:])
}

// verbatims returns the verbatim nodes in the order of the text; they never
// overlap.
func (m *markup) verbatims() []*node {
	var out []*node
	for _, n := range m.nodes {
		if n.verbatim {
			out = append(out, n)
		}
	}
	return out
}

var (
	tagRe = regexp.MustCompile(`<(/?)s([0-9]{1,6})>`)
	// tagLikeRe finds what the model could take for a tag of the markup or
	// of the prompt. A source with it is translated by pieces, and an
	// answer with it out of the exact tags is not read.
	tagLikeRe = regexp.MustCompile(`(?i)<\s*/?\s*(s\s*[0-9]+|source|target)\b[^>]*>`)
)

// outSpan is an entity of the translation: runes of the output and the
// index of its entity in the normalized ones.
type outSpan struct {
	s, e, origin int
}

// decode reads the translation with tags. ok is false when the answer does
// not keep the tree: a tag missing, repeated, unknown, out of its parent
// (the order of siblings may change: a translation reorders words), or
// anything like a tag out of the exact ones.
func (m *markup) decode(answer string) (out []rune, spans []outSpan, ok bool) {
	matches := tagRe.FindAllStringSubmatchIndex(answer, -1)
	var rest strings.Builder
	at := 0
	for _, mm := range matches {
		rest.WriteString(answer[at:mm[0]])
		rest.WriteByte(' ')
		at = mm[1]
	}
	rest.WriteString(answer[at:])
	if tagLikeRe.MatchString(rest.String()) {
		return nil, nil, false
	}

	stack := []*node{m.root}
	starts := []int{0}
	seen := make([]bool, len(m.nodes))
	text := func(seg string) {
		if !stack[len(stack)-1].verbatim {
			out = append(out, []rune(seg)...)
		}
	}
	at = 0
	for _, mm := range matches {
		text(answer[at:mm[0]])
		at = mm[1]
		n, err := strconv.Atoi(answer[mm[4]:mm[5]])
		if err != nil || n < 1 || n > len(m.nodes) {
			return nil, nil, false
		}
		nd, top := m.nodes[n-1], stack[len(stack)-1]
		if mm[3] == mm[2] { // <sN>
			if seen[n-1] || nd.parent != top {
				return nil, nil, false
			}
			seen[n-1] = true
			stack = append(stack, nd)
			starts = append(starts, len(out))
			if nd.verbatim {
				base := len(out)
				out = append(out, m.t.runes[nd.s:nd.e]...)
				for _, in := range nd.inner {
					spans = append(spans, outSpan{base + in.s, base + in.e, in.origin})
				}
			}
			continue
		}
		// </sN>
		if top != nd {
			return nil, nil, false
		}
		spans = append(spans, outSpan{starts[len(starts)-1], len(out), nd.origin})
		stack, starts = stack[:len(stack)-1], starts[:len(starts)-1]
	}
	text(answer[at:])
	if len(stack) != 1 || slices.Contains(seen, false) {
		return nil, nil, false
	}
	return out, spans, true
}

// pieces translates the text between the verbatim nodes, each piece alone
// and without tags, and keeps the verbatim nodes as they are. A piece
// without letters (spaces, punctuation, emoji) is not sent.
func (m *markup) pieces(ctx context.Context, p Profile, lang string, call CallFunc, res *Result) ([]rune, []outSpan, error) {
	var (
		out   []rune
		spans []outSpan
	)
	gap := func(seg []rune) error {
		s, e := trimSpace(seg, 0, len(seg))
		if !hasLetter(seg[s:e]) {
			out = append(out, seg...)
			return nil
		}
		req, err := p.Request(lang, string(seg[s:e]), false)
		if err != nil {
			return err
		}
		answer, err := call(ctx, req)
		res.Calls++
		if err != nil {
			return err
		}
		tr, err := p.Answer(answer, false)
		if err != nil {
			return err
		}
		if tr == "" {
			return ErrBadAnswer
		}
		out = append(out, seg[:s]...)
		out = append(out, []rune(tr)...)
		out = append(out, seg[e:]...)
		return nil
	}
	at := 0
	for _, v := range m.verbatims() {
		if err := gap(m.t.runes[at:v.s]); err != nil {
			return nil, nil, err
		}
		base := len(out)
		out = append(out, m.t.runes[v.s:v.e]...)
		spans = append(spans, outSpan{base, len(out), v.origin})
		for _, in := range v.inner {
			spans = append(spans, outSpan{base + in.s, base + in.e, in.origin})
		}
		at = v.e
	}
	if err := gap(m.t.runes[at:]); err != nil {
		return nil, nil, err
	}
	return out, spans, nil
}

// finish makes the result of a translation: formatting without spaces at
// its edges, the pieces of one entity joined where they touch, the text
// without spaces at its ends (but never into a verbatim content), offsets
// in UTF-16.
func (m *markup) finish(out []rune, spans []outSpan, res *Result) error {
	verbatim := func(sp outSpan) bool { return kindOf(m.ents[sp.origin]) == kindVerbatim }
	slices.SortFunc(spans, func(a, b outSpan) int {
		return cmp.Or(cmp.Compare(a.origin, b.origin), cmp.Compare(a.s, b.s), cmp.Compare(a.e, b.e))
	})
	// Pieces of one entity that touch, or have only spaces between them
	// (the model may put one between two tags), are one entity again.
	joined := spans[:0]
	for _, sp := range spans {
		if n := len(joined); n > 0 && joined[n-1].origin == sp.origin && joined[n-1].e <= sp.s {
			if gs, ge := trimSpace(out, joined[n-1].e, sp.s); gs == ge {
				joined[n-1].e = sp.e
				continue
			}
		}
		joined = append(joined, sp)
	}
	spans = joined[:0]
	for _, sp := range joined {
		if !verbatim(sp) {
			sp.s, sp.e = trimSpace(out, sp.s, sp.e)
		}
		if sp.s < sp.e {
			spans = append(spans, sp)
		}
	}

	left, right := 0, len(out)
	protectedFrom, protectedTo := len(out), 0
	for _, sp := range spans {
		if verbatim(sp) {
			protectedFrom, protectedTo = min(protectedFrom, sp.s), max(protectedTo, sp.e)
		}
	}
	for left < right && left < protectedFrom && unicode.IsSpace(out[left]) {
		left++
	}
	for right > left && right > protectedTo && unicode.IsSpace(out[right-1]) {
		right--
	}
	if left == right {
		return ErrBadAnswer
	}

	t := newUText(string(out[left:right]))
	if t.units() > MaxOutputFactor*m.t.units()+outputSlack {
		return ErrBadAnswer
	}
	ents := make([]*mtproto.MessageEntity, 0, len(spans))
	for _, sp := range spans {
		s, e := max(sp.s, left)-left, min(sp.e, right)-left
		if s >= e {
			continue
		}
		c := proto.Clone(m.ents[sp.origin]).(*mtproto.MessageEntity)
		c.Offset, c.Length = int32(t.pos[s]), int32(t.pos[e]-t.pos[s])
		ents = append(ents, c)
	}
	sortEntities(ents)
	res.Text, res.Entities = string(t.runes), ents
	return nil
}

// Translate translates src into lang, a language of p (Profile.Lang),
// through call: with the tags of its entities when it has any, by pieces
// when the answer does not keep them (ADR 0064, section 3).
//
// The entities of src are normalized here with a nil UserExistence: the
// caller resolves the input mentions of a client's text beforehand
// (Normalize with its UserExistence). An item without letters out of its
// verbatim entities is given back as it is, without a request; an empty
// item is an empty result.
//
// The error is that of call (ErrUnavailable, ErrRejected, ErrBadAnswer,
// the context's), ErrBadAnswer for an empty or too long translation, or
// ErrToLangInvalid for a lang that is not of p.
func Translate(ctx context.Context, p Profile, lang string, src *mtproto.TextWithEntities, call CallFunc) (*Result, error) {
	text := src.GetText()
	if strings.TrimSpace(text) == "" {
		return &Result{Text: "", Entities: []*mtproto.MessageEntity{}, Markup: MarkupKept}, nil
	}
	ents, err := Normalize(ctx, text, src.GetEntities(), 0, nil)
	if err != nil {
		return nil, err
	}
	m := buildMarkup(text, ents)
	res := &Result{Markup: MarkupKept}
	if !m.translatable() {
		res.Text, res.Entities = text, ents
		return res, nil
	}

	if len(m.nodes) > 0 && !tagLikeRe.MatchString(text) {
		req, err := p.Request(lang, m.render(), true)
		if err != nil {
			return nil, err
		}
		answer, err := call(ctx, req)
		res.Calls++
		if err != nil {
			return nil, err
		}
		if inner, err := p.Answer(answer, true); err == nil {
			if out, spans, ok := m.decode(inner); ok {
				if err = m.finish(out, spans, res); err != nil {
					return nil, err
				}
				return res, nil
			}
		}
	}

	if len(m.nodes) > 0 {
		res.Fallback = true
		if m.format {
			res.Markup = MarkupLost
		}
	}
	out, spans, err := m.pieces(ctx, p, lang, call, res)
	if err != nil {
		return nil, err
	}
	if err = m.finish(out, spans, res); err != nil {
		return nil, err
	}
	return res, nil
}
