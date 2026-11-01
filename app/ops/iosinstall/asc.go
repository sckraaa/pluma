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

package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// ascClient calls the App Store Connect API (OpenAPI 4.5) with a team key:
// devices and provisioning profiles. A request carries a fresh ES256 token
// (Generating Tokens for API Requests: 20 minutes at most).
type ascClient struct {
	base   string
	keyID  string
	issuer string
	key    *ecdsa.PrivateKey
	http   *http.Client
}

func newASCClient(keyPEM []byte, keyID, issuer string) (*ascClient, error) {
	blk, _ := pem.Decode(keyPEM)
	if blk == nil {
		return nil, errors.New("app store connect key: no PEM block")
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, fmt.Errorf("app store connect key: %w", err)
	}
	key, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("app store connect key: %T, want ECDSA", k)
	}
	return &ascClient{
		base:   "https://api.appstoreconnect.apple.com",
		keyID:  keyID,
		issuer: issuer,
		key:    key,
		http:   &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (c *ascClient) token(now time.Time) (string, error) {
	b64 := base64.RawURLEncoding.EncodeToString
	head, _ := json.Marshal(map[string]string{"alg": "ES256", "kid": c.keyID, "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{
		"iss": c.issuer, "iat": now.Unix(), "exp": now.Add(10 * time.Minute).Unix(), "aud": "appstoreconnect-v1",
	})
	in := b64(head) + "." + b64(claims)
	sum := sha256.Sum256([]byte(in))
	r, s, err := ecdsa.Sign(rand.Reader, c.key, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return in + "." + b64(sig), nil
}

// ascError is an error answer of the API; the body holds no secret.
type ascError struct {
	Status int
	Body   string
}

func (e *ascError) Error() string {
	return fmt.Sprintf("app store connect: HTTP %d: %s", e.Status, e.Body)
}

func (c *ascClient) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	tok, err := c.token(time.Now())
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		if len(raw) > 2000 {
			raw = raw[:2000]
		}
		return &ascError{Status: resp.StatusCode, Body: string(raw)}
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

type ascResource struct {
	Type          string                     `json:"type"`
	ID            string                     `json:"id"`
	Attributes    map[string]any             `json:"attributes,omitempty"`
	Relationships map[string]ascRelationship `json:"relationships,omitempty"`
}

type ascRelationship struct {
	Data json.RawMessage `json:"data"`
}

type ascRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

func (r ascResource) attr(name string) string {
	s, _ := r.Attributes[name].(string)
	return s
}

// list reads every page of a collection.
func (c *ascClient) list(ctx context.Context, path string) ([]ascResource, error) {
	var all []ascResource
	for path != "" {
		var page struct {
			Data  []ascResource `json:"data"`
			Links struct {
				Next string `json:"next"`
			} `json:"links"`
		}
		if err := c.do(ctx, http.MethodGet, path, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Data...)
		path = ""
		if page.Links.Next != "" {
			u, err := url.Parse(page.Links.Next)
			if err != nil {
				return nil, err
			}
			path = u.RequestURI()
		}
	}
	return all, nil
}

// deviceID returns the id of the registered device with udid, or "".
func (c *ascClient) deviceID(ctx context.Context, udid string) (string, error) {
	q := url.Values{"filter[udid]": {udid}, "filter[platform]": {"IOS"}, "limit": {"200"}}
	ds, err := c.list(ctx, "/v1/devices?"+q.Encode())
	if err != nil {
		return "", err
	}
	for _, d := range ds {
		if d.attr("udid") == udid {
			return d.ID, nil
		}
	}
	return "", nil
}

// registerDevice registers an iPhone or iPad and returns its id.
func (c *ascClient) registerDevice(ctx context.Context, name, udid string) (string, error) {
	var out struct {
		Data ascResource `json:"data"`
	}
	in := map[string]any{"data": map[string]any{
		"type":       "devices",
		"attributes": map[string]any{"name": name, "udid": udid, "platform": "IOS"},
	}}
	if err := c.do(ctx, http.MethodPost, "/v1/devices", in, &out); err != nil {
		return "", err
	}
	return out.Data.ID, nil
}

// enabledDevices returns the ids of the team's enabled iOS devices.
func (c *ascClient) enabledDevices(ctx context.Context) ([]string, error) {
	q := url.Values{"filter[platform]": {"IOS"}, "filter[status]": {"ENABLED"}, "limit": {"200"}}
	ds, err := c.list(ctx, "/v1/devices?"+q.Encode())
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(ds))
	for _, d := range ds {
		ids = append(ids, d.ID)
	}
	return ids, nil
}

// bundleID returns the id of the bundle ID resource of identifier.
func (c *ascClient) bundleID(ctx context.Context, identifier string) (string, error) {
	q := url.Values{"filter[identifier]": {identifier}, "limit": {"200"}}
	bs, err := c.list(ctx, "/v1/bundleIds?"+q.Encode())
	if err != nil {
		return "", err
	}
	// The filter matches prefixes too: chat.pluma.app finds its extensions.
	for _, b := range bs {
		if b.attr("identifier") == identifier {
			return b.ID, nil
		}
	}
	return "", fmt.Errorf("app store connect: no bundle ID %s", identifier)
}

type ascProfile struct {
	ID, Name string
}

// adHocProfiles returns the ad hoc profiles of a bundle ID resource.
func (c *ascClient) adHocProfiles(ctx context.Context, bundleID string) ([]ascProfile, error) {
	// This related endpoint takes no filter (PARAMETER_ERROR.ILLEGAL); the
	// App Store profiles of the bundle are left alone.
	ps, err := c.list(ctx, "/v1/bundleIds/"+url.PathEscape(bundleID)+"/profiles?limit=200")
	if err != nil {
		return nil, err
	}
	var out []ascProfile
	for _, p := range ps {
		if p.attr("profileType") == "IOS_APP_ADHOC" {
			out = append(out, ascProfile{ID: p.ID, Name: p.attr("name")})
		}
	}
	return out, nil
}

func (c *ascClient) deleteProfile(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/profiles/"+url.PathEscape(id), nil, nil)
}

// createAdHocProfile creates an ad hoc profile of the bundle ID for the
// certificate and devices and returns its content (the .mobileprovision).
func (c *ascClient) createAdHocProfile(ctx context.Context, name, bundleID, certificateID string, devices []string) ([]byte, error) {
	refs := func(typ string, ids []string) map[string]any {
		data := make([]ascRef, 0, len(ids))
		for _, id := range ids {
			data = append(data, ascRef{Type: typ, ID: id})
		}
		return map[string]any{"data": data}
	}
	in := map[string]any{"data": map[string]any{
		"type":       "profiles",
		"attributes": map[string]any{"name": name, "profileType": "IOS_APP_ADHOC"},
		"relationships": map[string]any{
			"bundleId":     map[string]any{"data": ascRef{Type: "bundleIds", ID: bundleID}},
			"certificates": refs("certificates", []string{certificateID}),
			"devices":      refs("devices", devices),
		},
	}}
	var out struct {
		Data ascResource `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/profiles", in, &out); err != nil {
		return nil, err
	}
	content, err := base64.StdEncoding.DecodeString(out.Data.attr("profileContent"))
	if err != nil || len(content) == 0 {
		return nil, errors.New("app store connect: a profile without content")
	}
	return content, nil
}
