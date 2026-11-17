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

package staticmap

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// publicKey is the access key in the path of the public map tiles: 16 to
// 128 characters of base64url (MAPS_PUBLIC_KEY, docs/ops/maps.md; the
// generated one is 32).
var publicKey = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

// Public is the public serving of the vector map tiles (task 112, ADR 0070):
// the base the clients' interactive maps load their styles, tiles, glyphs and
// sprites from, and the style ids, one per language of the labels, under
// <URL>/styles/<id>/style.json and the dark <URL>/styles/<id>-dark/style.json.
// The app config hands both to the clients.
type Public struct {
	// URL is https://<host>/<key>, without a trailing slash.
	URL string
	// Styles are the ids of the light styles, as MAPS_STYLES lists them.
	Styles []string
}

// Enabled reports whether the tiles are served to the clients.
func (p Public) Enabled() bool { return p.URL != "" }

// Public parses PublicURL with Styles. An empty PublicURL is a zero Public:
// the clients get no interactive map.
func (c Config) Public() (Public, error) {
	raw := strings.TrimSpace(c.PublicURL)
	if raw == "" {
		return Public{}, nil
	}
	base, err := parsePublicURL(raw)
	if err != nil {
		return Public{}, err
	}
	styles, err := parseStyles(c.Styles)
	if err != nil {
		return Public{}, err
	}
	return Public{URL: base, Styles: styles}, nil
}

// parsePublicURL accepts https://<host>/<key> and nothing else: no other
// scheme, no credentials, no query or fragment, no trailing slash and a key
// of publicKey as the only segment of the path. The value is not in the
// error: it holds the key.
func parsePublicURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Hostname() == "" || u.User != nil ||
		strings.ContainsAny(raw, "?#") || !strings.HasPrefix(u.Path, "/") ||
		!publicKey.MatchString(u.Path[1:]) || u.RawPath != "" {
		return "", errors.New("staticmap: MAPS_PUBLIC_URL must be https://<PLUMA_MAPS_DOMAIN>/<MAPS_PUBLIC_KEY>, the key 16-128 characters of A-Z, a-z, 0-9, '-' or '_', without a trailing slash, query or fragment (docs/ops/maps.md)")
	}
	return "https://" + u.Host + u.Path, nil
}
