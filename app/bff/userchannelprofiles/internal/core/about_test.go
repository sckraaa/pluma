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

package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 70 letters of Cyrillic are 140 bytes: the bio counts characters.
func TestAboutTooLong(t *testing.T) {
	for _, c := range []struct {
		about string
		want  bool
	}{
		{strings.Repeat("я", 70), false},
		{strings.Repeat("я", 71), true},
		{strings.Repeat("a", 70), false},
		{strings.Repeat("😀", 70), false},
		{"", false},
	} {
		if got := aboutTooLong(c.about, false); got != c.want {
			t.Errorf("%d runes: %v", len([]rune(c.about)), got)
		}
	}
}

// The clients limit the bio by help.getAppConfig: the app config must give
// the limits the server checks.
func TestAboutLimitsAreTheAppConfigs(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "teamgramd", "bin", "app_config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	for key, limit := range map[string]int{
		"about_length_limit_default": AboutLimit,
		"about_length_limit_premium": AboutLimitPremium,
	} {
		if v, ok := config[key].(float64); !ok || v != float64(limit) {
			t.Errorf("%s of app_config.json %v, the server's limit %d", key, config[key], limit)
		}
	}
}
