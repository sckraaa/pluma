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

package iosupdate

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

var testSecret = []byte("0123456789abcdef0123456789abcdef0123456789abcdef")

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// TestTicket: a ticket says its build, its day and its session, and is
// taken by the secret that minted it.
func TestTicket(t *testing.T) {
	now := at("2026-11-11T15:04:05Z")
	authKey := int64(-0x1234567890abcdef)
	ticket := Mint(testSecret, 60, authKey, now)
	if len(ticket) != 66 || strings.ContainsAny(ticket, "+/=&?# ") {
		t.Fatalf("a ticket of %d characters, not safe in a URL as it is", len(ticket))
	}
	got, err := Verify(testSecret, ticket, now)
	if err != nil || got.Build != 60 || got.Day != uint32(now.Unix()/86400) {
		t.Fatalf("%+v, %v", got, err)
	}
	// The session is named by a hash: the auth key is nowhere in the ticket.
	raw, _ := base64.RawURLEncoding.DecodeString(ticket)
	var key [8]byte
	binary.BigEndian.PutUint64(key[:], uint64(authKey))
	if bytes.Contains(raw, key[:]) || bytes.Contains(raw, key[:4]) {
		t.Error("the auth key is in the ticket")
	}
	other, err := Verify(testSecret, Mint(testSecret, 60, authKey+1, now), now)
	if err != nil || other.Subject == got.Subject {
		t.Errorf("another session: %v, the same subject %v", err, other.Subject == got.Subject)
	}
	// The same session with another build or on another day: the same
	// subject.
	next, err := Verify(testSecret, Mint(testSecret, 61, authKey, now.Add(24*time.Hour)), now.Add(24*time.Hour))
	if err != nil || next.Subject != got.Subject || next.Build != 61 {
		t.Errorf("the session later: %+v, %v", next, err)
	}
}

// TestTicketStable: within a UTC day a session gets one ticket for a
// build, whatever the moment, so the url of help.getAppUpdate does not
// change between two requests of the app; another day, build, session or
// secret gives another.
func TestTicketStable(t *testing.T) {
	first := Mint(testSecret, 60, 7001, at("2026-11-11T00:00:00Z"))
	for _, moment := range []string{"2026-11-11T00:00:01Z", "2026-11-11T12:00:00Z", "2026-11-11T23:59:59Z", "2026-11-12T02:59:59+03:00"} {
		if got := Mint(testSecret, 60, 7001, at(moment)); got != first {
			t.Errorf("at %s the ticket differs", moment)
		}
	}
	for name, other := range map[string]string{
		"the day before":  Mint(testSecret, 60, 7001, at("2026-11-10T23:59:59Z")),
		"the next day":    Mint(testSecret, 60, 7001, at("2026-11-12T00:00:00Z")),
		"another build":   Mint(testSecret, 61, 7001, at("2026-11-11T12:00:00Z")),
		"another session": Mint(testSecret, 60, 7002, at("2026-11-11T12:00:00Z")),
		"another secret":  Mint([]byte(strings.Repeat("s", 32)), 60, 7001, at("2026-11-11T12:00:00Z")),
	} {
		if other == first {
			t.Errorf("%s: the same ticket", name)
		}
	}
}

