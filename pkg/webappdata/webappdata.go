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

// Package webappdata builds the signed data the server gives to a bot's
// pages (task 106, ADR 0068): the initData of a mini app, which the client
// passes to the page in the tgWebAppData of its address, and the data of a
// login on the bot's site, which goes to the site in the query of its
// address. Both are checked by the bot with its token, as with Telegram;
// the initData also carries an Ed25519 signature third parties check with
// the server's public key.
package webappdata

import (
	"bytes"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// User is a WebAppUser (core.telegram.org/bots/webapps#webappuser), in the
// order of the fields of that page; the JSON is a field of the initData.
type User struct {
	Id                    int64  `json:"id"`
	IsBot                 bool   `json:"is_bot,omitempty"`
	FirstName             string `json:"first_name"`
	LastName              string `json:"last_name,omitempty"`
	Username              string `json:"username,omitempty"`
	LanguageCode          string `json:"language_code,omitempty"`
	IsPremium             bool   `json:"is_premium,omitempty"`
	AddedToAttachmentMenu bool   `json:"added_to_attachment_menu,omitempty"`
	AllowsWriteToPm       bool   `json:"allows_write_to_pm,omitempty"`
	PhotoUrl              string `json:"photo_url,omitempty"`
}

// Chat is a WebAppChat (core.telegram.org/bots/webapps#webappchat).
type Chat struct {
	Id       int64  `json:"id"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	Username string `json:"username,omitempty"`
	PhotoUrl string `json:"photo_url,omitempty"`
}

// InitData is a WebAppInitData (core.telegram.org/bots/webapps#webappinitdata)
// without hash and signature, which Sign adds. Empty fields are left out.
type InitData struct {
	QueryId      string // query_id: the web view query, for answerWebAppQuery
	User         *User
	Receiver     *User // the other user of a private chat (attachment menu)
	Chat         *Chat // the group, supergroup or channel (attachment menu)
	ChatType     string
	ChatInstance int64 // as in callback queries; 0: none
	StartParam   string
	CanSendAfter int32 // seconds; 0: none
	AuthDate     int64 // Unix time
}

// fields returns the fields of d, encoded as they are sent.
func (d *InitData) fields() (url.Values, error) {
	v := url.Values{}
	if d.QueryId != "" {
		v.Set("query_id", d.QueryId)
	}
	for _, o := range []struct {
		name string
		val  any
		set  bool
	}{
		{"user", d.User, d.User != nil},
		{"receiver", d.Receiver, d.Receiver != nil},
		{"chat", d.Chat, d.Chat != nil},
	} {
		if !o.set {
			continue
		}
		s, err := marshal(o.val)
		if err != nil {
			return nil, fmt.Errorf("webappdata: %s: %w", o.name, err)
		}
		v.Set(o.name, s)
	}
	if d.ChatType != "" {
		v.Set("chat_type", d.ChatType)
	}
	if d.ChatInstance != 0 {
		v.Set("chat_instance", strconv.FormatInt(d.ChatInstance, 10))
	}
	if d.StartParam != "" {
		v.Set("start_param", d.StartParam)
	}
	if d.CanSendAfter != 0 {
		v.Set("can_send_after", strconv.FormatInt(int64(d.CanSendAfter), 10))
	}
	if d.AuthDate <= 0 {
		return nil, errors.New("webappdata: no auth_date")
	}
	v.Set("auth_date", strconv.FormatInt(d.AuthDate, 10))
	return v, nil
}

// marshal is JSON without the HTML escapes of encoding/json, as a page reads
// it with JSON.parse (telegram-web-app.js).
func marshal(v any) (string, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}

// dataCheckString is the data-check-string of core.telegram.org/bots/webapps
// and /widgets/login-legacy: every field but those skipped, sorted by name,
// as key=<value>, joined by '\n'.
func dataCheckString(v url.Values, skip ...string) string {
	keys := make([]string, 0, len(v))
	for k := range v {
		skipped := false
		for _, s := range skip {
			skipped = skipped || k == s
		}
		if !skipped {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(v.Get(k))
	}
	return b.String()
}

func hmacHex(key []byte, data string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

// signaturePrefix starts the string the signature of an initData is made
// over: "<bot_id>:WebAppData\n" (core.telegram.org/bots/webapps, "Validating
// data for Third-Party Use").
func signaturePrefix(botId int64) string {
	return strconv.FormatInt(botId, 10) + ":WebAppData\n"
}

// Sign returns the initData of d for the bot botId, its query string as the
// page reads it: the fields, then signature (Ed25519 by signer over the
// fields without hash and signature), then hash (HMAC-SHA-256 under
// webAppKey, botkeys.Keys.WebApp, over all the fields but hash, signature
// included). Both definitions of core.telegram.org/bots/webapps hold only
// in this order (ADR 0068, "Документация").
func Sign(d *InitData, botId int64, webAppKey []byte, signer *Signer) (string, error) {
	if len(webAppKey) == 0 {
		return "", errors.New("webappdata: no web app key")
	}
	if signer == nil {
		return "", errors.New("webappdata: no signing key")
	}
	v, err := d.fields()
	if err != nil {
		return "", err
	}
	v.Set("signature", signer.sign(signaturePrefix(botId)+dataCheckString(v)))
	v.Set("hash", hmacHex(webAppKey, dataCheckString(v, "hash")))
	return v.Encode(), nil
}

// ErrInvalid is Verify and VerifyLogin for data that is not signed as it
// must be.
var ErrInvalid = errors.New("webappdata: invalid signature")

// Verify checks an initData as a bot and a third party would: hash under
// webAppKey and signature under publicKey (nil: not checked). It returns
// the fields.
func Verify(initData string, botId int64, webAppKey []byte, publicKey ed25519.PublicKey) (url.Values, error) {
	v, err := url.ParseQuery(initData)
	if err != nil {
		return nil, err
	}
	want, err := hex.DecodeString(v.Get("hash"))
	if err != nil {
		return nil, ErrInvalid
	}
	mac := hmac.New(sha256.New, webAppKey)
	mac.Write([]byte(dataCheckString(v, "hash")))
	if !hmac.Equal(mac.Sum(nil), want) {
		return nil, ErrInvalid
	}
	if publicKey != nil {
		sig, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(v.Get("signature"), "="))
		if err != nil || !ed25519.Verify(publicKey, []byte(signaturePrefix(botId)+dataCheckString(v, "hash", "signature")), sig) {
			return nil, ErrInvalid
		}
	}
	return v, nil
}
