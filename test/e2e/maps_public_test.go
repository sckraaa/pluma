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

//go:build e2e

package e2e

import (
	"context"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestAppConfigMapsKeys: the public map tiles of the clients' maps (task
// 112, ADR 0070) reach them through the app config only when bff has
// MAPS_PUBLIC_URL: tdesktop_config_map with the tokens of the Desktop picker,
// and pluma_maps_url and pluma_maps_styles of Android. A wrong URL stops bff
// instead of handing the clients a map that never loads.
func TestAppConfigMapsKeys(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	u := login(ctx, t, testPhones(t, 1)[0], "Maps")

	// The stand has no public tiles: none of the keys.
	cfg := appConfigOf(ctx, t, u)
	for _, key := range []string{"tdesktop_config_map", "pluma_maps_url", "pluma_maps_styles"} {
		if v, ok := cfg[key]; ok {
			t.Fatalf("without MAPS_PUBLIC_URL the config has %s: %v", key, v)
		}
	}

	t.Cleanup(func() { restore(t) })
	const url = "https://maps.e2e.pluma.test/E2eMapsKey0123456789_-abcdefgh"
	// The shell's variables win over the env file of run.sh.
	upBff := func(env ...string) ([]byte, error) {
		cmd := exec.Command(runScript, "compose", "up", "--detach", "--no-deps", "--wait", "--wait-timeout", "180", "bff")
		cmd.Env = append(os.Environ(), env...)
		return cmd.CombinedOutput()
	}
	if out, err := upBff("MAPS_PUBLIC_URL="+url, "MAPS_STYLES=en,ru"); err != nil {
		t.Fatalf("bff with MAPS_PUBLIC_URL: %v\n%s", err, out)
	}
	waitServes(ctx, t, u)
	cfg = appConfigOf(ctx, t, u)
	if got, want := cfg["tdesktop_config_map"], any(map[string]any{"maps": url, "bmaps": url}); !reflect.DeepEqual(got, want) {
		t.Errorf("tdesktop_config_map %v, want %v", got, want)
	}
	if got := cfg["pluma_maps_url"]; got != url {
		t.Errorf("pluma_maps_url %v, want %s", got, url)
	}
	if got, want := cfg["pluma_maps_styles"], any([]any{"en", "ru"}); !reflect.DeepEqual(got, want) {
		t.Errorf("pluma_maps_styles %v, want %v", got, want)
	}

	// A URL without the key, or not https, stops bff with the reason; the
	// key is not in its log.
	for _, bad := range []string{"https://maps.e2e.pluma.test", "http://maps.e2e.pluma.test/E2eMapsKey0123456789_-abcdefgh"} {
		if out, err := upBff("MAPS_PUBLIC_URL=" + bad); err == nil {
			t.Fatalf("bff started with MAPS_PUBLIC_URL %s\n%s", bad, out)
		}
		logs, err := exec.Command(runScript, "compose", "logs", "--no-log-prefix", "--tail", "50", "bff").CombinedOutput()
		if err != nil {
			t.Fatalf("bff logs: %v\n%s", err, logs)
		}
		if !strings.Contains(string(logs), "MAPS_PUBLIC_URL must be https://") {
			t.Errorf("bff with MAPS_PUBLIC_URL %s: no reason in its log:\n%s", bad, logs)
		}
		if strings.Contains(string(logs), "E2eMapsKey0123456789") {
			t.Errorf("bff logged the key of MAPS_PUBLIC_URL")
		}
	}

	restore(t)
	waitServes(ctx, t, u)
	if cfg = appConfigOf(ctx, t, u); cfg["pluma_maps_url"] != nil {
		t.Errorf("with the stand's env again: pluma_maps_url %v", cfg["pluma_maps_url"])
	}
}
