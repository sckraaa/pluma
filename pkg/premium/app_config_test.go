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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Everybody has the Premium limits and no feature waits for boosts (ADR
// 0041): every *_default limit equals its *_premium one, every *_level_min
// is 0, and the client is told Premium cannot be bought.
func TestAppConfigOpensEverything(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "teamgramd", "bin", "app_config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	for key, value := range config {
		switch {
		case strings.HasSuffix(key, "_default"):
			if premium, ok := config[strings.TrimSuffix(key, "_default")+"_premium"]; ok && premium != value {
				t.Errorf("%s is %v, its _premium %v", key, value, premium)
			}
		case strings.HasSuffix(key, "_level_min"):
			if value != float64(0) {
				t.Errorf("%s is %v, want 0", key, value)
			}
		}
	}
	for _, key := range []string{
		"channel_color_level_min", "channel_bg_icon_level_min", "channel_profile_bg_icon_level_min",
		"channel_emoji_status_level_min", "channel_wallpaper_level_min", "channel_custom_wallpaper_level_min",
		"channel_restrict_sponsored_level_min", "channel_autotranslation_level_min",
		"group_transcribe_level_min", "group_emoji_stickers_level_min", "group_profile_bg_icon_level_min",
		"group_emoji_status_level_min", "group_wallpaper_level_min", "group_custom_wallpaper_level_min",
	} {
		if _, ok := config[key]; !ok {
			t.Errorf("%s is missing: the clients fall back to the level Telegram wants", key)
		}
	}
	if config["premium_purchase_blocked"] != true {
		t.Errorf("premium_purchase_blocked is %v", config["premium_purchase_blocked"])
	}
	if config["upload_max_fileparts_default"] != float64(8000) {
		t.Errorf("upload_max_fileparts_default is %v, want the parts of 4 GB", config["upload_max_fileparts_default"])
	}
}
