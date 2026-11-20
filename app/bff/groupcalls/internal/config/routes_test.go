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

package config

import (
	"slices"
	"testing"
)

// GROUPCALLS_ROUTES (task 116): the grammar of the countries of CDN_ROUTES
// (pkg/cdn/config_test.go), route names of the SFU.
func TestParseRoutes(t *testing.T) {
	rs, err := ParseRoutes(" RU, BY : ru ; * : direct ")
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || !slices.Equal(rs[0].Countries, []string{"RU", "BY"}) || !slices.Equal(rs[0].Routes, []string{"ru"}) ||
		rs[1].Countries != nil || !slices.Equal(rs[1].Routes, []string{"direct"}) || !rs.Countries() {
		t.Fatalf("%+v", rs)
	}
	for country, want := range map[string][]string{"RU": {"ru"}, "BY": {"ru"}, "DE": {"direct"}, "": {"direct"}} {
		if got := rs.For(country); !slices.Equal(got, want) {
			t.Errorf("%q: %v, want %v", country, got, want)
		}
	}
	if rs, err := ParseRoutes(""); err != nil || rs != nil || rs.For("RU") != nil || rs.Countries() {
		t.Fatalf("empty: %+v %v", rs, err)
	}
	if rs, err := ParseRoutes("*:ru,direct"); err != nil || !slices.Equal(rs.For("DE"), []string{"ru", "direct"}) || rs.Countries() {
		t.Fatalf("one record: %+v %v", rs, err)
	}
	for _, bad := range []string{
		"RU:ru",
		"*:direct;*:ru",
		"RU:ru;RU:direct;*:direct",
		"RU,RU:ru;*:direct",
		"ru:ru;*:direct",
		"RUS:ru;*:direct",
		"RU;*:direct",
		"RU:;*:direct",
		"RU:RU;*:direct",
		"RU:ru,ru;*:direct",
		"RU:ru x;*:direct",
		"*:a,b,c,d,e,f,g,h,i",
		"RU:ru;*:direct;",
	} {
		if rs, err := ParseRoutes(bad); err == nil {
			t.Errorf("%q accepted: %+v", bad, rs)
		}
	}
}
