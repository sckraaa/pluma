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

// Package garage prepares the Garage object store dfs uses (ADR 0014)
// through its admin API v2: the layout of the single node, the S3 key of
// dfs and the buckets. Every step is idempotent: running it on a prepared
// store changes nothing.
package garage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Admin is a client of the Garage admin API.
type Admin struct {
	base   string
	token  string
	client *http.Client
}

// New returns a client of the admin API at base (http://garage:3903).
func New(base, token string) *Admin {
	return &Admin{base: strings.TrimRight(base, "/"), token: token, client: &http.Client{Timeout: 15 * time.Second}}
}

var errNotFound = errors.New("garage: not found")

// APIError is an answer of the admin API other than 2xx.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("garage admin API: %d %s", e.Status, strings.TrimSpace(e.Body))
}

func (a *Admin) do(ctx context.Context, method, path string, query url.Values, in, out any) error {
	u := a.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		return errNotFound
	}
	if resp.StatusCode/100 != 2 {
		return &APIError{Status: resp.StatusCode, Body: string(b)}
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

type nodeRole struct {
	Zone     string   `json:"zone"`
	Capacity *int64   `json:"capacity"`
	Tags     []string `json:"tags"`
}

type clusterStatus struct {
	LayoutVersion int64 `json:"layoutVersion"`
	Nodes         []struct {
		ID   string    `json:"id"`
		IsUp bool      `json:"isUp"`
		Role *nodeRole `json:"role"`
	} `json:"nodes"`
}

// Wait retries the cluster status until the admin API answers.
func (a *Admin) Wait(ctx context.Context, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for {
		var st clusterStatus
		err := a.do(ctx, http.MethodGet, "/v2/GetClusterStatus", nil, nil, &st)
		if err == nil {
			return nil
		}
		var apiErr *APIError
		if errors.As(err, &apiErr) && (apiErr.Status == http.StatusUnauthorized || apiErr.Status == http.StatusForbidden) {
			return err // a wrong token does not get better
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("garage admin API at %s: %w", a.base, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// EnsureLayout gives the single node of the cluster a storage role.
func (a *Admin) EnsureLayout(ctx context.Context, zone string, capacity int64) (changed bool, err error) {
	var st clusterStatus
	if err := a.do(ctx, http.MethodGet, "/v2/GetClusterStatus", nil, nil, &st); err != nil {
		return false, err
	}
	if len(st.Nodes) != 1 {
		return false, fmt.Errorf("garage: %d nodes, a single-node store is expected", len(st.Nodes))
	}
	if st.Nodes[0].Role != nil {
		return false, nil
	}
	var layout struct {
		Version int64 `json:"version"`
	}
	if err := a.do(ctx, http.MethodGet, "/v2/GetClusterLayout", nil, nil, &layout); err != nil {
		return false, err
	}
	update := map[string]any{"roles": []map[string]any{{
		"id": st.Nodes[0].ID, "zone": zone, "capacity": capacity, "tags": []string{},
	}}}
	if err := a.do(ctx, http.MethodPost, "/v2/UpdateClusterLayout", nil, update, nil); err != nil {
		return false, err
	}
	if err := a.do(ctx, http.MethodPost, "/v2/ApplyClusterLayout", nil, map[string]any{"version": layout.Version + 1}, nil); err != nil {
		return false, err
	}
	return true, nil
}

// EnsureKey imports the S3 key of dfs; an existing key must have the same
// secret.
func (a *Admin) EnsureKey(ctx context.Context, id, secret, name string) (changed bool, err error) {
	var info struct {
		SecretAccessKey string `json:"secretAccessKey"`
	}
	err = a.do(ctx, http.MethodGet, "/v2/GetKeyInfo", url.Values{"id": {id}, "showSecretKey": {"true"}}, nil, &info)
	switch {
	case err == nil:
		if info.SecretAccessKey != secret {
			return false, fmt.Errorf("garage: key %s exists with another secret", id)
		}
		return false, nil
	case !errors.Is(err, errNotFound):
		return false, err
	}
	in := map[string]any{"accessKeyId": id, "secretAccessKey": secret, "name": name}
	return true, a.do(ctx, http.MethodPost, "/v2/ImportKey", nil, in, nil)
}

// EnsureBucket creates the bucket alias if needed and gives the key read,
// write and owner rights on it.
func (a *Admin) EnsureBucket(ctx context.Context, alias, keyID string) (created bool, err error) {
	var bucket struct {
		ID string `json:"id"`
	}
	err = a.do(ctx, http.MethodGet, "/v2/GetBucketInfo", url.Values{"globalAlias": {alias}}, nil, &bucket)
	if errors.Is(err, errNotFound) {
		created = true
		err = a.do(ctx, http.MethodPost, "/v2/CreateBucket", nil, map[string]any{"globalAlias": alias}, &bucket)
	}
	if err != nil {
		return false, err
	}
	allow := map[string]any{
		"bucketId": bucket.ID, "accessKeyId": keyID,
		"permissions": map[string]bool{"read": true, "write": true, "owner": true},
	}
	return created, a.do(ctx, http.MethodPost, "/v2/AllowBucketKey", nil, allow, nil)
}
