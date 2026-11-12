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

package stories

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The clients count the stories, their captions and the stealth mode by
// help.getAppConfig: the app config must give what the server checks, and
// say that stories are posted.
func TestLimitsAreTheAppConfigs(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "teamgramd", "bin", "app_config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	for key, limit := range map[string]int{
		"story_expiring_limit_default":              MaxActive,
		"story_expiring_limit_premium":              MaxActive,
		"stories_sent_weekly_limit_default":         MaxWeekly,
		"stories_sent_weekly_limit_premium":         MaxWeekly,
		"stories_sent_monthly_limit_default":        MaxMonthly,
		"stories_sent_monthly_limit_premium":        MaxMonthly,
		"story_caption_length_limit_default":        MaxCaption,
		"story_caption_length_limit_premium":        MaxCaption,
		"stories_suggested_reactions_limit_default": MaxSuggestedReactions,
		"stories_suggested_reactions_limit_premium": MaxSuggestedReactions,
		"stories_pinned_to_top_count_max":           MaxPinnedToTop,
		"stories_stealth_past_period":               StealthPast,
		"stories_stealth_future_period":             StealthFuture,
		"stories_stealth_cooldown_period":           StealthCooldown,
	} {
		if v, ok := config[key].(float64); !ok || v != float64(limit) {
			t.Errorf("%s of app_config.json %v, the server's value %d", key, config[key], limit)
		}
	}
	for _, key := range []string{"stories_posting", "stories_entities"} {
		if config[key] != "enabled" {
			t.Errorf("%s of app_config.json %v, want enabled", key, config[key])
		}
	}
}
