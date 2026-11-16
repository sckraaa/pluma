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
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// The check of a delivery (task 107, "Здоровье и бюджет").
const (
	// healthTimeout bounds each request of a check.
	healthTimeout = 10 * time.Second
	// healthValidChecks: a delivery that passed a check takes redirects
	// for this many intervals of the checks.
	healthValidChecks = 3
	// healthRangeFrom and healthRangeTo are the range of the canary a check
	// fetches: 4 KB inside it, so that a CDN that answers a range with the
	// whole object, or another range, fails.
	healthRangeFrom = 131072
	healthRangeTo   = 135167
	// healthUserAgent is the User-Agent of the checks, the one of the
	// patched clients (task 107, client contract p. 10).
	healthUserAgent = "Pluma"
)

// Reasons a delivery is out (cdn_deliveries.unhealthy_reason).
const (
	UnhealthyStatus   = "status"    // not 206 for the range, or not 403/410 for a broken signature
	UnhealthyRange    = "range"     // another Content-Range
	UnhealthyBytes    = "bytes"     // other bytes than the canary's, or another length
	UnhealthyTimeout  = "timeout"   // no answer within healthTimeout
	UnhealthyTLS      = "tls"       // the certificate of the CDN was refused
	UnhealthyNetwork  = "network"   // no connection: DNS, refused, reset
	UnhealthyTokenOff = "token_off" // a broken signature was served: URLs no longer expire
	UnhealthyStore    = "store"     // the URL could not be signed
)

// Health checks the deliveries every HealthEvery and writes what it found
// into cdn_deliveries, which the redirects read (ADR 0071 §5):
//
//  1. the canary through the delivery, signed as the clients get it, with
//     Range: bytes=131072-135167: 206, exactly that Content-Range of the
//     262144 bytes, and the canary's bytes;
//  2. the same URL with its signature broken (the last character of md5
//     or of the path-hmac signature): 403 or 410. A 2xx means the CDN no
//     longer checks the signature (Secure Token off in the portal, or not
//     checked for a cached object) and the URLs stopped expiring:
//     token_off.
//
// A delivery that passes both takes redirects for healthValidChecks
// intervals (healthy_until); one that fails is out at once, with its
// reason. The requests take the usual way out of the container (through
// the VPS, ADR 0061 §3), never a redirect or a compressed answer.
type Health struct {
	store  Store
	set    *Settings
	client *http.Client
	now    func() time.Time

	mu   sync.Mutex
	last map[int32]string // the last reason of each delivery, "" healthy; for the log of changes
}

