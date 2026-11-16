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

package cdn

import (
	"bytes"
	"encoding/hex"
	"math"
	"strings"
	"testing"
)

// TestRedirectTokenRoundTrip: the layout of the clients' contract, and back.
func TestRedirectTokenRoundTrip(t *testing.T) {
	id := randomBytes(t, TokenSize)
	url := "https://abc123.gcorecdn.com" + ObjectPath(id) + "?md5=Z67eXScYNJKnUT0AwcuBag&expires=1763300000"
	tok, err := EncodeRedirectToken(id, 1763300000, url)
	if err != nil {
		t.Fatal(err)
	}
	// "PLH1" | id | exp:uint32be | url
	want := append(append(append([]byte("PLH1"), id...), 0x69, 0x19, 0xd2, 0xa0), url...)
	if !bytes.Equal(tok, want) {
		t.Fatalf("token %s, want %s", hex.EncodeToString(tok[:44]), hex.EncodeToString(want[:44]))
	}
	gotId, exp, gotURL, err := ParseRedirectToken(tok)
	if err != nil || !bytes.Equal(gotId, id) || exp != 1763300000 || gotURL != url {
		t.Fatalf("parsed %x, %d, %q, %v", gotId, exp, gotURL, err)
	}
	// The id is a copy: changing it does not change the token.
	gotId[0] ^= 0xff
	if tok[4] != id[0] {
		t.Fatal("the parsed id shares the token's bytes")
	}
	// The stand of the e2e tests serves over http.
	if _, err = EncodeRedirectToken(id, math.MaxUint32, "http://fakecdn:8080/o/00"); err != nil {
		t.Fatalf("http to the stand: %v", err)
	}
}

// TestRedirectTokenRejects: the parse is strict.
func TestRedirectTokenRejects(t *testing.T) {
	id := randomBytes(t, TokenSize)
	tok, err := EncodeRedirectToken(id, 1763300000, "https://cdn.example.com/o/00")
	if err != nil {
		t.Fatal(err)
	}
	head := tok[:redirectTokenHead]
	with := func(url string) []byte { return append(bytes.Clone(head), url...) }
	for name, b := range map[string][]byte{
		"empty":            nil,
		"the bare id":      id,
		"no magic":         append([]byte("PLH2"), tok[4:]...),
		"lower case magic": append([]byte("plh1"), tok[4:]...),
		"truncated id":     tok[:30],
		"no URL":           head,
		"not UTF-8":        with("https://cdn.example.com/o/\xff\xfe"),
		"no scheme":        with("cdn.example.com/o/00"),
		"ftp":              with("ftp://cdn.example.com/o/00"),
		"no host":          with("https:///o/00"),
		"user info":        with("https://user@cdn.example.com/o/00"),
		"control char":     with("https://cdn.example.com/o/\n00"),
		"too long":         with("https://cdn.example.com/o/" + strings.Repeat("a", maxRedirectURL)),
		"zero expiry":      append(append(append([]byte("PLH1"), id...), 0, 0, 0, 0), "https://cdn.example.com/o/00"...),
	} {
		if _, _, _, err := ParseRedirectToken(b); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
	for name, c := range map[string]struct {
		id  []byte
		exp int64
		url string
	}{
		"short id":       {id[:31], 1, "https://cdn.example.com/o/00"},
		"zero expiry":    {id, 0, "https://cdn.example.com/o/00"},
		"past uint32":    {id, math.MaxUint32 + 1, "https://cdn.example.com/o/00"},
		"empty URL":      {id, 1, ""},
		"relative URL":   {id, 1, "/o/00"},
		"not UTF-8 URL":  {id, 1, "https://cdn.example.com/\xff"},
		"javascript URL": {id, 1, "javascript:alert(1)"},
	} {
		if _, err := EncodeRedirectToken(c.id, c.exp, c.url); err == nil {
			t.Errorf("%s: encoded", name)
		}
	}
}
