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

package cdn

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Config is the HTTPS CDN of the home in bff.yaml (task 107, ADR 0071,
// docs/ops/cdn.md). Every value but the id of a delivery is a string, so
// that an empty variable of the environment takes the default; Parse checks
// them. A delivery without BaseURL is off; with none on, the CDN is off:
// upload.getFile never redirects and the origin does not listen.
type Config struct {
	// Deliveries are the CDNs files may be redirected to.
	Deliveries []DeliveryConfig `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Routes is CDN_ROUTES ("*:201" when empty), see ParseRoutes.
	Routes string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Origin is where the CDNs pull the cipher from.
	Origin OriginConfig `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// UrlTTL is how long a signed URL works, CDN_URL_TTL ("6h"): whole
	// seconds from 10s to 24h.
	UrlTTL string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// PrepareRateMbit bounds the reads of files for their hashes, megabits a
	// second (CDN_PREPARE_RATE_MBIT, "800").
	PrepareRateMbit string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// MinFileSize is CDN_MIN_FILE_SIZE, a multiple of 1 MiB ("1048576").
	MinFileSize string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Kinds are the kinds of files that go through the CDN, CDN_KINDS
	// ("document,photo"); the files of secret chats never do.
	Kinds string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// DevAllowHTTP is CDN_DEV_ALLOW_HTTP: "1" lets a delivery have an
	// http:// BaseURL. Only the stand of the e2e tests sets it.
	DevAllowHTTP string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// HealthEvery is how often each delivery is checked, CDN_HEALTH_EVERY
	// ("1m").
	HealthEvery string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// AvoidTTL is how long a key avoids a delivery its client fell back
	// from, CDN_AVOID_TTL ("1h").
	AvoidTTL string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// DeliveryConfig is a delivery in Config.
type DeliveryConfig struct {
	// Id is the dc_id of the redirects, MinDeliveryID to MaxDeliveryID.
	Id   int32  `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	Name string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Signer is gcore-md5 (when empty) or path-hmac (CDN_GCORE_SIGNER).
	Signer string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// BaseURL is https://<host> of the CDN resource (CDN_GCORE_BASE_URL);
	// empty: the delivery is off.
	BaseURL string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Key signs the URLs (CDN_GCORE_TOKEN_KEY): for gcore-md5 the Secure
	// Token key, 6 to 32 characters; for path-hmac 32 bytes in base64.
	Key string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Source is home-origin (when empty).
	Source string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// MonthlyGB is the budget of a calendar month, GB of 10^9 bytes
	// (CDN_GCORE_MONTHLY_GB, "800").
	MonthlyGB string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// OriginAuth is the value of X-Pluma-Origin-Auth the CDN of this
	// delivery sends the origin (CDN_RU_ORIGIN_AUTH), 64 hex digits; empty:
	// Origin.AuthValue, CDN_ORIGIN_AUTH.
	OriginAuth string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// HealthAddr is host:port the checks of the delivery dial
	// (CDN_RU_HEALTH_ADDR); empty: the host of BaseURL.
	HealthAddr string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// OriginConfig is the origin in Config.
type OriginConfig struct {
	// Listen is host:port of the origin in bff, reached by Caddy over the
	// Docker network only (CDN_ORIGIN_LISTEN, "0.0.0.0:20480").
	Listen string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// AuthValue is what the CDN puts in X-Pluma-Origin-Auth (CDN_ORIGIN_AUTH),
	// 64 hex digits; required while a delivery that pulls from the origin
	// has no OriginAuth of its own.
	AuthValue string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// MaxConcurrent is how many requests the origin serves at once ("16").
	MaxConcurrent string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// Defaults of Config.
const (
	DefaultRoutes          = "*:201"
	DefaultOriginListen    = "0.0.0.0:20480"
	DefaultOriginMax       = 16
	DefaultURLTTL          = 6 * time.Hour
	DefaultPrepareRateMbit = 800
	DefaultKinds           = "document,photo"
	DefaultHealthEvery     = time.Minute
	DefaultAvoidTTL        = time.Hour
	DefaultMonthlyGB       = 800

	minURLTTL        = 10 * time.Second
	maxURLTTL        = 24 * time.Hour
	maxOriginMax     = 1024
	maxPrepareMbit   = 10000
	minHealthEvery   = time.Second
	maxHealthEvery   = time.Hour
	minAvoidTTL      = time.Second
	maxAvoidTTL      = 24 * time.Hour
	maxMonthlyGB     = 1 << 20
	originAuthDigits = 64
)

// Settings are a parsed Config.
type Settings struct {
	// Deliveries are the enabled ones, in the order of the config.
	Deliveries []Delivery
	// Routes are CDN_ROUTES; every id names one of Deliveries.
	Routes []Route
	Origin OriginSettings

	URLTTL                time.Duration
	PrepareBytesPerSecond int64
	MinFileSize           int64
	// Kinds are the kinds of files that go through the CDN.
	Kinds        []FileKind
	DevAllowHTTP bool
	HealthEvery  time.Duration
	AvoidTTL     time.Duration
}

// OriginSettings are the parsed OriginConfig.
type OriginSettings struct {
	// Listen is empty when no delivery pulls from the origin: it does not
	// listen then.
	Listen string
	// AuthValue is CDN_ORIGIN_AUTH, the value of X-Pluma-Origin-Auth of
	// the deliveries without their own; empty when every delivery has its
	// own. The origin takes the OriginAuth of the deliveries.
	AuthValue Secret
	// MaxConcurrent is how many requests are served at once.
	MaxConcurrent int
}

// Secret is a value of the config that must not reach a log: fmt prints
// it as a placeholder.
type Secret string

func (Secret) String() string { return "[secret]" }

// GoString keeps %#v from printing the value.
func (Secret) GoString() string { return "[secret]" }

// Delivery returns the enabled delivery id.
func (s *Settings) Delivery(id int32) (*Delivery, bool) {
	i := slices.IndexFunc(s.Deliveries, func(d Delivery) bool { return d.ID == id })
	if i < 0 {
		return nil, false
	}
	return &s.Deliveries[i], true
}

// AllowsKind reports whether files of kind k go through the CDN.
func (s *Settings) AllowsKind(k FileKind) bool { return slices.Contains(s.Kinds, k) }

// Enabled reports whether a delivery is configured.
func (c Config) Enabled() bool {
	return slices.ContainsFunc(c.Deliveries, func(d DeliveryConfig) bool { return strings.TrimSpace(d.BaseURL) != "" })
}

// Parse checks an enabled Config and returns its settings; nil for a
// disabled one, whose values are not looked at. A wrong value stops the
// start rather than leave the CDN half on.
func (c Config) Parse() (*Settings, error) {
	if !c.Enabled() {
		return nil, nil
	}
	s := &Settings{}
	var err error
	if s.DevAllowHTTP, err = flagOf(c.DevAllowHTTP); err != nil {
		return nil, fmt.Errorf("cdn: CDN_DEV_ALLOW_HTTP %q: %w", c.DevAllowHTTP, err)
	}
	if s.URLTTL, err = durationOr(c.UrlTTL, DefaultURLTTL); err != nil || s.URLTTL < minURLTTL || s.URLTTL > maxURLTTL || s.URLTTL%time.Second != 0 {
		return nil, fmt.Errorf("cdn: CDN_URL_TTL %q: whole seconds from %v to %v", c.UrlTTL, minURLTTL, maxURLTTL)
	}
	if s.HealthEvery, err = durationOr(c.HealthEvery, DefaultHealthEvery); err != nil || s.HealthEvery < minHealthEvery || s.HealthEvery > maxHealthEvery {
		return nil, fmt.Errorf("cdn: CDN_HEALTH_EVERY %q: a duration from %v to %v", c.HealthEvery, minHealthEvery, maxHealthEvery)
	}
	if s.AvoidTTL, err = durationOr(c.AvoidTTL, DefaultAvoidTTL); err != nil || s.AvoidTTL < minAvoidTTL || s.AvoidTTL > maxAvoidTTL {
		return nil, fmt.Errorf("cdn: CDN_AVOID_TTL %q: a duration from %v to %v", c.AvoidTTL, minAvoidTTL, maxAvoidTTL)
	}
	mbit, err := intOr(c.PrepareRateMbit, DefaultPrepareRateMbit)
	if err != nil || mbit <= 0 || mbit > maxPrepareMbit {
		return nil, fmt.Errorf("cdn: CDN_PREPARE_RATE_MBIT %q: a number of megabits a second from 1 to %d", c.PrepareRateMbit, maxPrepareMbit)
	}
	s.PrepareBytesPerSecond = mbit * 1000 * 1000 / 8
	if s.MinFileSize, err = intOr(c.MinFileSize, DefaultMinFileSize); err != nil {
		return nil, fmt.Errorf("cdn: CDN_MIN_FILE_SIZE %q: %w", c.MinFileSize, err)
	}
	if err = ValidMinFileSize(s.MinFileSize); err != nil {
		return nil, err
	}
	if s.Kinds, err = parseKinds(c.Kinds); err != nil {
		return nil, err
	}

	var ids []int32
	var names []string
	for i := range c.Deliveries {
		d, err := c.Deliveries[i].parse(s)
		if err != nil {
			return nil, err
		}
		if slices.Contains(ids, d.ID) || slices.Contains(names, d.Name) {
			return nil, fmt.Errorf("cdn: two deliveries with id %d or name %q", d.ID, d.Name)
		}
		ids, names = append(ids, d.ID), append(names, d.Name)
		if d.BaseURL != "" {
			s.Deliveries = append(s.Deliveries, d)
		}
	}

	routes := strings.TrimSpace(c.Routes)
	if routes == "" {
		routes = DefaultRoutes
	}
	if s.Routes, err = ParseRoutes(routes); err != nil {
		return nil, err
	}
	for _, r := range s.Routes {
		for _, id := range r.Deliveries {
			if _, ok := s.Delivery(id); !ok {
				return nil, fmt.Errorf("cdn: CDN_ROUTES names delivery %d, which is unknown or off (no BaseURL)", id)
			}
		}
	}

	if err = c.Origin.parse(s); err != nil {
		return nil, err
	}
	if err = originAuths(s); err != nil {
		return nil, err
	}
	return s, nil
}

// originAuths gives each delivery that pulls from the origin its value of
// X-Pluma-Origin-Auth: its own, or CDN_ORIGIN_AUTH. The origin tells the
// deliveries apart by that value (task 109, "Выдача 202"): a path-hmac
// delivery also by the signature of its path, but every gcore-md5 one asks
// for the same /o/<hex>, so two of them with one value are refused, their
// bytes could not be counted to each.
func originAuths(s *Settings) error {
	byValue := map[Secret]int32{}
	for i := range s.Deliveries {
		d := &s.Deliveries[i]
		if d.Source != SourceHomeOrigin {
			continue
		}
		if d.OriginAuth == "" {
			d.OriginAuth = s.Origin.AuthValue
		}
		if _, signed := d.Signer.(*PathHMACSigner); signed {
			continue
		}
		if other, ok := byValue[d.OriginAuth]; ok {
			return fmt.Errorf("cdn: deliveries %d and %d both pull /o/ from the origin with one X-Pluma-Origin-Auth: give one of them its own OriginAuth", other, d.ID)
		}
		byValue[d.OriginAuth] = d.ID
	}
	return nil
}

// parse checks a delivery; it returns one with an empty BaseURL when it is
// off, whose other fields are not looked at.
func (c DeliveryConfig) parse(s *Settings) (Delivery, error) {
	d := Delivery{ID: c.Id, Name: strings.TrimSpace(c.Name)}
	if d.ID < MinDeliveryID || d.ID > MaxDeliveryID {
		return d, fmt.Errorf("cdn: delivery id %d: from %d to %d", d.ID, MinDeliveryID, MaxDeliveryID)
	}
	if d.Name == "" {
		return d, fmt.Errorf("cdn: delivery %d has no name", d.ID)
	}
	base := strings.TrimSpace(c.BaseURL)
	if base == "" {
		return d, nil
	}
	var err error
	if d.BaseURL, d.InsecureHTTP, err = parseBaseURL(base, s.DevAllowHTTP); err != nil {
		return d, fmt.Errorf("cdn: delivery %d: %w", d.ID, err)
	}
	if d.Source = strings.TrimSpace(c.Source); d.Source == "" {
		d.Source = SourceHomeOrigin
	}
	if d.Source != SourceHomeOrigin {
		return d, fmt.Errorf("cdn: delivery %d: source %q, only %s is supported", d.ID, d.Source, SourceHomeOrigin)
	}
	// The values are compared as they are written, and never printed.
	if c.OriginAuth != "" {
		if !validOriginAuth(c.OriginAuth) {
			return d, fmt.Errorf("cdn: delivery %d: OriginAuth must be %d hex digits (openssl rand -hex 32)", d.ID, originAuthDigits)
		}
		d.OriginAuth = Secret(c.OriginAuth)
	}
	if d.HealthAddr, err = parseHealthAddr(c.HealthAddr); err != nil {
		return d, fmt.Errorf("cdn: delivery %d: %w", d.ID, err)
	}
	gb, err := intOr(c.MonthlyGB, DefaultMonthlyGB)
	if err != nil || gb <= 0 || gb > maxMonthlyGB {
		return d, fmt.Errorf("cdn: delivery %d: MonthlyGB %q: a positive number of GB", d.ID, c.MonthlyGB)
	}
	d.MonthlyBytes = gb * 1000 * 1000 * 1000

	signer := strings.TrimSpace(c.Signer)
	if signer == "" {
		signer = SignerGcoreMD5
	}
	// The key is used as it is written, never printed.
	switch signer {
	case SignerGcoreMD5:
		d.Signer, err = NewGcoreSigner(d.BaseURL, c.Key, s.URLTTL)
	case SignerPathHMAC:
		var key []byte
		if key, err = ParsePathHMACKey(c.Key); err == nil {
			d.Signer, err = NewPathHMACSigner(d.BaseURL, key, s.URLTTL)
		}
	default:
		err = fmt.Errorf("cdn: signer %q, want %s or %s", signer, SignerGcoreMD5, SignerPathHMAC)
	}
	if err != nil {
		return d, fmt.Errorf("cdn: delivery %d: %w", d.ID, err)
	}
	return d, nil
}

// parseBaseURL checks the base of the URLs of a delivery: https:// and a
// host, nothing after it but a slash, which is dropped; http:// only with
// devHTTP. It reports whether the base is http://.
func parseBaseURL(base string, devHTTP bool) (string, bool, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", false, errors.New("BaseURL does not parse")
	}
	insecure := u.Scheme == "http"
	switch {
	case u.Scheme != "https" && !insecure:
		return "", false, fmt.Errorf("BaseURL has scheme %q, want https", u.Scheme)
	case insecure && !devHTTP:
		return "", false, errors.New("BaseURL is http://, which only CDN_DEV_ALLOW_HTTP=1 of the test stand allows")
	case u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || (u.Path != "" && u.Path != "/"):
		return "", false, errors.New("BaseURL must be a scheme and a host only")
	}
	return u.Scheme + "://" + u.Host, insecure, nil
}

// parseHealthAddr checks HealthAddr: empty, or a host and a port.
func parseHealthAddr(v string) (string, error) {
	if v = strings.TrimSpace(v); v == "" {
		return "", nil
	}
	host, port, err := net.SplitHostPort(v)
	if p, perr := strconv.Atoi(port); err != nil || perr != nil || host == "" || p <= 0 || p > 65535 {
		return "", fmt.Errorf("HealthAddr %q: host:port", v)
	}
	return v, nil
}

// validOriginAuth reports whether v is a value of X-Pluma-Origin-Auth:
// originAuthDigits hex digits.
func validOriginAuth(v string) bool {
	_, err := hex.DecodeString(v)
	return err == nil && len(v) == originAuthDigits
}

// parse checks the origin, which is required while a delivery pulls from
// it; its AuthValue only while one of them has no OriginAuth of its own.
func (c OriginConfig) parse(s *Settings) error {
	if !slices.ContainsFunc(s.Deliveries, func(d Delivery) bool { return d.Source == SourceHomeOrigin }) {
		return nil
	}
	shared := slices.ContainsFunc(s.Deliveries, func(d Delivery) bool { return d.Source == SourceHomeOrigin && d.OriginAuth == "" })
	o := &s.Origin
	if o.Listen = strings.TrimSpace(c.Listen); o.Listen == "" {
		o.Listen = DefaultOriginListen
	}
	_, port, err := net.SplitHostPort(o.Listen)
	if p, perr := strconv.Atoi(port); err != nil || perr != nil || p <= 0 || p > 65535 {
		return fmt.Errorf("cdn: CDN_ORIGIN_LISTEN %q: host:port", o.Listen)
	}
	// The value is compared as it is written, and never printed.
	if (shared || c.AuthValue != "") && !validOriginAuth(c.AuthValue) {
		return fmt.Errorf("cdn: CDN_ORIGIN_AUTH must be %d hex digits (openssl rand -hex 32)", originAuthDigits)
	}
	o.AuthValue = Secret(c.AuthValue)
	n, err := intOr(c.MaxConcurrent, DefaultOriginMax)
	if err != nil || n < 1 || n > maxOriginMax {
		return fmt.Errorf("cdn: Origin.MaxConcurrent %q: from 1 to %d", c.MaxConcurrent, maxOriginMax)
	}
	o.MaxConcurrent = int(n)
	return nil
}

// kindNames are the names of CDN_KINDS.
var kindNames = map[string]FileKind{"document": KindDocument, "photo": KindPhoto}

// parseKinds parses CDN_KINDS, names separated by commas.
func parseKinds(v string) ([]FileKind, error) {
	if v = strings.TrimSpace(v); v == "" {
		v = DefaultKinds
	}
	var kinds []FileKind
	for name := range strings.SplitSeq(v, ",") {
		name = strings.TrimSpace(name)
		if name == "encrypted" {
			// The owner's decision of 2026-11-16 (ADR 0071 §6): a secret
			// chat file has one recipient, and the CDN would see the pair
			// of addresses.
			return nil, errors.New("cdn: CDN_KINDS: the files of secret chats (encrypted) never go through the CDN")
		}
		k, ok := kindNames[name]
		if !ok {
			return nil, fmt.Errorf("cdn: CDN_KINDS: unknown kind %q, want document or photo", name)
		}
		if !slices.Contains(kinds, k) {
			kinds = append(kinds, k)
		}
	}
	return kinds, nil
}

// flagOf parses a switch of the environment: "" or "0" off, "1" on.
func flagOf(v string) (bool, error) {
	switch strings.TrimSpace(v) {
	case "", "0":
		return false, nil
	case "1":
		return true, nil
	}
	return false, errors.New("want 0 or 1")
}

// durationOr parses v, or returns def for an empty one.
func durationOr(v string, def time.Duration) (time.Duration, error) {
	if v = strings.TrimSpace(v); v == "" {
		return def, nil
	}
	return time.ParseDuration(v)
}

// intOr parses v, or returns def for an empty one.
func intOr(v string, def int64) (int64, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return def, nil
	}
	return strconv.ParseInt(v, 10, 64)
}
