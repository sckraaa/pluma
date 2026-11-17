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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/teamgram/proto/mtproto"

	"github.com/zeromicro/go-zero/core/logx"
)

type AppConfig struct {
	Config *mtproto.JSONValue // a jsonObject, keys in the order of the file
	Hash   int32

	// compact is what the hash is of: the file without insignificant
	// whitespace, and the keys set over it at the start (withStrings).
	compact []byte
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
	return &AppConfig{Config: v, Hash: hashOf(b.Bytes()), compact: b.Bytes()}, nil
}

// hashOf is the hash of a config, never 0: 0 means "no config cached" to
// the clients.
func hashOf(parts ...[]byte) int32 {
	h := fnv.New32a()
	for _, p := range parts {
		h.Write(p)
	}
	hash := int32(h.Sum32() & 0x7fffffff)
	if hash == 0 {
		hash = 1
	}
	return hash
}

// urlAuthDomainsKey is the list of the domains whose links the clients open
// through messages.requestUrlAuth (core.telegram.org/api/config,
// url_auth_domains; read by tdesktop core/ui_integration.cpp:100, Android
// MessagesController.java:3033, iOS UrlHandling.swift:1654).
const urlAuthDomainsKey = "url_auth_domains"

// WithURLAuthDomains returns the config with url_auth_domains set to domains,
// the login domains of bots (task 106, ADR 0068 p. 6), and a hash of its
// own; c itself for no domains, which the file already says as []. The
// other keys keep their order and values.
func (c *AppConfig) WithURLAuthDomains(domains []string) *AppConfig {
	if len(domains) == 0 {
		return c
	}
	list := make([]*mtproto.JSONValue, 0, len(domains))
	for _, d := range domains {
		list = append(list, mtproto.MakeTLJsonString(&mtproto.JSONValue{Value_STRING: d}).To_JSONValue())
	}
	value := mtproto.MakeTLJsonObjectValue(&mtproto.JSONObjectValue{
		Key:   urlAuthDomainsKey,
		Value: mtproto.MakeTLJsonArray(&mtproto.JSONValue{Value_VECTORJSONVALUE: list}).To_JSONValue(),
	}).To_JSONObjectValue()
	old := c.Config.GetValue_VECTORJSONOBJECTVALUE()
	values := make([]*mtproto.JSONObjectValue, 0, len(old)+1)
	replaced := false
	for _, v := range old {
		if v.GetKey() == urlAuthDomainsKey {
			v, replaced = value, true
		}
		values = append(values, v)
	}
	if !replaced {
		values = append(values, value)
	}
	return &AppConfig{
		Config:  mtproto.MakeTLJsonObject(&mtproto.JSONValue{Value_VECTORJSONOBJECTVALUE: values}).To_JSONValue(),
		Hash:    hashOf(c.compact, []byte("\n"+urlAuthDomainsKey+"="+strings.Join(domains, ","))),
		compact: c.compact,
	}
}

// The switches of the translation of messages (task 101, ADR 0064, section
// 9). "enabled" shows the translate button and the translation bar of a
// chat; "disabled" hides them. A client without the key decides by itself:
// Android takes "enabled" (MessagesController.java:1741-1742 at 7be672ea)
// and would ask a server without an engine, iOS takes "disabled"
// (AccountContext/Sources/AccountContext.swift:2254-2255 at 24638bc69b),
// Web K translates only on "enabled" (hooks/usePeerTranslation.ts:27 at
// aa74832e). tdesktop does not read them (lang/translate_provider.cpp:34-45
// at v7.4.1).
const (
	translationsManualKey = "translations_manual_enabled"
	translationsAutoKey   = "translations_auto_enabled"
	translationsDisabled  = "disabled"
)

// WithoutTranslation returns the config with both switches of translation
// set to "disabled", added when the file lacks them, and a hash of its own:
// the config of a server with no translation engine, whatever the file says
// (ADR 0064, section 9). The other keys keep their order and values.
func (c *AppConfig) WithoutTranslation() *AppConfig {
	return c.withStrings([]stringKey{
		{translationsManualKey, translationsDisabled},
		{translationsAutoKey, translationsDisabled},
	})
}

// gifSearchEmojiesKey is the emoji categories of the GIF tab of the emoji
// panel, each a query to the GIF bot (Android MessagesController.java:3090,
// iOS ChatEntityKeyboardInputNode.swift:233, tdesktop
// stickers_list_footer.cpp:137).
const gifSearchEmojiesKey = "gif_search_emojies"

