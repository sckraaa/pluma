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

package premium

import (
	"reflect"
	"testing"
)

// fakeQuerier answers the row of the users query.
type fakeQuerier struct {
	premium bool
	until   int64
	rows    int
}

func (f *fakeQuerier) QueryRowsPartial(v any, _ string, _ ...any) error {
	out := reflect.ValueOf(v).Elem()
	if f.rows == 0 {
		return nil
	}
	row := reflect.New(out.Type().Elem()).Elem()
	row.FieldByName("Premium").SetBool(f.premium)
	row.FieldByName("Until").SetInt(f.until)
	out.Set(reflect.Append(out, row))
	return nil
}

func TestActive(t *testing.T) {
	if !Active(true, 0, 100) || !Active(true, 101, 100) {
		t.Error("a Premium in force")
	}
	if Active(true, 100, 100) || Active(false, 0, 100) {
		t.Error("an ended or absent Premium")
	}
}

func TestChannelsLimit(t *testing.T) {
	for _, tc := range []struct {
		name         string
		q            *fakeQuerier
		now          int64
		wantChannels int
		wantPublic   int
	}{
		{"free", &fakeQuerier{rows: 1}, 100, ChannelsLimitFree, PublicLimitFree},
		{"premium without an end", &fakeQuerier{rows: 1, premium: true}, 100, ChannelsLimitPremium, PublicLimitPremium},
		{"premium in force", &fakeQuerier{rows: 1, premium: true, until: 200}, 100, ChannelsLimitPremium, PublicLimitPremium},
		{"expired premium", &fakeQuerier{rows: 1, premium: true, until: 50}, 100, ChannelsLimitFree, PublicLimitFree},
		{"no user", &fakeQuerier{}, 100, ChannelsLimitFree, PublicLimitFree},
	} {
		got, err := ChannelsLimit(tc.q, 1, tc.now)
		if err != nil || got != tc.wantChannels {
			t.Errorf("%s: channels %d %v", tc.name, got, err)
		}
		got, err = PublicChannelsLimit(tc.q, 1, tc.now)
		if err != nil || got != tc.wantPublic {
			t.Errorf("%s: public %d %v", tc.name, got, err)
		}
	}
}
