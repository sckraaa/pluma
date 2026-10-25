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
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// FCMConfig is the FCM part of the push config.
type FCMConfig struct {
	// CredentialsFile is the JSON key of a service account with the role
	// Firebase Cloud Messaging API Admin; the provider is off when it is
	// empty or the file is empty.
	CredentialsFile string `json:",optional"`                           //nolint:staticcheck // a go-zero conf option, not encoding/json's
	Endpoint        string `json:",default=https://fcm.googleapis.com"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// TokenURL replaces the token_uri of the credentials (tests).
	TokenURL string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

const fcmScope = "https://www.googleapis.com/auth/firebase.messaging"

// googleTokenURL is Google's OAuth endpoint, for credentials without
// token_uri.
const googleTokenURL = "https://oauth2.googleapis.com/token" //nolint:gosec // an endpoint, not a credential

type serviceAccount struct {
	Type        string `json:"type"`
	ProjectId   string `json:"project_id"`
	PrivateKey  string `json:"private_key"`
	ClientEmail string `json:"client_email"`
	TokenURI    string `json:"token_uri"`
}

// FCM sends data messages through FCM HTTP v1.
type FCM struct {
	conf     FCMConfig
	account  serviceAccount
	key      *rsa.PrivateKey
	tokenURL string
	client   *http.Client
	breaker  *breaker
	now      func() time.Time

	mu          sync.Mutex
	accessToken string
	expires     time.Time
}

// NewFCM returns the FCM sender of conf, nil (off) without credentials.
func NewFCM(conf FCMConfig, client *http.Client) (*FCM, error) {
	if conf.CredentialsFile == "" {
		return nil, nil
	}
	data, err := os.ReadFile(conf.CredentialsFile)
	if err != nil {
		return nil, fmt.Errorf("fcm: credentials: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	var sa serviceAccount
	if err = json.Unmarshal(data, &sa); err != nil || sa.Type != "service_account" || sa.ProjectId == "" || sa.ClientEmail == "" {
		return nil, fmt.Errorf("fcm: %s is not the key of a service account", conf.CredentialsFile)
	}
	k, err := parsePKCS8([]byte(sa.PrivateKey))
	if err != nil {
		return nil, fmt.Errorf("fcm: credentials: %w", err)
	}
	key, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("fcm: the credentials key is not RSA")
	}
	tokenURL := sa.TokenURI
	if conf.TokenURL != "" {
		tokenURL = conf.TokenURL
	}
	if tokenURL == "" {
		tokenURL = googleTokenURL
	}
	if client == nil {
		client = &http.Client{Transport: &http.Transport{ForceAttemptHTTP2: true, MaxIdleConnsPerHost: 4, IdleConnTimeout: 5 * time.Minute}}
	}
	return &FCM{conf: conf, account: sa, key: key, tokenURL: tokenURL, client: client, breaker: newBreaker(), now: time.Now}, nil
}

// ProjectId is the Firebase project of the credentials.
func (f *FCM) ProjectId() string { return f.account.ProjectId }

var errFCMAuth = errors.New("fcm: the access token was refused")

// token returns an OAuth access token, fetched again five minutes before it
// expires or when FCM refused it.
func (f *FCM) token(ctx context.Context, renew bool) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.now()
	if f.accessToken != "" && !renew && now.Before(f.expires.Add(-5*time.Minute)) {
		return f.accessToken, nil
	}
	assertion, err := signRS256(f.key,
		map[string]string{"alg": "RS256", "typ": "JWT"},
		map[string]any{"iss": f.account.ClientEmail, "scope": fcmScope, "aud": f.tokenURL, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix()})
	if err != nil {
		return "", err
	}
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {assertion}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	resp, err := f.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16384))
	var t struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		Error       string `json:"error"`
	}
	if err = json.Unmarshal(body, &t); err != nil || resp.StatusCode != http.StatusOK || t.AccessToken == "" {
		if resp.StatusCode >= 500 {
			return "", fmt.Errorf("fcm: token endpoint %d", resp.StatusCode)
		}
		return "", fmt.Errorf("%w: %d %s", errFCMAuth, resp.StatusCode, t.Error)
	}
	f.accessToken, f.expires = t.AccessToken, now.Add(time.Duration(t.ExpiresIn)*time.Second)
	return f.accessToken, nil
}

type fcmRequest struct {
	ValidateOnly bool       `json:"validate_only,omitempty"`
	Message      fcmMessage `json:"message"`
}

type fcmMessage struct {
	Token   string            `json:"token"`
	Data    map[string]string `json:"data"`
	Android fcmAndroid        `json:"android"`
}

type fcmAndroid struct {
	Priority string `json:"priority"`
	TTL      string `json:"ttl"`
}

type fcmError struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
		Details []struct {
			Type      string `json:"@type"`
			ErrorCode string `json:"errorCode"`
		} `json:"details"`
	} `json:"error"`
}

func (e *fcmError) code() string {
	for _, d := range e.Error.Details {
		if d.ErrorCode != "" {
			return d.ErrorCode
		}
	}
	return e.Error.Status
}

// FCMTTL is how long FCM keeps a push for an offline device.
const FCMTTL = 24 * time.Hour

// Send sends a data message to token within the deadline of ctx;
// validateOnly checks it without delivering (pluma-admin push check).
func (f *FCM) Send(ctx context.Context, token string, data map[string]string, validateOnly bool) (Outcome, error) {
	if f == nil {
		return OutcomeDisabled, nil
	}
	body, err := json.Marshal(fcmRequest{
		ValidateOnly: validateOnly,
		Message: fcmMessage{
			Token:   token,
			Data:    data,
			Android: fcmAndroid{Priority: "HIGH", TTL: fmt.Sprintf("%ds", int(FCMTTL.Seconds()))},
		},
	})
	if err != nil {
		return OutcomeRejected, err
	}
	renewed := false
	return sendWithRetries(ctx, f.breaker, func(ctx context.Context) attempt {
		at := f.try(ctx, body, false)
		if at.outcome == OutcomeAuthFailed && errors.Is(at.err, errFCMAuth) && !renewed {
			renewed = true
			at = f.try(ctx, body, true)
		}
		return at
	})
}

func (f *FCM) try(ctx context.Context, body []byte, renew bool) attempt {
	token, err := f.token(ctx, renew)
	switch {
	case errors.Is(err, errFCMAuth):
		return attempt{outcome: OutcomeAuthFailed, err: err}
	case err != nil:
		return attempt{outcome: OutcomeUnavailable, retry: ctx.Err() == nil, err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		f.conf.Endpoint+"/v1/projects/"+url.PathEscape(f.account.ProjectId)+"/messages:send", bytes.NewReader(body))
	if err != nil {
		return attempt{outcome: OutcomeRejected, err: err}
	}
	req.Header.Set("authorization", "Bearer "+token)
	req.Header.Set("content-type", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		return attempt{outcome: OutcomeUnavailable, retry: ctx.Err() == nil, err: err}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 16384))
	if resp.StatusCode == http.StatusOK {
		return attempt{outcome: OutcomeSent}
	}
	var e fcmError
	_ = json.Unmarshal(data, &e)
	code := e.code()
	err = fmt.Errorf("fcm: %d %s", resp.StatusCode, code)
	switch {
	case code == "UNREGISTERED" || code == "SENDER_ID_MISMATCH" || resp.StatusCode == http.StatusNotFound,
		code == "INVALID_ARGUMENT" && strings.Contains(e.Error.Message, "registration token"):
		return attempt{outcome: OutcomeTokenInvalid, err: err}
	case resp.StatusCode == http.StatusUnauthorized:
		return attempt{outcome: OutcomeAuthFailed, err: fmt.Errorf("%w: %v", errFCMAuth, err)}
	case resp.StatusCode == http.StatusForbidden || code == "THIRD_PARTY_AUTH_ERROR":
		return attempt{outcome: OutcomeAuthFailed, err: err}
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return attempt{outcome: OutcomeUnavailable, retry: true, retryAfter: retryAfter(resp.Header), err: err}
	}
	return attempt{outcome: OutcomeRejected, err: err}
}