// WithGifSearchEmojies returns the config with gif_search_emojies set to
// emojies, in place when the file has the key and at the end otherwise, and
// a hash of its own: the config of a server whose @gif searches by text
// (task 099, ADR 0062, p. 2). The file keeps the list empty, which is the
// config without a search. The other keys keep their order and values.
func (c *AppConfig) WithGifSearchEmojies(emojies []string) *AppConfig {
	list := make([]*mtproto.JSONValue, 0, len(emojies))
	for _, e := range emojies {
		list = append(list, mtproto.MakeTLJsonString(&mtproto.JSONValue{Value_STRING: e}).To_JSONValue())
	}
	value := mtproto.MakeTLJsonObjectValue(&mtproto.JSONObjectValue{
		Key:   gifSearchEmojiesKey,
		Value: mtproto.MakeTLJsonArray(&mtproto.JSONValue{Value_VECTORJSONVALUE: list}).To_JSONValue(),
	}).To_JSONObjectValue()
	values := make([]*mtproto.JSONObjectValue, 0, len(c.Config.GetValue_VECTORJSONOBJECTVALUE())+1)
	values = append(values, c.Config.GetValue_VECTORJSONOBJECTVALUE()...)
	replaced := false
	for i, v := range values {
		if v.GetKey() == gifSearchEmojiesKey {
			values[i], replaced = value, true
		}
	}
	if !replaced {
		values = append(values, value)
	}
	compact := append([]byte(nil), c.compact...)
	compact = append(compact, "\n"+gifSearchEmojiesKey+"="+strings.Join(emojies, ",")...)
	return &AppConfig{
		Config:  mtproto.MakeTLJsonObject(&mtproto.JSONValue{Value_VECTORJSONOBJECTVALUE: values}).To_JSONValue(),
		Hash:    hashOf(compact),
		compact: compact,
	}
}

// The keys of the public map tiles of the clients' interactive maps (task
// 112, ADR 0070).
const (
	// tdesktopConfigMapKey is an object of strings (tdesktop
	// main/main_app_config.cpp:471-493 at v7.4.1: one value of another
	// type and tdesktop ignores the whole object).
	tdesktopConfigMapKey = "tdesktop_config_map"
	// plumaMapsURLKey and plumaMapsStylesKey are Pluma's own, read by the
	// patched Android app: the base of the tiles and the ids of the light
	// styles there (each also has a dark <id>-dark).
	plumaMapsURLKey    = "pluma_maps_url"
	plumaMapsStylesKey = "pluma_maps_styles"
)

// tdesktopMapsKeys are the keys of tdesktop_config_map that tdesktop hands
// its location picker as the maps token: "maps" for the location of a
// message, a poll and an Instant View (inline_bots/bot_attach_web_view.cpp
// :286-296, boxes/create_poll_box.cpp:2593-2601 and :2676-2684,
// iv/editor/iv_editor_session.cpp:234-244 at v7.4.1), "bmaps" for the
// address of a business (settings/business/settings_location.cpp:71-81).
// The picker shows only with a token (ui/controls/location_picker.cpp
// :343-351); the patched picker takes it for the base of the tiles. "geo"
// and "bgeo", the token of the geocoder, are not set: there is none.
var tdesktopMapsKeys = []string{"maps", "bmaps"}

// WithMaps returns the config with the public map tiles at url and the
// light styles there, and a hash of its own: tdesktop_config_map with
// tdesktopMapsKeys set to url (the file's other keys of it kept),
// pluma_maps_url and pluma_maps_styles. Each is set in place when the file
// has it and at the end otherwise; the other keys keep their order and
// values.
func (c *AppConfig) WithMaps(url string, styles []string) *AppConfig {
	var entries []*mtproto.JSONObjectValue
	for _, v := range c.Config.GetValue_VECTORJSONOBJECTVALUE() {
		if v.GetKey() == tdesktopConfigMapKey && v.GetValue().GetPredicateName() == mtproto.Predicate_jsonObject {
			entries = append(entries, v.GetValue().GetValue_VECTORJSONOBJECTVALUE()...)
		}
	}
	for _, key := range tdesktopMapsKeys {
		entry := mtproto.MakeTLJsonObjectValue(&mtproto.JSONObjectValue{Key: key, Value: jsonString(url)}).To_JSONObjectValue()
		replaced := false
		for i, e := range entries {
			if e.GetKey() == key {
				entries[i], replaced = entry, true
			}
		}
		if !replaced {
			entries = append(entries, entry)
		}
	}
	list := make([]*mtproto.JSONValue, 0, len(styles))
	for _, s := range styles {
		list = append(list, jsonString(s))
	}
	return c.withValues([]keyValue{
		{
			key:   tdesktopConfigMapKey,
			value: mtproto.MakeTLJsonObject(&mtproto.JSONValue{Value_VECTORJSONOBJECTVALUE: entries}).To_JSONValue(),
			text:  strings.Join(tdesktopMapsKeys, ",") + "=" + url,
		},
		{key: plumaMapsURLKey, value: jsonString(url), text: url},
		{
			key:   plumaMapsStylesKey,
			value: mtproto.MakeTLJsonArray(&mtproto.JSONValue{Value_VECTORJSONVALUE: list}).To_JSONValue(),
			text:  strings.Join(styles, ","),
		},
	})
}