// TestTicketExpiry: a ticket is taken on its UTC day and the next one,
// from 24 to 48 hours, and never before its day.
func TestTicketExpiry(t *testing.T) {
	for _, c := range []struct {
		minted, checked string
		want            error
	}{
		{"2026-11-11T00:00:00Z", "2026-11-11T00:00:00Z", nil},
		{"2026-11-11T00:00:00Z", "2026-11-12T23:59:59Z", nil},
		{"2026-11-11T00:00:00Z", "2026-11-13T00:00:00Z", ErrExpired},
		{"2026-11-11T23:59:59Z", "2026-11-12T00:00:00Z", nil},
		{"2026-11-11T23:59:59Z", "2026-11-12T23:59:59Z", nil}, // 24 hours, the least
		{"2026-11-11T23:59:59Z", "2026-11-13T00:00:00Z", ErrExpired},
		{"2026-11-11T12:00:00Z", "2026-12-11T12:00:00Z", ErrExpired},
		{"2026-11-11T00:00:00Z", "2026-11-10T23:59:59Z", ErrExpired}, // of a day to come
	} {
		_, err := Verify(testSecret, Mint(testSecret, 60, 7001, at(c.minted)), at(c.checked))
		if !errors.Is(err, c.want) || (c.want == nil && err != nil) {
			t.Errorf("minted %s, checked %s: %v, want %v", c.minted, c.checked, err, c.want)
		}
	}
}

// TestTicketInvalid: what the secret did not sign is not a ticket, expired
// or not.
func TestTicketInvalid(t *testing.T) {
	now := at("2026-11-11T12:00:00Z")
	good := Mint(testSecret, 60, 7001, now)
	raw, _ := base64.RawURLEncoding.DecodeString(good)
	enc := base64.RawURLEncoding.EncodeToString
	changed := func(i int) string {
		b := bytes.Clone(raw)
		b[i] ^= 0x01
		return enc(b)
	}
	// A valid signature over build 0 or another version.
	signed := func(edit func(b []byte)) string {
		b := bytes.Clone(raw[:bodySize])
		edit(b)
		return enc(append(b, mac(testSecret, "pluma ios update ticket", b)...))
	}
	cases := map[string]string{
		"empty":                "",
		"short":                good[:len(good)-1],
		"long":                 good + "A",
		"not base64":           strings.Repeat("!", len(good)),
		"padded":               good[:len(good)-1] + "=",
		"another secret":       Mint([]byte(strings.Repeat("s", 32)), 60, 7001, now),
		"the version changed":  changed(0),
		"the build changed":    changed(4),
		"the day changed":      changed(8),
		"the session changed":  changed(12),
		"the signature":        changed(ticketSize - 1),
		"the subject's hash":   enc(append(bytes.Clone(raw[:bodySize]), mac(testSecret, "pluma ios update subject", raw[:bodySize])...)),
		"build 0, signed":      signed(func(b []byte) { binary.BigEndian.PutUint32(b[1:], 0) }),
		"a negative build":     signed(func(b []byte) { binary.BigEndian.PutUint32(b[1:], 1<<31) }),
		"version 2, signed":    signed(func(b []byte) { b[0] = 2 }),
		"an old day, unsigned": changed(5),
	}
	for name, ticket := range cases {
		if _, err := Verify(testSecret, ticket, now); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", name, err)
		}
	}
	if _, err := Verify(testSecret, good, now); err != nil {
		t.Fatal(err)
	}
}

func TestInstallURL(t *testing.T) {
	ticket := Mint(testSecret, 60, 7001, at("2026-11-11T12:00:00Z"))
	got := InstallURL("https://pluma.example/ios/adhoc/manifest", ticket)
	const prefix = "itms-services://?action=download-manifest&url="
	if !strings.HasPrefix(got, prefix) {
		t.Fatal(got)
	}
	// The manifest's address is one query value: its own "?" and "=" are
	// escaped.
	rest := strings.TrimPrefix(got, prefix)
	if strings.ContainsAny(rest, "?&=/:") {
		t.Errorf("the manifest's address is not escaped: %s", got)
	}
	manifest, err := url.QueryUnescape(rest)
	if err != nil || manifest != "https://pluma.example/ios/adhoc/manifest?u="+ticket {
		t.Errorf("%q, %v", manifest, err)
	}
}

func TestValidSecret(t *testing.T) {
	if ValidSecret("") || ValidSecret(strings.Repeat("a", 31)) || !ValidSecret(strings.Repeat("a", 32)) {
		t.Error("the least secret is 32 characters")
	}
}
