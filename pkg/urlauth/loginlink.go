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

package urlauth

import (
	"net/url"
	"strings"
)

// loginFields are the fields of a login a site gets
// (core.telegram.org/widgets/login-legacy); a field of the same name already
// in the query of its address is replaced.
var loginFields = map[string]bool{
	"id": true, "first_name": true, "last_name": true, "username": true, "photo_url": true, "auth_date": true, "hash": true,
}

// LoginLink returns link with the fields of a login (webappdata.SignLogin)
// added to its query, after the site's own fields, in their order. The
// fragment stays. A login button and a link of url_auth_domains open it in
// the app; the login widget format of oauth.pluma.chat redirects the browser
// to it.
func LoginLink(link string, fields url.Values) (string, error) {
	u, err := url.Parse(link)
	if err != nil {
		return "", err
	}
	kept := make([]string, 0, 4)
	if u.RawQuery != "" {
		for _, pair := range strings.Split(u.RawQuery, "&") {
			key, _, _ := strings.Cut(pair, "=")
			if name, err := url.QueryUnescape(key); err == nil && loginFields[name] {
				continue
			}
			if pair != "" {
				kept = append(kept, pair)
			}
		}
	}
	kept = append(kept, fields.Encode())
	u.RawQuery = strings.Join(kept, "&")
	u.ForceQuery = false
	return u.String(), nil
}
