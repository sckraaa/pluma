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
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The clients take the limits of folders from help.getAppConfig: the app
// config must give the limits the server checks.
func TestFolderLimitsAreTheAppConfigs(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "..", "teamgramd", "bin", "app_config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	for key, limit := range map[string]int{
		"dialog_filters_limit_default":       DialogFiltersLimit,
		"dialog_filters_limit_premium":       DialogFiltersLimitPremium,
		"dialog_filters_chats_limit_default": FilterChatsLimit,
		"dialog_filters_chats_limit_premium": FilterChatsLimitPremium,
	} {
		if v, ok := config[key].(float64); !ok || v != float64(limit) {
			t.Errorf("%s of app_config.json %v, the server's limit %d", key, config[key], limit)
		}
	}
}
