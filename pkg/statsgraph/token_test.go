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

package statsgraph

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// newKey is a throwaway key of a test.
func newKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, MinKeyBytes)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return key
}

func newSigner(t *testing.T) *Signer {
	t.Helper()
	s, err := NewSigner(newKey(t))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

const tokenNow = int64(1_800_000_000)

func sampleToken() Token {
	return Token{
		Kind: "growth", PeerType: 4, PeerID: 1_000_000_123, Object: 77, Viewer: 136817688,
		Lang: "pt-br", Dark: true, Expires: tokenNow + TokenLifetime,
	}
}

func TestTokenRoundTrip(t *testing.T) {
	s := newSigner(t)
	for _, tok := range []Token{
		sampleToken(),
		{Kind: "k", PeerID: -5, Object: -1, Viewer: 1, Expires: tokenNow + 1},
		{Kind: strings.Repeat("k", MaxTokenField), Lang: strings.Repeat("l", MaxTokenField), PeerType: -1, Expires: 1 << 62},
	} {
		str, err := s.Sign(tok)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(str, "v1.") || strings.ContainsAny(str, "+/= ") || len(str) > maxTokenLen {
			t.Errorf("token %q", str)
		}
		got, err := s.Verify(str, tokenNow)
		if err != nil {
			t.Fatalf("%+v: %v", tok, err)
		}
		if got != tok {
			t.Errorf("got %+v, want %+v", got, tok)
		}
	}
}

func TestTokenRejects(t *testing.T) {
	s := newSigner(t)
	tok := sampleToken()
	str, err := s.Sign(tok)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(str, ".")

	// The same fields for another viewer, signed by nobody: the token of a
	// user handed to another, or edited.
	other := tok
	other.Viewer++
	forgedViewer := parts[0] + "." + base64.RawURLEncoding.EncodeToString(other.encode()) + "." + parts[2]

	otherKey := newSigner(t)
	strOtherKey, err := otherKey.Sign(tok)
	if err != nil {
		t.Fatal(err)
	}

	flip := func(s string, i int) string {
		b := []byte(s)
		if b[i] == 'A' {
			b[i] = 'B'
		} else {
			b[i] = 'A'
		}
		return string(b)
	}

	invalid := map[string]string{
		"empty":           "",
		"garbage":         "not a token",
		"two parts":       parts[0] + "." + parts[1],
		"four parts":      str + ".x",
		"forged viewer":   forgedViewer,
		"another key":     strOtherKey,
		"payload edited":  parts[0] + "." + flip(parts[1], 3) + "." + parts[2],
		"mac edited":      parts[0] + "." + parts[1] + "." + flip(parts[2], 5),
		"mac cut":         parts[0] + "." + parts[1] + "." + parts[2][:20],
		"mac not base64":  parts[0] + "." + parts[1] + ".!!!",
		"no version":      "." + parts[1] + "." + parts[2],
		"version letters": "vx." + parts[1] + "." + parts[2],
		"too long":        str + strings.Repeat("A", maxTokenLen),
	}
	for name, v := range invalid {
		if _, err := s.Verify(v, tokenNow); !errors.Is(err, mtproto.ErrGraphInvalidReload) {
			t.Errorf("%s: %v", name, err)
		}
	}

	// Another version of the format: the client is told to load the
	// statistics again for a token it can use.
	for _, v := range []string{"v2", "v0", "v10"} {
		if _, err := s.Verify(v+"."+parts[1]+"."+parts[2], tokenNow); !errors.Is(err, mtproto.ErrGraphOutdatedReload) {
			t.Errorf("%s: %v", v, err)
		}
	}

	if _, err := s.Verify(str, tok.Expires-1); err != nil {
		t.Errorf("a second before it expires: %v", err)
	}
	for _, at := range []int64{tok.Expires, tok.Expires + 86400} {
		if _, err := s.Verify(str, at); !errors.Is(err, mtproto.ErrGraphExpiredReload) {
			t.Errorf("at %d: %v", at, err)
		}
	}
	// A forged token never says it expired: that would tell it was close.
	if _, err := s.Verify(forgedViewer, tok.Expires+1); !errors.Is(err, mtproto.ErrGraphInvalidReload) {
		t.Errorf("forged and expired: %v", err)
	}
}

// Signed fields that do not read back are refused, whatever signed them.
func TestTokenMalformedPayload(t *testing.T) {
	s := newSigner(t)
	good := sampleToken().encode()
	payloads := map[string][]byte{
		"empty":         {},
		"no kind":       append([]byte{0}, good[1+len("growth"):]...),
		"cut":           good[:len(good)-1],
		"trailing":      append(append([]byte(nil), good...), 0),
		"dark 2":        func() []byte { b := append([]byte(nil), good...); b[len(b)-9] = 2; return b }(),
		"kind too long": append([]byte{MaxTokenField + 1}, make([]byte, 80)...),
	}
	for name, p := range payloads {
		enc := base64.RawURLEncoding.EncodeToString(p)
		str := tokenVersion + "." + enc + "." + base64.RawURLEncoding.EncodeToString(s.mac(tokenVersion, enc))
		if _, err := s.Verify(str, tokenNow); !errors.Is(err, mtproto.ErrGraphInvalidReload) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestTokenSignRejects(t *testing.T) {
	s := newSigner(t)
	for _, tok := range []Token{
		{Expires: 1},
		{Kind: strings.Repeat("k", MaxTokenField+1)},
		{Kind: "k", Lang: strings.Repeat("l", MaxTokenField+1)},
	} {
		if _, err := s.Sign(tok); err == nil {
			t.Errorf("%+v signed", tok)
		}
	}
}

func TestKeys(t *testing.T) {
	key := newKey(t)
	got, err := ParseKey(" " + base64.StdEncoding.EncodeToString(key) + "\n")
	if err != nil || string(got) != string(key) {
		t.Fatalf("parse: %v", err)
	}
	for name, v := range map[string]string{
		"empty":      "",
		"short":      base64.StdEncoding.EncodeToString(key[:MinKeyBytes-1]),
		"not base64": "%%%" + base64.StdEncoding.EncodeToString(key),
	} {
		if _, err := ParseKey(v); err == nil {
			t.Errorf("%s: parsed", name)
		} else if strings.Contains(err.Error(), v) && v != "" {
			t.Errorf("%s: the error holds the key", name)
		}
	}
	if _, err := NewSigner(key[:MinKeyBytes-1]); err == nil {
		t.Error("a short key made a signer")
	}

	// The signer keeps its own copy of the key.
	s, err := NewSigner(key)
	if err != nil {
		t.Fatal(err)
	}
	str, err := s.Sign(sampleToken())
	if err != nil {
		t.Fatal(err)
	}
	key[0] ^= 0xff
	if _, err := s.Verify(str, tokenNow); err != nil {
		t.Errorf("after the caller's key changed: %v", err)
	}
}
