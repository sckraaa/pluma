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
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/teamgram/teamgram-server/pkg/mtserver/codec"
)

// Config is the CDN of the home in bff.yaml (task 102, ADR 0065,
// docs/ops/cdn.md). Every value is a string, so that an empty variable of
// the environment takes the default; Parse checks them. Without
// NodeControl the CDN is off: upload.getFile never redirects, nothing is
// pushed and help.getCdnConfig is empty.
type Config struct {
	// NodeControl is host:port of the control API of the node (CDN_NODE_CONTROL,
	// the tunnel address 10.77.0.1:20510).
	NodeControl string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// ServerName is the name of the node's certificate; the host of
	// NodeControl when empty.
	ServerName string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// CertFile and KeyFile are bff's certificate of the control API, CAFile
	// signs the node's (teamgramd/deploy/cdn/control-certs.sh). Required with
	// NodeControl: the API is mutual TLS only.
	CertFile string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	KeyFile  string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	CAFile   string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// DcId is the CDN DC of the node (CDN_DC_ID, "201"): what
	// upload.fileCdnRedirect and help.getCdnConfig name.
	DcId string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// DcAddress is ip:port the clients reach the node at (CDN_DC_ADDRESS),
	// and DcSecret its secret, "dd" and 32 hex digits (CDN_DC_SECRET): the
	// home checks the node there as a client would.
	DcAddress string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	DcSecret  string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// MinFileSize is CDN_MIN_FILE_SIZE, a multiple of 1 MiB ("1048576").
	MinFileSize string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// PushRateMbit bounds the pushes to the node, megabits a second
	// (CDN_PUSH_RATE_MBIT, "20").
	PushRateMbit string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// PushConcurrency is how many files are pushed at once
	// (CDN_PUSH_CONCURRENCY, "1"); the rate is shared by them.
	PushConcurrency string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// MonthlyEgressLimitGB: the node is taken out of the redirects when it
	// sent this many GB (10^9 bytes) to clients in a calendar month (UTC)
	// (CDN_MONTHLY_EGRESS_LIMIT_GB). Required with NodeControl: below the
	// traffic the VPS plan includes.
	MonthlyEgressLimitGB string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// ReuploadWait is how long upload.reuploadCdnFile waits for the copy
	// (CDN_REUPLOAD_WAIT, "10s").
	ReuploadWait string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// Defaults of Config.
const (
	DefaultDcId            = 201
	DefaultPushRateMbit    = 20
	DefaultPushConcurrency = 1
	DefaultReuploadWait    = 10 * time.Second

	maxPushConcurrency = 4
	maxReuploadWait    = time.Minute
)

// Settings are a parsed Config.
type Settings struct {
	NodeControl string
	ServerName  string
	CertFile    string
	KeyFile     string
	CAFile      string

	DcId      int32
	DcAddress string
	DcSecret  codec.Secret

	MinFileSize        int64
	PushBytesPerSecond int64
	PushConcurrency    int
	MonthlyEgressLimit int64 // bytes
	ReuploadWait       time.Duration
}

// Enabled reports whether the CDN is configured.
func (c Config) Enabled() bool { return strings.TrimSpace(c.NodeControl) != "" }

// Parse checks an enabled Config and returns its settings; nil for a
// disabled one. A wrong value stops the start rather than leave the CDN
// half on.
func (c Config) Parse() (*Settings, error) {
	if !c.Enabled() {
		return nil, nil
	}
	s := &Settings{
		NodeControl: strings.TrimSpace(c.NodeControl),
		ServerName:  strings.TrimSpace(c.ServerName),
		CertFile:    c.CertFile,
		KeyFile:     c.KeyFile,
		CAFile:      c.CAFile,
	}
	host, _, err := net.SplitHostPort(s.NodeControl)
	if err != nil {
		return nil, fmt.Errorf("cdn: CDN_NODE_CONTROL %q: %w", s.NodeControl, err)
	}
	if s.ServerName == "" {
		s.ServerName = host
	}
	if s.CertFile == "" || s.KeyFile == "" || s.CAFile == "" {
		return nil, errors.New("cdn: Cdn needs CertFile, KeyFile and CAFile: the control API of the node is mutual TLS only")
	}

	dc, err := intOr(c.DcId, DefaultDcId)
	if err != nil || dc < 2 || dc >= 1000 {
		// tdesktop takes a DC of 1000 and above for a temporary one
		// (mtproto/mtp_instance.h:45); 1 is the home.
		return nil, fmt.Errorf("cdn: CDN_DC_ID %q: a number from 2 to 999", c.DcId)
	}
	s.DcId = int32(dc)

	ap, err := netip.ParseAddrPort(strings.TrimSpace(c.DcAddress))
	if err != nil || ap.Port() == 0 {
		// The clients do not resolve the names of dc_options
		// (docs/ops/clients.md).
		return nil, fmt.Errorf("cdn: CDN_DC_ADDRESS %q: an IP address and a port", c.DcAddress)
	}
	s.DcAddress = ap.String()
	if s.DcSecret, err = codec.ParseSecretHex(strings.TrimSpace(c.DcSecret)); err != nil {
		return nil, fmt.Errorf("cdn: CDN_DC_SECRET: %w", err)
	}

	if s.MinFileSize, err = intOr(c.MinFileSize, DefaultMinFileSize); err != nil {
		return nil, fmt.Errorf("cdn: CDN_MIN_FILE_SIZE %q: %w", c.MinFileSize, err)
	}
	if err = ValidMinFileSize(s.MinFileSize); err != nil {
		return nil, err
	}
	mbit, err := intOr(c.PushRateMbit, DefaultPushRateMbit)
	if err != nil || mbit <= 0 || mbit > 10000 {
		return nil, fmt.Errorf("cdn: CDN_PUSH_RATE_MBIT %q: a number of megabits a second from 1 to 10000", c.PushRateMbit)
	}
	s.PushBytesPerSecond = mbit * 1000 * 1000 / 8
	n, err := intOr(c.PushConcurrency, DefaultPushConcurrency)
	if err != nil || n < 1 || n > maxPushConcurrency {
		return nil, fmt.Errorf("cdn: CDN_PUSH_CONCURRENCY %q: from 1 to %d", c.PushConcurrency, maxPushConcurrency)
	}
	s.PushConcurrency = int(n)
	gb, err := strconv.ParseInt(strings.TrimSpace(c.MonthlyEgressLimitGB), 10, 64)
	if err != nil || gb <= 0 || gb > 1<<20 {
		return nil, fmt.Errorf("cdn: CDN_MONTHLY_EGRESS_LIMIT_GB %q: required, a positive number of GB below the traffic of the VPS plan", c.MonthlyEgressLimitGB)
	}
	s.MonthlyEgressLimit = gb * 1000 * 1000 * 1000
	s.ReuploadWait = DefaultReuploadWait
	if v := strings.TrimSpace(c.ReuploadWait); v != "" {
		if s.ReuploadWait, err = time.ParseDuration(v); err != nil || s.ReuploadWait < time.Second || s.ReuploadWait > maxReuploadWait {
			return nil, fmt.Errorf("cdn: CDN_REUPLOAD_WAIT %q: a duration from 1s to %v", c.ReuploadWait, maxReuploadWait)
		}
	}
	return s, nil
}

// intOr parses v, or returns def for an empty one.
func intOr(v string, def int64) (int64, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return def, nil
	}
	return strconv.ParseInt(v, 10, 64)
}
