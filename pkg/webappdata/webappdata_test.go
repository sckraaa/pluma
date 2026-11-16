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

package webappdata

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/teamgram/teamgram-server/pkg/botkeys"
)

const (
	testToken = "123456:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsaw"
	testBotId = 123456
	// testSeed is bytes 0..31.
	testSeed = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
)

func testSigner(t *testing.T) *Signer {
	t.Helper()
	s, err := NewSigner(testSeed)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func testInitData() *InitData {
	return &InitData{
		QueryId: "AAHdF6IQAAAAAN0XohDhrOrc",
		User: &User{
			Id: 279058397, FirstName: "Vladislav Ö", LastName: "Kibenko", Username: "vdkfrost",
			LanguageCode: "ru", IsPremium: true, AllowsWriteToPm: true,
			PhotoUrl: "https://pluma.chat/_l/avatar/vdkfrost",
		},
		ChatInstance: -8213651434,
		StartParam:   "ref_7",
		CanSendAfter: 2,
		AuthDate:     1662771648,
	}
}

// The vector was computed apart from this package by a Python script on
// the text of core.telegram.org/bots/webapps: hmac and hashlib for hash,
// the Ed25519 of the cryptography package for signature,
// urllib.parse.urlencode for the encoding.
const wantInitData = "auth_date=1662771648&can_send_after=2&chat_instance=-8213651434" +
	"&hash=78a098edb8746d5e6743e95514f287b932dd8dde003bd8096b247f9187adc956" +
	"&query_id=AAHdF6IQAAAAAN0XohDhrOrc" +
	"&signature=0Y_WjB5dSRE2kTazZVWOw76t7KJ8oxCQocntT7o_zB1v7s6t7wZ2zb9vT0VSjjCIh9YVxRC78GwoBkE2dNP2Cg" +
	"&start_param=ref_7" +
	"&user=%7B%22id%22%3A279058397%2C%22first_name%22%3A%22Vladislav+%C3%96%22%2C%22last_name%22%3A%22Kibenko%22" +
	"%2C%22username%22%3A%22vdkfrost%22%2C%22language_code%22%3A%22ru%22%2C%22is_premium%22%3Atrue" +
	"%2C%22allows_write_to_pm%22%3Atrue%2C%22photo_url%22%3A%22https%3A%2F%2Fpluma.chat%2F_l%2Favatar%2Fvdkfrost%22%7D"

func TestSignVector(t *testing.T) {
	s := testSigner(t)
	if got := s.PublicKeyHex(); got != "03a107bff3ce10be1d70dd18e74bc09967e4d6309ba50d5f1ddc8664125531b8" {
		t.Errorf("public key %s", got)
	}
	got, err := Sign(testInitData(), testBotId, botkeys.Derive(testToken).WebApp, s)
	if err != nil {
		t.Fatal(err)
	}
	if got != wantInitData {
		t.Errorf("initData\n got %s\nwant %s", got, wantInitData)
	}
}

// TestSignIndependently checks hash and signature by the algorithm of the
// page written out again here: hash over every field but hash, signature
// included; signature over "<bot_id>:WebAppData\n" and the fields without
// hash and signature.
func TestSignIndependently(t *testing.T) {
	s := testSigner(t)
	raw, err := Sign(testInitData(), testBotId, botkeys.Derive(testToken).WebApp, s)
	if err != nil {
		t.Fatal(err)
	}
	v, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	pairs := func(skip ...string) string {
		var out []string
		for _, k := range []string{"auth_date", "can_send_after", "chat_instance", "hash", "query_id", "signature", "start_param", "user"} {
			skipped := false
			for _, sk := range skip {
				skipped = skipped || sk == k
			}
			if !skipped {
				out = append(out, k+"="+v.Get(k))
			}
		}
		return strings.Join(out, "\n")
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(testToken))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(pairs("hash")))
	if hex.EncodeToString(mac.Sum(nil)) != v.Get("hash") {
		t.Error("hash is not HMAC-SHA-256 over the fields with signature")
	}
	sig, err := base64.RawURLEncoding.DecodeString(v.Get("signature"))
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(s.PublicKey(), []byte("123456:WebAppData\n"+pairs("hash", "signature")), sig) {
		t.Error("signature does not verify over the fields without hash")
	}
	if !strings.HasPrefix(v.Get("user"), `{"id":279058397,"first_name":"Vladislav Ö","last_name":"Kibenko","username":"vdkfrost","language_code":"ru","is_premium":true,`) {
		t.Errorf("user JSON %s", v.Get("user"))
	}
	if strings.Contains(v.Get("user"), `&`) || strings.Contains(v.Get("user"), "is_bot") {
		t.Errorf("user JSON escapes HTML or sends false flags: %s", v.Get("user"))
	}
}

