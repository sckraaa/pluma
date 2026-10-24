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
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// jsonOf returns v as encoding/json would decode it.
func jsonOf(t *testing.T, v *mtproto.JSONValue) any {
	t.Helper()
	switch v.GetPredicateName() {
	case mtproto.Predicate_jsonObject:
		m := make(map[string]any)
		for _, kv := range v.GetValue_VECTORJSONOBJECTVALUE() {
			m[kv.GetKey()] = jsonOf(t, kv.GetValue())
		}
		return m
	case mtproto.Predicate_jsonArray:
		a := make([]any, 0, len(v.GetValue_VECTORJSONVALUE()))
		for _, e := range v.GetValue_VECTORJSONVALUE() {
			a = append(a, jsonOf(t, e))
		}
		return a
	case mtproto.Predicate_jsonString:
		return v.GetValue_STRING()
	case mtproto.Predicate_jsonNumber:
		return v.GetValue_FLOAT64()
	case mtproto.Predicate_jsonBool:
		return mtproto.FromBool(v.GetValue_BOOL())
	case mtproto.Predicate_jsonNull:
		return nil
	}
	t.Fatalf("JSON value %s", v.GetPredicateName())
	return nil
}

// The app config (task 015): help.getAppConfig gives the whole of
// teamgramd/bin/app_config.json, and with its hash help.appConfigNotModified.
func TestAppConfig(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	u := login(ctx, t, testPhones(t, 1)[0], "Config")

	data, err := os.ReadFile(filepath.Join("..", "..", "teamgramd", "bin", "app_config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	if err = json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}

	o, err := u.c.Invoke(ctx, &mtproto.TLHelpGetAppConfig61E3F854{
		Constructor: u.c.Constructor(mtproto.Predicate_help_getAppConfig61E3F854),
	})
	if err != nil {
		t.Fatalf("help.getAppConfig: %v", err)
	}
	r, ok := o.(*mtproto.TLHelpAppConfig)
	if !ok {
		t.Fatalf("help.getAppConfig returned %T, want help.appConfig", o)
	}
	if r.GetHash() == 0 {
		t.Error("help.appConfig with hash 0")
	}
	got := jsonOf(t, r.GetConfig())
	if !reflect.DeepEqual(got, any(want)) {
		t.Errorf("help.getAppConfig: %v, want app_config.json %v", got, want)
	}
	for key, v := range map[string]any{
		"premium_purchase_blocked":                                true,
		"dialog_filters_limit_premium":                            float64(30),
		"dialog_filters_chats_limit_premium":                      float64(200),
		"new_noncontact_peers_require_premium_without_ownpremium": false,
	} {
		if m, _ := got.(map[string]any); m[key] != v {
			t.Errorf("%s %v, want %v", key, m[key], v)
		}
	}

	o, err = u.c.Invoke(ctx, &mtproto.TLHelpGetAppConfig61E3F854{
		Constructor: u.c.Constructor(mtproto.Predicate_help_getAppConfig61E3F854),
		Hash:        r.GetHash(),
	})
	if err != nil {
		t.Fatalf("help.getAppConfig with the hash: %v", err)
	}
	if _, ok := o.(*mtproto.TLHelpAppConfigNotModified); !ok {
		t.Errorf("help.getAppConfig with the hash returned %T, want help.appConfigNotModified", o)
	}

	// The older constructor answers the same config, without a hash.
	raw, err := u.c.InvokeRaw(ctx, &mtproto.TLHelpGetAppConfig98914110{
		Constructor: mtproto.CRC32_help_getAppConfig98914110,
	})
	if err != nil {
		t.Fatalf("help.getAppConfig (JSONValue): %v", err)
	}
	d := mtproto.NewDecodeBuf(raw)
	obj := d.Object()
	old, ok := obj.(*mtproto.TLJsonObject)
	if err := d.GetError(); err != nil || !ok {
		t.Fatalf("help.getAppConfig (JSONValue) returned %T (%v), want jsonObject", obj, err)
	}
	if got := jsonOf(t, old.To_JSONValue()); !reflect.DeepEqual(got, any(want)) {
		t.Errorf("help.getAppConfig (JSONValue): %v, want app_config.json %v", got, want)
	}
}
