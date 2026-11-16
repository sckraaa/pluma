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
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strconv"
)

// Login is the data of a login on a bot's site in the format of the login
// widget (core.telegram.org/widgets/login-legacy, "Receiving authorization
// data"), which a login button (bots/api LoginUrl) also gives the site.
// Empty optional fields are left out.
type Login struct {
	Id        int64
	FirstName string
	LastName  string
	Username  string
	PhotoUrl  string
	AuthDate  int64 // Unix time
}

// fields returns the fields of l without hash.
func (l *Login) fields() (url.Values, error) {
	if l.Id == 0 || l.AuthDate <= 0 {
		return nil, errors.New("webappdata: a login needs id and auth_date")
	}
	v := url.Values{}
	v.Set("id", strconv.FormatInt(l.Id, 10))
	v.Set("auth_date", strconv.FormatInt(l.AuthDate, 10))
	for name, val := range map[string]string{
		"first_name": l.FirstName,
		"last_name":  l.LastName,
		"username":   l.Username,
		"photo_url":  l.PhotoUrl,
	} {
		if val != "" {
			v.Set(name, val)
		}
	}
	return v, nil
}

// SignLogin returns the fields of l with hash, hex(HMAC_SHA256(
// data_check_string, loginKey)), where loginKey is botkeys.Keys.Login,
// SHA256(token) (core.telegram.org/widgets/login-legacy, "Checking
// authorization"). The caller adds them to the query of the site's
// address.
func SignLogin(l *Login, loginKey []byte) (url.Values, error) {
	if len(loginKey) == 0 {
		return nil, errors.New("webappdata: no login key")
	}
	v, err := l.fields()
	if err != nil {
		return nil, err
	}
	v.Set("hash", hmacHex(loginKey, dataCheckString(v)))
	return v, nil
}

// VerifyLogin checks the fields of a login as the site would, with
// loginKey.
func VerifyLogin(v url.Values, loginKey []byte) error {
	want, err := hex.DecodeString(v.Get("hash"))
	if err != nil {
		return ErrInvalid
	}
	mac := hmac.New(sha256.New, loginKey)
	mac.Write([]byte(dataCheckString(v, "hash")))
	if !hmac.Equal(mac.Sum(nil), want) {
		return ErrInvalid
	}
	return nil
}
