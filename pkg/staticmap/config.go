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
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Defaults of the Maps block (ADR 0063, p. 7).
const (
	DefaultStyles           = "ru,en"
	DefaultTimeout          = 10 * time.Second
	DefaultConcurrency      = 4
	DefaultCacheTTL         = 6 * time.Hour
	DefaultPerUserPerMinute = 60
)

// Bounds of the settings Validate accepts.
const (
	minTimeout          = time.Second
	maxTimeout          = time.Minute
	maxConcurrency      = 32
	minCacheTTL         = time.Minute
	maxCacheTTL         = 7 * 24 * time.Hour
	maxPerUserPerMinute = 10000
	maxStyles           = 8
)

// styleName is a style id of the maps service: it goes into the path of the
// render URL as is.
var styleName = regexp.MustCompile(`^[a-z-]{2,10}$`)

// Config is the Maps block of the bff configuration. Every setting is a
// string so that an empty environment variable (MAPS_TIMEOUT= in .env, the
// compose default) means the default instead of failing the whole config:
// go-zero refuses "" for numbers and durations.
type Config struct {
	// URL is the base of the maps service (tileserver-gl), as
	// http://maps:8080. Empty: maps are off and inputWebFileGeoPointLocation
	// is answered LOCATION_INVALID.
	URL string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Styles are the style ids the service renders, comma separated, one per
	// language of the labels (DefaultStyles). StyleFor picks one per session.
	Styles string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Timeout bounds one render, and separately the wait for a free render
	// slot ("10s").
	Timeout string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Concurrency is how many renders one bff runs at once ("4").
	Concurrency string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// CacheTTL is how long a rendered map is kept in Redis ("6h").
	CacheTTL string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// PerUserPerMinute is how many maps that are not in the cache one user
	// may have rendered per minute ("60").
	PerUserPerMinute string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// settings is a parsed Config.
type settings struct {
	url              string
	styles           []string
	timeout          time.Duration
	concurrency      int
	cacheTTL         time.Duration
	perUserPerMinute int
}

// Enabled reports whether maps are configured.
func (c Config) Enabled() bool { return strings.TrimSpace(c.URL) != "" }

// Validate checks an enabled configuration; a disabled one is valid.
func (c Config) Validate() error {
	if !c.Enabled() {
		return nil
	}
	_, err := c.parse()
	return err
}

func (c Config) parse() (settings, error) {
	var (
		s   settings
		err error
	)
	if s.url, err = parseBaseURL(c.URL); err != nil {
		return settings{}, err
	}
	if s.styles, err = parseStyles(c.Styles); err != nil {
		return settings{}, err
	}
	if s.timeout, err = parseDuration("MAPS_TIMEOUT", c.Timeout, DefaultTimeout, minTimeout, maxTimeout); err != nil {
		return settings{}, err
	}
	if s.cacheTTL, err = parseDuration("MAPS_CACHE_TTL", c.CacheTTL, DefaultCacheTTL, minCacheTTL, maxCacheTTL); err != nil {
		return settings{}, err
	}
	if s.concurrency, err = parseCount("MAPS_CONCURRENCY", c.Concurrency, DefaultConcurrency, maxConcurrency); err != nil {
		return settings{}, err
	}
	if s.perUserPerMinute, err = parseCount("MAPS_PER_USER_PER_MINUTE", c.PerUserPerMinute, DefaultPerUserPerMinute, maxPerUserPerMinute); err != nil {
		return settings{}, err
	}
	return s, nil
}

// parseBaseURL accepts an http(s) URL of a host without a path, query or
// credentials, and returns it without a trailing slash.
func parseBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", errors.New("staticmap: MAPS_URL must be an http(s) URL of the maps service without a path, as http://maps:8080 (docs/ops/maps.md)")
	}
	return u.Scheme + "://" + u.Host, nil
}

func parseStyles(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		raw = DefaultStyles
	}
	var styles []string
	seen := map[string]bool{}
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if !styleName.MatchString(s) || seen[s] {
			return nil, fmt.Errorf("staticmap: MAPS_STYLES %q must be distinct style ids of 2-10 letters a-z or '-', comma separated (docs/ops/maps.md)", raw)
		}
		seen[s] = true
		styles = append(styles, s)
	}
	if len(styles) > maxStyles {
		return nil, fmt.Errorf("staticmap: MAPS_STYLES lists more than %d styles (docs/ops/maps.md)", maxStyles)
	}
	return styles, nil
}

func parseDuration(name, raw string, def, lo, hi time.Duration) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < lo || d > hi {
		return 0, fmt.Errorf("staticmap: %s %q must be a duration from %s to %s (docs/ops/maps.md)", name, raw, lo, hi)
	}
	return d, nil
}

func parseCount(name, raw string, def, hi int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > hi {
		return 0, fmt.Errorf("staticmap: %s %q must be a whole number from 1 to %d (docs/ops/maps.md)", name, raw, hi)
	}
	return n, nil
}
