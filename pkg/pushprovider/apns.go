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

package provider

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"
)

// APNsConfig is the APNs part of the push config.
type APNsConfig struct {
	// KeyFile is the .p8 of the APNs key (token-based authentication); the
	// provider is off when it is empty or the file is empty.
	KeyFile string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	KeyId   string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	TeamId  string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Topic is the bundle ID of the app; VoIP pushes go to Topic.voip.
	Topic         string `json:",optional"`                                   //nolint:staticcheck // a go-zero conf option, not encoding/json's
	ProductionURL string `json:",default=https://api.push.apple.com"`         //nolint:staticcheck // a go-zero conf option, not encoding/json's
	SandboxURL    string `json:",default=https://api.sandbox.push.apple.com"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// APNs push types and priorities.
const (
	APNsAlert      = "alert"
	APNsBackground = "background"
	APNsVoIP       = "voip"
)

// APNsPush is one request to APNs.
type APNsPush struct {
	Token   string // hex device token
	Sandbox bool   // the development gateway
	Type    string // APNsAlert, APNsBackground or APNsVoIP
	Body    []byte // the JSON payload
	// Expiration: until when APNs keeps the push for an offline device.
	Expiration time.Time
}

// tokenRefresh: Apple takes a provider token for an hour and refuses one
// renewed more often than every 20 minutes.
const tokenRefresh = 50 * time.Minute

// APNs sends to Apple over HTTP/2.
type APNs struct {
	conf    APNsConfig
	key     *ecdsa.PrivateKey
	client  *http.Client
	breaker *breaker
	now     func() time.Time

	mu      sync.Mutex
	token   string
	tokenAt time.Time
}

// NewAPNs returns the APNs sender of conf, nil (off) without a key. client
// is the HTTP client (HTTP/2 over TLS); nil is a default one.
func NewAPNs(conf APNsConfig, client *http.Client) (*APNs, error) {
	if conf.KeyFile == "" {
		return nil, nil
	}
	data, err := os.ReadFile(conf.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("apns: key: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	if conf.KeyId == "" || conf.TeamId == "" || conf.Topic == "" {
		return nil, errors.New("apns: KeyId, TeamId and Topic are required with a key")
	}
	k, err := parsePKCS8(data)
	if err != nil {
		return nil, fmt.Errorf("apns: key %s: %w", conf.KeyFile, err)
	}
	key, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("apns: key %s is not an EC key", conf.KeyFile)
	}
	if client == nil {
		client = &http.Client{Transport: &http.Transport{ForceAttemptHTTP2: true, MaxIdleConnsPerHost: 4, IdleConnTimeout: 5 * time.Minute}}
	}
	return &APNs{conf: conf, key: key, client: client, breaker: newBreaker(), now: time.Now}, nil
}

// providerToken returns the JWT of the requests, renewed every 50 minutes
// or when Apple says it expired.
func (a *APNs) providerToken(renew bool) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	if a.token != "" && !renew && now.Sub(a.tokenAt) < tokenRefresh {
		return a.token, nil
	}
	t, err := signES256(a.key,
		map[string]string{"alg": "ES256", "kid": a.conf.KeyId},
		map[string]any{"iss": a.conf.TeamId, "iat": now.Unix()})
	if err != nil {
		return "", err
	}
	a.token, a.tokenAt = t, now
	return t, nil
}

// APNsError is an answer of APNs other than 200.
type APNsError struct {
	Status int
	Reason string `json:"reason"`
}

func (e *APNsError) Error() string { return fmt.Sprintf("apns: %d %s", e.Status, e.Reason) }

// Send sends p within the deadline of ctx.
func (a *APNs) Send(ctx context.Context, p *APNsPush) (Outcome, error) {
	if a == nil {
		return OutcomeDisabled, nil
	}
	renewed := false
	return sendWithRetries(ctx, a.breaker, func(ctx context.Context) attempt {
		at := a.try(ctx, p, false)
		var e *APNsError
		if errors.As(at.err, &e) && e.Reason == "ExpiredProviderToken" && !renewed {
			renewed = true
			at = a.try(ctx, p, true)
		}
		return at
	})
}

func (a *APNs) try(ctx context.Context, p *APNsPush, renew bool) attempt {
	jwt, err := a.providerToken(renew)
	if err != nil {
		return attempt{outcome: OutcomeAuthFailed, err: err}
	}
	base := a.conf.ProductionURL
	if p.Sandbox {
		base = a.conf.SandboxURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/3/device/"+url.PathEscape(p.Token), bytes.NewReader(p.Body))
	if err != nil {
		return attempt{outcome: OutcomeRejected, err: err}
	}
	topic, priority := a.conf.Topic, "10"
	switch p.Type {
	case APNsBackground:
		priority = "5"
	case APNsVoIP:
		topic += ".voip"
	}
	req.Header.Set("authorization", "bearer "+jwt)
	req.Header.Set("apns-topic", topic)
	req.Header.Set("apns-push-type", p.Type)
	req.Header.Set("apns-priority", priority)
	req.Header.Set("content-type", "application/json")
	if !p.Expiration.IsZero() {
		req.Header.Set("apns-expiration", strconv.FormatInt(p.Expiration.Unix(), 10))
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return attempt{outcome: OutcomeUnavailable, retry: ctx.Err() == nil, err: err}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusOK {
		return attempt{outcome: OutcomeSent}
	}
	e := &APNsError{Status: resp.StatusCode}
	_ = json.Unmarshal(body, e)
	err = e
	switch {
	case resp.StatusCode == http.StatusGone,
		e.Reason == "BadDeviceToken", e.Reason == "DeviceTokenNotForTopic", e.Reason == "Unregistered", e.Reason == "ExpiredToken":
		return attempt{outcome: OutcomeTokenInvalid, err: err}
	case resp.StatusCode == http.StatusForbidden:
		return attempt{outcome: OutcomeAuthFailed, err: err}
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return attempt{outcome: OutcomeUnavailable, retry: true, retryAfter: retryAfter(resp.Header), err: err}
	}
	return attempt{outcome: OutcomeRejected, err: err}
}
