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

package appconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/proto"
)

var root = filepath.Join("..", "..", "..", "..", "..")

func mustParse(t *testing.T, data string) *AppConfig {
	t.Helper()
	c, err := Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse(%s): %v", data, err)
	}
	return c
}

// field returns the value of key in the config.
func field(t *testing.T, c *AppConfig, key string) *mtproto.JSONValue {
	t.Helper()
	for _, v := range c.Config.GetValue_VECTORJSONOBJECTVALUE() {
		if v.GetKey() == key {
			return v.GetValue()
		}
	}
	t.Fatalf("no %s in the config", key)
	return nil
}

func TestParse(t *testing.T) {
	c := mustParse(t, `{
		"limit": 30,
		"rate": 0.625,
		"blocked": true,
		"posting": "disabled",
		"order": ["a", 2, false, null],
		"success": {"🎯": {"value": 6}},
		"none": null,
		"empty": []
	}`)
	if c.Config.GetPredicateName() != mtproto.Predicate_jsonObject {
		t.Fatalf("config %s, want jsonObject", c.Config.GetPredicateName())
	}
	var keys []string
	for _, v := range c.Config.GetValue_VECTORJSONOBJECTVALUE() {
		keys = append(keys, v.GetKey())
	}
	if got := strings.Join(keys, ","); got != "limit,rate,blocked,posting,order,success,none,empty" {
		t.Errorf("keys %s, want the order of the file", got)
	}

	if v := field(t, c, "limit"); v.GetPredicateName() != mtproto.Predicate_jsonNumber || v.GetValue_FLOAT64() != 30 {
		t.Errorf("limit %v", v)
	}
	if v := field(t, c, "rate"); v.GetValue_FLOAT64() != 0.625 {
		t.Errorf("rate %v", v)
	}
	if v := field(t, c, "blocked"); v.GetPredicateName() != mtproto.Predicate_jsonBool || !mtproto.FromBool(v.GetValue_BOOL()) {
		t.Errorf("blocked %v", v)
	}
	if v := field(t, c, "posting"); v.GetPredicateName() != mtproto.Predicate_jsonString || v.GetValue_STRING() != "disabled" {
		t.Errorf("posting %v", v)
	}
	order := field(t, c, "order")
	if order.GetPredicateName() != mtproto.Predicate_jsonArray || len(order.GetValue_VECTORJSONVALUE()) != 4 {
		t.Fatalf("order %v", order)
	}
	for i, want := range []string{
		mtproto.Predicate_jsonString,
		mtproto.Predicate_jsonNumber,
		mtproto.Predicate_jsonBool,
		mtproto.Predicate_jsonNull,
	} {
		if got := order.GetValue_VECTORJSONVALUE()[i].GetPredicateName(); got != want {
			t.Errorf("order[%d] %s, want %s", i, got, want)
		}
	}
	success := field(t, c, "success").GetValue_VECTORJSONOBJECTVALUE()
	if len(success) != 1 || success[0].GetKey() != "🎯" ||
		success[0].GetValue().GetValue_VECTORJSONOBJECTVALUE()[0].GetValue().GetValue_FLOAT64() != 6 {
		t.Errorf("success %v", success)
	}
	if v := field(t, c, "none"); v.GetPredicateName() != mtproto.Predicate_jsonNull {
		t.Errorf("none %v", v)
	}
	if v := field(t, c, "empty"); v.GetPredicateName() != mtproto.Predicate_jsonArray || len(v.GetValue_VECTORJSONVALUE()) != 0 {
		t.Errorf("empty %v", v)
	}
}

func TestHash(t *testing.T) {
	a := mustParse(t, `{"limit": 30, "blocked": true}`)
	if a.Hash == 0 {
		t.Fatal("hash 0: the clients take it for no cached config")
	}
	if b := mustParse(t, "{\n  \"limit\":30,\n\t\"blocked\" : true\n}\n"); b.Hash != a.Hash {
		t.Errorf("reformatting changed the hash: %d, %d", a.Hash, b.Hash)
	}
	for _, data := range []string{
		`{"limit": 20, "blocked": true}`,
		`{"limit": 30, "blocked": false}`,
		`{"blocked": true, "limit": 30}`,
		`{"limit": 30, "blocked": true, "posting": "disabled"}`,
	} {
		if c := mustParse(t, data); c.Hash == a.Hash {
			t.Errorf("%s has the hash of the other config", data)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, data := range []string{
		``,
		`[]`,
		`"config"`,
		`{"limit": 30`,
		`{"limit": 30,}`,
		`{"limit": 30} {}`,
		`{"limit": 30, "limit": 20}`,
		`{"success": {"🎯": 1, "🎯": 2}}`,
	} {
		if _, err := Parse([]byte(data)); err == nil {
			t.Errorf("Parse(%s) accepted it", data)
		}
	}
}

func TestReply(t *testing.T) {
	c := mustParse(t, `{"premium_purchase_blocked": true, "dialog_filters_limit_premium": 30}`)
	for _, hash := range []int32{0, c.Hash + 1} {
		r := c.Reply(hash)
		if r.GetPredicateName() != mtproto.Predicate_help_appConfig || r.GetHash() != c.Hash {
			t.Fatalf("hash %d: %s with hash %d, want help.appConfig with %d", hash, r.GetPredicateName(), r.GetHash(), c.Hash)
		}
		if !proto.Equal(r.GetConfig(), c.Config) {
			t.Errorf("hash %d: config %v, want %v", hash, r.GetConfig(), c.Config)
		}
	}
	if r := c.Reply(c.Hash); r.GetPredicateName() != mtproto.Predicate_help_appConfigNotModified {
		t.Errorf("the client's hash: %s, want help.appConfigNotModified", r.GetPredicateName())
	}
}

// The config the server ships: it parses, keeps Premium purchases off
// (ADR 0016) and says what the server checks for the privacy setting of
// task 014.
func TestShippedConfig(t *testing.T) {
	c, err := Load(filepath.Join(root, "teamgramd", "bin", "app_config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if v := field(t, c, "premium_purchase_blocked"); !mtproto.FromBool(v.GetValue_BOOL()) {
		t.Error("premium_purchase_blocked is not true")
	}
	if v := field(t, c, "new_noncontact_peers_require_premium_without_ownpremium"); v.GetPredicateName() != mtproto.Predicate_jsonBool || mtproto.FromBool(v.GetValue_BOOL()) {
		t.Errorf("new_noncontact_peers_require_premium_without_ownpremium %v, want false", v)
	}
}

// Every key the server sends is in the table of docs/ops/app-config.md with
// the clients that read it and why it has its value.
func TestShippedConfigDocumented(t *testing.T) {
	c, err := Load(filepath.Join(root, "teamgramd", "bin", "app_config.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile(filepath.Join(root, "docs", "ops", "app-config.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range c.Config.GetValue_VECTORJSONOBJECTVALUE() {
		if !strings.Contains(string(doc), "| `"+v.GetKey()+"` |") {
			t.Errorf("%s is not in the table of docs/ops/app-config.md", v.GetKey())
		}
	}
}
