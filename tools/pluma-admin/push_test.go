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

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teamgram/teamgram-server/pkg/conf"
)

// TestLoadsPushConfig: push check reads the provider part of push.yaml.
func TestLoadsPushConfig(t *testing.T) {
	t.Setenv("MYSQL_PASSWORD", "pw")
	t.Setenv("PUSH_SECRET_ENCRYPTION_KEY", testHmacKey)
	t.Setenv("APNS_KEY_ID", "KEYID12345")
	t.Setenv("APNS_TEAM_ID", "TEAMID1234")
	t.Setenv("APNS_KEY_FILE", "/keys/apns.p8")
	t.Setenv("FCM_CREDENTIALS_FILE", "/keys/fcm.json")
	for _, f := range []string{"../../teamgramd/etc/push.yaml", "../../teamgramd/etc2/push.yaml"} {
		var c pushConfig
		if err := conf.Load(f, &c); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if c.APNs.KeyId != "KEYID12345" || c.APNs.TeamId != "TEAMID1234" || c.APNs.Topic != "chat.pluma.app" ||
			c.APNs.ProductionURL != "https://api.push.apple.com" || c.APNs.SandboxURL != "https://api.sandbox.push.apple.com" ||
			c.FCM.CredentialsFile == "" || c.FCM.Endpoint != "https://fcm.googleapis.com" {
			t.Fatalf("%s: unexpected config %+v", f, c)
		}
	}
}

// Without keys push check says the providers are off and succeeds; usage
// errors are usage errors.
func TestPushCheckOff(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "push.yaml")
	if err := os.WriteFile(cfg, []byte("APNs:\n  KeyFile: "+empty+"\nFCM:\n  CredentialsFile: "+empty+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"-f", cfg, "push", "check"}, &out, &errOut, nil); code != 0 ||
		!strings.Contains(out.String(), "APNs: off") || !strings.Contains(out.String(), "FCM: off") {
		t.Fatalf("push check without keys: exit %d\n%s%s", code, out.String(), errOut.String())
	}
	if code := run([]string{"-f", cfg, "push"}, &out, &errOut, nil); code != 2 {
		t.Fatalf("push without check: exit %d", code)
	}
}
