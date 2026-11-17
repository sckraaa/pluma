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
	"context"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/proto"
)

func keyBold(offset, length int32) *mtproto.MessageEntity {
	return mtproto.MakeTLMessageEntityBold(&mtproto.MessageEntity{Offset: offset, Length: length}).To_MessageEntity()
}

func keyTextUrl(offset, length int32, url string) *mtproto.MessageEntity {
	return mtproto.MakeTLMessageEntityTextUrl(&mtproto.MessageEntity{Offset: offset, Length: length, Url: url}).To_MessageEntity()
}

func keySource() KeySource {
	return KeySource{
		Profile:        "hy-mt1.5",
		ProfileVersion: 1,
		Model:          "hy-mt1.5-1.8b-q8_0",
		ToLang:         "ru",
		Text:           "Hello, world",
		Entities:       []*mtproto.MessageEntity{keyBold(0, 5), keyTextUrl(7, 5, "https://example.com")},
	}
}

// The order of the entities does not change the key; nil entities are
// nothing.
func TestCacheKeyEntityOrder(t *testing.T) {
	a := keySource()
	b := keySource()
	b.Entities = []*mtproto.MessageEntity{nil, b.Entities[1], b.Entities[0]}
	if CacheKey(a) != CacheKey(b) {
		t.Fatal("the order of the entities changed the key")
	}
}

// Every part of the source is part of the key.
func TestCacheKeyParts(t *testing.T) {
	base := CacheKey(keySource())
	changes := map[string]func(*KeySource){
		"profile":         func(s *KeySource) { s.Profile = "other" },
		"profile version": func(s *KeySource) { s.ProfileVersion = 2 },
		"model":           func(s *KeySource) { s.Model = "hy-mt1.5-1.8b-q6_k" },
		"language":        func(s *KeySource) { s.ToLang = "uk" },
		"text":            func(s *KeySource) { s.Text = "Hello, world!" },
		"no entities":     func(s *KeySource) { s.Entities = nil },
		"entity offset":   func(s *KeySource) { s.Entities[0] = keyBold(1, 4) },
		"entity type": func(s *KeySource) {
			s.Entities[0] = mtproto.MakeTLMessageEntityItalic(&mtproto.MessageEntity{Offset: 0, Length: 5}).To_MessageEntity()
		},
		"url":           func(s *KeySource) { s.Entities[1] = keyTextUrl(7, 5, "https://example.org") },
		"one more bold": func(s *KeySource) { s.Entities = append(s.Entities, keyBold(0, 5)) },
		"pre language": func(s *KeySource) {
			s.Entities[0] = mtproto.MakeTLMessageEntityPre(&mtproto.MessageEntity{Offset: 0, Length: 5, Language: "go"}).To_MessageEntity()
		},
		"mention user": func(s *KeySource) {
			s.Entities[0] = mtproto.MakeTLMessageEntityMentionName(&mtproto.MessageEntity{Offset: 0, Length: 5, UserId_INT64: 7}).To_MessageEntity()
		},
		"custom emoji": func(s *KeySource) {
			s.Entities[0] = mtproto.MakeTLMessageEntityCustomEmoji(&mtproto.MessageEntity{Offset: 0, Length: 5, DocumentId: 9}).To_MessageEntity()
		},
		"collapsed": func(s *KeySource) {
			s.Entities[0] = mtproto.MakeTLMessageEntityBlockquote(&mtproto.MessageEntity{Offset: 0, Length: 5, Collapsed: true}).To_MessageEntity()
		},
	}
	seen := map[Key]string{base: "base"}
	for name, change := range changes {
		s := keySource()
		change(&s)
		k := CacheKey(s)
		if was, ok := seen[k]; ok {
			t.Errorf("%s: the same key as %s", name, was)
		}
		seen[k] = name
	}
	// A blockquote that is not collapsed is another key than a collapsed
	// one, and both differ from bold.
	s := keySource()
	s.Entities[0] = mtproto.MakeTLMessageEntityBlockquote(&mtproto.MessageEntity{Offset: 0, Length: 5}).To_MessageEntity()
	if _, ok := seen[CacheKey(s)]; ok {
		t.Error("a blockquote has the key of another source")
	}
}

