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

package conf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/teamgram/teamgram-server/pkg/env2"
)

// TestLinkHost: the links the server builds (invites, boosts: env2.TDotMe)
// and those the clients build from help.getConfig's me_url_prefix are of
// one host (task 026).
func TestLinkHost(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "teamgramd/bin/config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Config struct {
			MeUrlPrefix string `json:"me_url_prefix"`
		} `json:"data2"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if want := "https://" + env2.TDotMe + "/"; config.Config.MeUrlPrefix != want {
		t.Errorf("me_url_prefix %q, the server builds %q", config.Config.MeUrlPrefix, want)
	}
}
