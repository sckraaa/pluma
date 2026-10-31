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
	"slices"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/zeromicro/go-zero/core/jsonx"
)

// BIRTHDAY_SETUP is pending until the account has a birthday or dismissed
// the suggestion.
func TestPendingSuggestions(t *testing.T) {
	for _, c := range []struct {
		hasBirthday bool
		dismissed   []string
		want        []string
	}{
		{false, nil, []string{"BIRTHDAY_SETUP"}},
		{false, []string{"BIRTHDAY_CONTACTS_TODAY"}, []string{"BIRTHDAY_SETUP"}},
		{false, []string{"BIRTHDAY_SETUP"}, []string{}},
		{true, nil, []string{}},
	} {
		if got := PendingSuggestions(c.hasBirthday, c.dismissed); !slices.Equal(got, c.want) {
			t.Errorf("birthday %v, dismissed %v: %v, want %v", c.hasBirthday, c.dismissed, got, c.want)
		}
	}
}

func TestValidSuggestion(t *testing.T) {
	for name, want := range map[string]bool{
		"BIRTHDAY_CONTACTS_TODAY": true,
		"PREMIUM_GRACE":           true,
		"":                        false,
		"birthday_setup":          false,
		"BIRTHDAY SETUP":          false,
	} {
		if got := ValidSuggestion(name); got != want {
			t.Errorf("%q: %v", name, got)
		}
	}
}

// params are initConnection params as the session stores them: the
// JSONValue marshalled with jsonx (sess.MainAuthWrapper.onInitConnection).
func params(t *testing.T, fields ...*mtproto.JSONObjectValue) string {
	t.Helper()
	s, err := jsonx.MarshalToString(mtproto.MakeTLJsonObject(&mtproto.JSONValue{Value_VECTORJSONOBJECTVALUE: fields}).To_JSONValue())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func field(key string, value *mtproto.JSONValue) *mtproto.JSONObjectValue {
	return mtproto.MakeTLJsonObjectValue(&mtproto.JSONObjectValue{Key: key, Value: value}).To_JSONObjectValue()
}

func number(v float64) *mtproto.JSONValue {
	return mtproto.MakeTLJsonNumber(&mtproto.JSONValue{Value_FLOAT64: v}).To_JSONValue()
}

// The time zone of a client is the tz_offset of its params, UTC without
// one or with one out of range.
func TestClientZone(t *testing.T) {
	str := mtproto.MakeTLJsonString(&mtproto.JSONValue{Value_STRING: "10800"}).To_JSONValue()
	for name, c := range map[string]struct {
		params string
		offset int
	}{
		"Moscow":        {params(t, field("device_token", str), field("tz_offset", number(10800))), 10800},
		"New York":      {params(t, field("tz_offset", number(-5*3600))), -5 * 3600},
		"Kiritimati":    {params(t, field("tz_offset", number(14*3600))), 14 * 3600},
		"no params":     {"null", 0},
		"empty":         {"", 0},
		"no tz_offset":  {params(t, field("package_id", str)), 0},
		"a string":      {params(t, field("tz_offset", str)), 0},
		"out of range":  {params(t, field("tz_offset", number(15*3600))), 0},
		"not a JSON":    {"{", 0},
		"not an object": {`{"predicate_name":"jsonNull"}`, 0},
	} {
		zone := ClientZone(c.params)
		if _, offset := time.Date(2026, time.October, 31, 12, 0, 0, 0, zone).Zone(); offset != c.offset {
			t.Errorf("%s: offset %d, want %d", name, offset, c.offset)
		}
	}
}