// jsonString is s as a JSON value.
func jsonString(s string) *mtproto.JSONValue {
	return mtproto.MakeTLJsonString(&mtproto.JSONValue{Value_STRING: s}).To_JSONValue()
}

// stringKey is a key of the config and the string it is set to.
type stringKey struct{ key, value string }

// withStrings returns the config with each key set to its string (see
// withValues).
func (c *AppConfig) withStrings(set []stringKey) *AppConfig {
	values := make([]keyValue, 0, len(set))
	for _, s := range set {
		values = append(values, keyValue{key: s.key, value: jsonString(s.value), text: s.value})
	}
	return c.withValues(values)
}

// keyValue is a key of the config, the value it is set to, and the text of
// that value the hash covers.
type keyValue struct {
	key   string
	value *mtproto.JSONValue
	text  string
}

// withValues returns the config with each key set to its value, in place
// when the key is there and at the end otherwise. The hash covers the file
// and the keys set, so the clients that cached the config of the file take
// this one, and a later change of the config (WithURLAuthDomains) still
// counts what is set here.
func (c *AppConfig) withValues(set []keyValue) *AppConfig {
	values := make([]*mtproto.JSONObjectValue, 0, len(c.Config.GetValue_VECTORJSONOBJECTVALUE())+len(set))
	values = append(values, c.Config.GetValue_VECTORJSONOBJECTVALUE()...)
	compact := append([]byte(nil), c.compact...)
	for _, s := range set {
		value := mtproto.MakeTLJsonObjectValue(&mtproto.JSONObjectValue{
			Key:   s.key,
			Value: s.value,
		}).To_JSONObjectValue()
		replaced := false
		for i, v := range values {
			if v.GetKey() == s.key {
				values[i], replaced = value, true
			}
		}
		if !replaced {
			values = append(values, value)
		}
		compact = append(compact, "\n"+s.key+"="+s.text...)
	}
	return &AppConfig{
		Config:  mtproto.MakeTLJsonObject(&mtproto.JSONValue{Value_VECTORJSONOBJECTVALUE: values}).To_JSONValue(),
		Hash:    hashOf(compact),
		compact: compact,
	}
}

// Live is the config of the file with what comes from the database:
// url_auth_domains, the login domains of bots, read again at most every
// refresh period, so that a domain set with pluma-admin reaches the clients
// with their next help.getAppConfig.
type Live struct {
	base    *AppConfig
	domains func(ctx context.Context) ([]string, error)
	every   time.Duration
	now     func() time.Time

	mu    sync.Mutex
	cur   *AppConfig
	until time.Time
}

// Timings of Live: how long a read of the domains may take, and how soon a
// failed one is tried again (the last config read stays meanwhile).
const (
	domainsTimeout    = 3 * time.Second
	domainsRetryAfter = 5 * time.Second
)

// NewLive returns the config of base with the domains read by domains, at
// most every period.
func NewLive(base *AppConfig, domains func(ctx context.Context) ([]string, error), every time.Duration) *Live {
	return &Live{base: base, domains: domains, every: every, now: time.Now}
}

// Current returns the config to answer with. A failed read of the domains
// keeps the config read last, the file's at first, and is logged.
func (l *Live) Current(ctx context.Context) *AppConfig {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if l.cur != nil && now.Before(l.until) {
		return l.cur
	}
	qctx, cancel := context.WithTimeout(ctx, domainsTimeout)
	defer cancel()
	domains, err := l.domains(qctx)
	if err != nil {
		logx.WithContext(ctx).Errorf("app config: login domains of bots: %v", err)
		if l.cur == nil {
			l.cur = l.base
		}
		l.until = now.Add(domainsRetryAfter)
		return l.cur
	}
	l.cur, l.until = l.base.WithURLAuthDomains(domains), now.Add(l.every)
	return l.cur
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