// NewHealth returns the checks of the deliveries of set.
func NewHealth(store Store, set *Settings) *Health {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DisableCompression = true
	tr.MaxIdleConnsPerHost = 2
	return &Health{
		store: store,
		set:   set,
		client: &http.Client{
			Transport: tr,
			Timeout:   healthTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		now:  time.Now,
		last: map[int32]string{},
	}
}

// Run checks every delivery now and every HealthEvery until ctx is done.
func (h *Health) Run(ctx context.Context) {
	t := time.NewTicker(h.set.HealthEvery)
	defer t.Stop()
	for {
		h.Round(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Round checks every delivery once, at the same time, and records what it
// found.
func (h *Health) Round(ctx context.Context) {
	var wg sync.WaitGroup
	for i := range h.set.Deliveries {
		d := &h.set.Deliveries[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.record(ctx, d, h.Check(ctx, d))
		}()
	}
	wg.Wait()
}

// Check checks delivery d once; it returns why d is out, empty if it
// passed.
func (h *Health) Check(ctx context.Context, d *Delivery) string {
	signed, _, err := d.Signer.SignURL(CanaryPath, h.now())
	if err != nil {
		logx.WithContext(ctx).Errorf("cdn: delivery %d: sign the canary: %v", d.ID, err)
		return UnhealthyStore
	}
	if reason := h.checkCanary(ctx, signed); reason != "" {
		return reason
	}
	broken, err := breakSignature(d.Signer.Name(), signed)
	if err != nil {
		logx.WithContext(ctx).Errorf("cdn: delivery %d: break the signature of the canary: %v", d.ID, err)
		return UnhealthyStore
	}
	return h.checkRefused(ctx, broken)
}

// checkCanary fetches the range of the canary at signed.
func (h *Health) checkCanary(ctx context.Context, signed string) string {
	res, err := h.get(ctx, signed)
	if err != nil {
		return failureReason(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusPartialContent {
		return UnhealthyStatus
	}
	if res.Header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", healthRangeFrom, healthRangeTo, canarySize) {
		return UnhealthyRange
	}
	want := canaryBytes[healthRangeFrom : healthRangeTo+1]
	body, err := io.ReadAll(io.LimitReader(res.Body, int64(len(want))+1))
	if err != nil {
		return failureReason(err)
	}
	if !bytes.Equal(body, want) {
		return UnhealthyBytes
	}
	return ""
}

// checkRefused fetches the canary at broken, whose signature is broken:
// the CDN must refuse it.
func (h *Health) checkRefused(ctx context.Context, broken string) string {
	res, err := h.get(ctx, broken)
	if err != nil {
		return failureReason(err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, canarySize)) // lets the connection be kept
	switch {
	case res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusGone:
		return ""
	case res.StatusCode >= 200 && res.StatusCode < 300:
		return UnhealthyTokenOff
	}
	return UnhealthyStatus
}

// get asks for the range of the canary at u, as a client asks for a part.
func (h *Health) get(ctx context.Context, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, errors.New("the URL does not parse")
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", healthRangeFrom, healthRangeTo))
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("User-Agent", healthUserAgent)
	res, err := h.client.Do(req)
	if err != nil {
		// The error of net/http names the URL, signature and all.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, err
	}
	return res, nil
}

// record writes the outcome of a check of d.
func (h *Health) record(ctx context.Context, d *Delivery, reason string) {
	if ctx.Err() != nil {
		return // stopping: the check was cut short, not failed
	}
	ms := h.now().UnixMilli()
	row := &DeliveryHealth{DcId: d.ID, UnhealthyReason: reason, UpdatedAt: ms}
	if reason == "" {
		row.HealthyUntil = ms + healthValidChecks*h.set.HealthEvery.Milliseconds()
		metricDeliveryHealthy.Set(1, dcLabel(d.ID))
	} else {
		metricDeliveryHealthy.Set(0, dcLabel(d.ID))
	}
	h.logChange(ctx, d, reason)
	if err := h.store.PutDeliveryHealth(ctx, row); err != nil {
		// The row in force lapses by itself at its healthy_until.
		logx.WithContext(ctx).Errorf("cdn: delivery %d: record its check: %v", d.ID, err)
	}
}

// logChange logs a delivery that went out or came back; only its host is
// named, never a URL.
func (h *Health) logChange(ctx context.Context, d *Delivery, reason string) {
	h.mu.Lock()
	prev, seen := h.last[d.ID]
	h.last[d.ID] = reason
	h.mu.Unlock()
	if seen && prev == reason {
		return
	}
	host := strings.TrimPrefix(strings.TrimPrefix(d.BaseURL, "https://"), "http://")
	if reason == "" {
		logx.WithContext(ctx).Infof("cdn: delivery %d (%s) is healthy", d.ID, host)
		return
	}
	logx.WithContext(ctx).Errorf("cdn: delivery %d (%s) is out: %s", d.ID, host, reason)
}

// failureReason names a request that got no answer.
func failureReason(err error) string {
	var ne net.Error
	var certErr *tls.CertificateVerificationError
	var unknownCA x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	var invalid x509.CertificateInvalidError
	var alert tls.AlertError
	var record tls.RecordHeaderError
	switch {
	case errors.As(err, &certErr), errors.As(err, &unknownCA), errors.As(err, &hostErr),
		errors.As(err, &invalid), errors.As(err, &alert), errors.As(err, &record):
		return UnhealthyTLS
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return UnhealthyTimeout
	}
	return UnhealthyNetwork
}

// breakSignature returns signed (a URL of the signer named signer) with
// the last character of its signature changed. The character is flipped
// in its two highest bits, of the six base64 carries: the last character
// of 128 bits (an md5) has only those two significant, and a CDN that
// decodes the signature before it compares must still see another one.
func breakSignature(signer, signed string) (string, error) {
	u, err := url.Parse(signed)
	if err != nil {
		return "", errors.New("the URL does not parse")
	}
	switch signer {
	case SignerGcoreMD5:
		q := u.Query()
		md5 := q.Get("md5")
		if md5 == "" {
			return "", errors.New("no md5 in the URL")
		}
		broken, err := flipLast(md5)
		if err != nil {
			return "", err
		}
		// The order of the parameters is kept: md5, then expires.
		u.RawQuery = strings.Replace(u.RawQuery, "md5="+md5, "md5="+broken, 1)
		return u.String(), nil
	case SignerPathHMAC:
		parts := strings.Split(u.Path, "/") // "", "t", bucket, sig, ...
		if len(parts) < 5 || parts[1] != "t" {
			return "", errors.New("no signature in the path")
		}
		if parts[3], err = flipLast(parts[3]); err != nil {
			return "", err
		}
		u.Path = strings.Join(parts, "/")
		return u.String(), nil
	}
	return "", fmt.Errorf("unknown signer %q", signer)
}

// base64URL is the alphabet of base64url.
const base64URL = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

// flipLast changes the last character of s, a base64url string, in the
// two highest of its six bits.
func flipLast(s string) (string, error) {
	if s == "" {
		return "", errors.New("an empty signature")
	}
	i := strings.IndexByte(base64URL, s[len(s)-1])
	if i < 0 {
		return "", errors.New("the signature is not base64url")
	}
	return s[:len(s)-1] + string(base64URL[i^0x30]), nil
}
