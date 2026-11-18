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

package cdn

import (
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/conf"
	zeroconf "github.com/zeromicro/go-zero/core/conf"
)

// loadBffCdn loads the Cdn block of a bff config with the variables of env,
// every other variable empty, as the compose files pass them.
func loadBffCdn(t *testing.T, path string, env map[string]string) Config {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content, err := conf.ExpandEnv(data, func(name string) (string, bool) { return env[name], true })
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Cdn Config `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	}
	if err = zeroconf.LoadFromYamlBytes(content, &v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return v.Cdn
}

// TestBffConfigCdn: the Cdn block of both bff configs is off with empty
// variables, and with the URL and the keys of .env it is delivery 201 of
// Gcore with the defaults of the brief.
func TestBffConfigCdn(t *testing.T) {
	for _, f := range []string{"../../teamgramd/etc/bff.yaml", "../../teamgramd/etc2/bff.yaml"} {
		if set, err := loadBffCdn(t, f, nil).Parse(); err != nil || set != nil {
			t.Fatalf("%s with no variables: %v, %v", f, set, err)
		}
		set, err := loadBffCdn(t, f, map[string]string{
			"CDN_GCORE_BASE_URL":  "https://cdn.example.com",
			"CDN_GCORE_TOKEN_KEY": hex.EncodeToString(randomBytes(t, 16)),
			"CDN_ORIGIN_AUTH":     hex.EncodeToString(randomBytes(t, 32)),
		}).Parse()
		if err != nil || set == nil {
			t.Fatalf("%s: %v", f, err)
		}
		d, ok := set.Delivery(201)
		if !ok || d.Name != "gcore" || d.Source != SourceHomeOrigin || d.MonthlyBytes != DefaultMonthlyGB*1000*1000*1000 || d.InsecureHTTP {
			t.Fatalf("%s: delivery %+v", f, d)
		}
		if len(set.Routes) != 1 || len(set.Routes[0].Deliveries) != 1 || set.Routes[0].Deliveries[0] != 201 {
			t.Fatalf("%s: routes %+v", f, set.Routes)
		}
		if set.Origin.Listen != DefaultOriginListen || set.Origin.MaxConcurrent != DefaultOriginMax ||
			set.URLTTL != 6*time.Hour || set.HealthEvery != time.Minute || set.AvoidTTL != time.Hour ||
			set.MinFileSize != 1<<20 || len(set.Kinds) != 2 || set.DevAllowHTTP {
			t.Fatalf("%s: settings %+v", f, set)
		}
		if _, ok := set.Delivery(202); ok {
			t.Fatalf("%s: delivery 202 on without CDN_RU_BASE_URL", f)
		}

		// Delivery 202 of the Russian node, routed by country (task 109).
		env := map[string]string{
			"CDN_GCORE_BASE_URL":  "https://cdn.example.com",
			"CDN_GCORE_TOKEN_KEY": hex.EncodeToString(randomBytes(t, 16)),
			"CDN_ORIGIN_AUTH":     hex.EncodeToString(randomBytes(t, 32)),
			"CDN_RU_BASE_URL":     "https://r1.example.com",
			"CDN_RU_TOKEN_KEY":    hex.EncodeToString(randomBytes(t, 16)),
			"CDN_RU_ORIGIN_AUTH":  hex.EncodeToString(randomBytes(t, 32)),
			"CDN_RU_HEALTH_ADDR":  "10.78.0.1:443",
			"CDN_RU_MONTHLY_GB":   "2000",
			"CDN_ROUTES":          "RU,BY:202,201;*:201",
		}
		if set, err = loadBffCdn(t, f, env).Parse(); err != nil || set == nil {
			t.Fatalf("%s with delivery 202: %v", f, err)
		}
		ru, ok := set.Delivery(202)
		if !ok || ru.Name != "ru" || ru.Signer.Name() != SignerGcoreMD5 || ru.Source != SourceHomeOrigin || ru.HealthAddr != "10.78.0.1:443" ||
			string(ru.OriginAuth) != env["CDN_RU_ORIGIN_AUTH"] || ru.MonthlyBytes != 2000_000_000_000 {
			t.Fatalf("%s: delivery 202 %+v", f, ru)
		}
		if len(set.Routes) != 2 || len(set.Routes[0].Countries) != 2 || set.Routes[0].Deliveries[0] != 202 {
			t.Fatalf("%s: routes %+v", f, set.Routes)
		}
		// Without its own value 202 would share 201's: the start stops.
		env["CDN_RU_ORIGIN_AUTH"] = ""
		if _, err = loadBffCdn(t, f, env).Parse(); err == nil {
			t.Fatalf("%s: delivery 202 without CDN_RU_ORIGIN_AUTH accepted", f)
		}
	}
}