func TestVerify(t *testing.T) {
	s := testSigner(t)
	key := botkeys.Derive(testToken).WebApp
	raw, err := Sign(testInitData(), testBotId, key, s)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := Verify(raw, testBotId, key, s.PublicKey()); err != nil || v.Get("start_param") != "ref_7" {
		t.Fatalf("verify: %v, %v", v, err)
	}
	other := botkeys.Derive("123456:another").WebApp
	if _, err := Verify(raw, testBotId, other, s.PublicKey()); !errors.Is(err, ErrInvalid) {
		t.Errorf("another token: %v", err)
	}
	// The signature names the bot: another bot id does not verify.
	if _, err := Verify(raw, testBotId+1, key, s.PublicKey()); !errors.Is(err, ErrInvalid) {
		t.Errorf("another bot: %v", err)
	}
	tampered := strings.Replace(raw, "start_param=ref_7", "start_param=ref_8", 1)
	if _, err := Verify(tampered, testBotId, key, nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("tampered: %v", err)
	}
}

func TestInitDataFields(t *testing.T) {
	s := testSigner(t)
	key := botkeys.Derive(testToken).WebApp
	d := &InitData{
		User:     &User{Id: 1, FirstName: "A"},
		Receiver: &User{Id: 2, FirstName: "B", IsBot: true},
		Chat:     &Chat{Id: -100, Type: "supergroup", Title: "G"},
		ChatType: "supergroup",
		AuthDate: 10,
	}
	raw, err := Sign(d, testBotId, key, s)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := url.ParseQuery(raw)
	for name, want := range map[string]string{
		"user":      `{"id":1,"first_name":"A"}`,
		"receiver":  `{"id":2,"is_bot":true,"first_name":"B"}`,
		"chat":      `{"id":-100,"type":"supergroup","title":"G"}`,
		"chat_type": "supergroup",
		"auth_date": "10",
	} {
		if v.Get(name) != want {
			t.Errorf("%s = %q, want %q", name, v.Get(name), want)
		}
	}
	for _, absent := range []string{"query_id", "chat_instance", "start_param", "can_send_after"} {
		if v.Has(absent) {
			t.Errorf("empty %s is sent", absent)
		}
	}
	if _, err := Sign(&InitData{User: d.User}, testBotId, key, s); err == nil {
		t.Error("signed without auth_date")
	}
	if _, err := Sign(d, testBotId, nil, s); err == nil {
		t.Error("signed without a key")
	}
	if _, err := Sign(d, testBotId, key, nil); err == nil {
		t.Error("signed without a signer")
	}
}

// The hash of the legacy login was computed by the same Python script:
// hmac.new(sha256(token), data_check_string, sha256).
func TestSignLogin(t *testing.T) {
	key := botkeys.Derive(testToken).Login
	v, err := SignLogin(&Login{
		Id: 279058397, FirstName: "Vladislav", Username: "vdkfrost",
		PhotoUrl: "https://pluma.chat/_l/avatar/vdkfrost", AuthDate: 1662771648,
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Get("hash"); got != "7dd1a7ce2e1e03c4bd7f0934b9a0af69b51c9323c938d47669311ad74d475543" {
		t.Errorf("hash %s", got)
	}
	if v.Has("last_name") {
		t.Error("an empty last_name is sent")
	}
	if err := VerifyLogin(v, key); err != nil {
		t.Errorf("verify: %v", err)
	}
	v.Set("id", "1")
	if err := VerifyLogin(v, key); !errors.Is(err, ErrInvalid) {
		t.Errorf("tampered: %v", err)
	}
	if _, err := SignLogin(&Login{Id: 1}, key); err == nil {
		t.Error("signed without auth_date")
	}
}

func TestSignerAndVersions(t *testing.T) {
	for _, k := range []string{"", "  "} {
		if s, err := (Config{SigningKey: k}).Signer(); s != nil || err != nil {
			t.Errorf("%q: %v, %v", k, s, err)
		}
	}
	for _, k := range []string{"xyz", testSeed[:60]} {
		if _, err := (Config{SigningKey: k}).Signer(); err == nil {
			t.Errorf("%q accepted", k)
		}
	}
	v := Versions{Android: "9.1", TDesktop: "9.0", Unknown: "7.0"}
	for platform, want := range map[string]string{"android": "9.1", "tdesktop": "9.0", "ios": "7.0", "other": "7.0", "ANDROID": "9.1"} {
		if got := v.For(platform); got != want {
			t.Errorf("%s: %s, want %s", platform, got, want)
		}
	}
	if got := (Versions{}).For("web"); got != defaultVersion {
		t.Errorf("empty config: %s", got)
	}
}
