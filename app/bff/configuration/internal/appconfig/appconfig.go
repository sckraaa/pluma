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

// Package appconfig is the client configuration of help.getAppConfig: a JSON
// object kept as data next to config.json (teamgramd/bin/app_config.json,
// docs/ops/app-config.md).
package appconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"

	"github.com/teamgram/proto/mtproto"
)

type AppConfig struct {
	Config *mtproto.JSONValue // a jsonObject, keys in the order of the file
	Hash   int32
}

// Load reads and parses the file.
func Load(path string) (*AppConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// Parse makes the config of a JSON object. The hash is of the object without
// insignificant whitespace: a change of a key or a value gives the clients a
// new config, reformatting the file does not.
func Parse(data []byte) (*AppConfig, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	v, err := value(d)
	if err != nil {
		return nil, err
	}
	if v.GetPredicateName() != mtproto.Predicate_jsonObject {
		return nil, errors.New("the app config is not a JSON object")
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, errors.New("data after the JSON object")
	}

	var b bytes.Buffer
	if err := json.Compact(&b, data); err != nil {
		return nil, err
	}
	h := fnv.New32a()
	h.Write(b.Bytes())
	// 0 means "no config cached" to the clients.
	hash := int32(h.Sum32() & 0x7fffffff)
	if hash == 0 {
		hash = 1
	}
	return &AppConfig{Config: v, Hash: hash}, nil
}

// Reply is the answer to help.getAppConfig with the hash of the client's
// config.
func (c *AppConfig) Reply(hash int32) *mtproto.Help_AppConfig {
	if hash == c.Hash {
		return mtproto.MakeTLHelpAppConfigNotModified(&mtproto.Help_AppConfig{}).To_Help_AppConfig()
	}
	return mtproto.MakeTLHelpAppConfig(&mtproto.Help_AppConfig{
		Hash:   c.Hash,
		Config: c.Config,
	}).To_Help_AppConfig()
}

func value(d *json.Decoder) (*mtproto.JSONValue, error) {
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch t := t.(type) {
	case json.Delim:
		if t == '{' {
			return object(d)
		}
		return array(d)
	case string:
		return mtproto.MakeTLJsonString(&mtproto.JSONValue{Value_STRING: t}).To_JSONValue(), nil
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return nil, err
		}
		return mtproto.MakeTLJsonNumber(&mtproto.JSONValue{Value_FLOAT64: f}).To_JSONValue(), nil
	case bool:
		return mtproto.MakeTLJsonBool(&mtproto.JSONValue{Value_BOOL: mtproto.ToBool(t)}).To_JSONValue(), nil
	default:
		return mtproto.MakeTLJsonNull(&mtproto.JSONValue{}).To_JSONValue(), nil
	}
}

// object reads the rest of an object after its '{'. A key given twice is an
// error: the clients would apply the values in turn.
func object(d *json.Decoder) (*mtproto.JSONValue, error) {
	values := []*mtproto.JSONObjectValue{}
	seen := make(map[string]bool)
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		key := t.(string)
		if seen[key] {
			return nil, fmt.Errorf("key %q given twice", key)
		}
		seen[key] = true
		v, err := value(d)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		values = append(values, mtproto.MakeTLJsonObjectValue(&mtproto.JSONObjectValue{
			Key:   key,
			Value: v,
		}).To_JSONObjectValue())
	}
	if _, err := d.Token(); err != nil {
		return nil, err
	}
	return mtproto.MakeTLJsonObject(&mtproto.JSONValue{Value_VECTORJSONOBJECTVALUE: values}).To_JSONValue(), nil
}

// array reads the rest of an array after its '['.
func array(d *json.Decoder) (*mtproto.JSONValue, error) {
	values := []*mtproto.JSONValue{}
	for d.More() {
		v, err := value(d)
		if err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	if _, err := d.Token(); err != nil {
		return nil, err
	}
	return mtproto.MakeTLJsonArray(&mtproto.JSONValue{Value_VECTORJSONVALUE: values}).To_JSONValue(), nil
}