// Fields are written with their lengths: moving bytes from one field to
// the next is another key.
func TestCacheKeyBoundaries(t *testing.T) {
	a, b := keySource(), keySource()
	a.Model, a.ToLang = "mod", "elru"
	b.Model, b.ToLang = "model", "ru"
	if CacheKey(a) == CacheKey(b) {
		t.Fatal("model and language run into each other")
	}
	a, b = keySource(), keySource()
	a.ToLang, a.Text = "r", "uHello"
	b.ToLang, b.Text = "ru", "Hello"
	if CacheKey(a) == CacheKey(b) {
		t.Fatal("language and text run into each other")
	}
}

// The entities of a translation come back from their column as they went
// in, and no entities are an empty blob, not NULL.
func TestCacheEntitiesRoundTrip(t *testing.T) {
	b, err := marshalCacheEntities(nil)
	if err != nil {
		t.Fatal(err)
	}
	if b == nil || len(b) != 0 {
		t.Fatalf("no entities: %v", b)
	}
	got, err := unmarshalCacheEntities(b)
	if err != nil || len(got) != 0 {
		t.Fatalf("no entities back: %v %v", got, err)
	}
	in := keySource().Entities
	in = append(in,
		mtproto.MakeTLMessageEntityPre(&mtproto.MessageEntity{Offset: 1, Length: 2, Language: "go"}).To_MessageEntity(),
		mtproto.MakeTLMessageEntityMentionName(&mtproto.MessageEntity{Offset: 3, Length: 4, UserId_INT64: 777}).To_MessageEntity(),
		mtproto.MakeTLMessageEntityCustomEmoji(&mtproto.MessageEntity{Offset: 5, Length: 2, DocumentId: 1 << 40}).To_MessageEntity(),
		mtproto.MakeTLMessageEntityBlockquote(&mtproto.MessageEntity{Offset: 0, Length: 12, Collapsed: true}).To_MessageEntity(),
	)
	if b, err = marshalCacheEntities(in); err != nil {
		t.Fatal(err)
	}
	if got, err = unmarshalCacheEntities(b); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(in) {
		t.Fatalf("%d entities back of %d", len(got), len(in))
	}
	for i := range in {
		if !proto.Equal(in[i], got[i]) {
			t.Errorf("entity %d: %v, want %v", i, got[i], in[i])
		}
	}
}

func TestTranslationPeerCheck(t *testing.T) {
	for _, p := range []TranslationPeer{
		{Type: mtproto.PEER_USER, Id: 1},
		{Type: mtproto.PEER_CHAT, Id: 2},
		{Type: mtproto.PEER_CHANNEL, Id: 3},
	} {
		if err := p.check(); err != nil {
			t.Errorf("%+v: %v", p, err)
		}
	}
	for _, p := range []TranslationPeer{
		{Type: mtproto.PEER_SELF, Id: 1},
		{Type: mtproto.PEER_EMPTY, Id: 1},
		{Type: mtproto.PEER_USER, Id: 0},
		{Type: mtproto.PEER_CHANNEL, Id: -5},
	} {
		if err := p.check(); err == nil {
			t.Errorf("%+v is accepted", p)
		}
	}
}

// A translation without a known markup is refused before the database:
// the column would hold a value no reader understands.
func TestPutTranslationMarkup(t *testing.T) {
	s := NewStore(nil)
	for _, m := range []Markup{0, 3} {
		if err := s.PutTranslation(context.Background(), CacheKey(keySource()), "ru", "m", &CachedTranslation{Text: "x", Markup: m}); err == nil {
			t.Errorf("markup %d accepted", m)
		}
	}
}
