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

package ringtones

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/teamgram/teamgram-server/pkg/contactnotes"
)

// The clients check a sound and cut a note by the limits of
// help.getAppConfig before they send them: the app config must give the
// limits the server keeps.
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
		"ringtone_size_max":         SizeMax,
		"ringtone_duration_max":     DurationMax,
		"ringtone_saved_count_max":  SavedCountMax,
		"contact_note_length_limit": contactnotes.LengthMax,
	} {
		if v, ok := config[key].(float64); !ok || v != float64(limit) {
			t.Errorf("%s of app_config.json %v, the server's limit %d", key, config[key], limit)
		}
	}
}

func TestIsMP3(t *testing.T) {
	for mime, want := range map[string]bool{
		"audio/mpeg": true, "audio/mp3": true, "audio/mpeg3": true,
		"audio/ogg": false, "audio/m4a": false, "video/mpeg": false, "": false,
	} {
		if IsMP3(mime) != want {
			t.Errorf("IsMP3(%q) = %v, want %v", mime, !want, want)
		}
	}
}
