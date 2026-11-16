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

// Package config is the configuration of a CDN node (task 102, ADR 0065).
package config

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	"github.com/teamgram/teamgram-server/pkg/mtserver/codec"
)

// Config of a CDN node.
type Config struct {
	Log logx.LogConf

	// DcId is the CDN DC the node serves (CDN_DC_ID), as dc_options names
	// it: below 1000, which tdesktop takes for a temporary DC
	// (mtp_instance.h:45), and not 1, the home.
	DcId int32

	MTProto struct {
		// ListenOn is the client port (CDN_LISTEN inside the container).
		ListenOn string
		// Secret is the obfuscation secret of the port, "dd" and 32 hex
		// digits, as dc_options carries it for the DC (CDN_SECRET). Only
		// connections made with it are accepted.
		Secret string
		// MaxAuthKeys bounds the client keys held in memory.
		MaxAuthKeys int `json:",default=20000"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
		// MaxConnsPerIP bounds the connections of one address (an IPv6
		// /64 counts as one).
		MaxConnsPerIP int `json:",default=32"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
		// KeyExchangesPerIPMinute bounds the key exchanges of one address,
		// with the gateway's number (gnetway ip_limits.go).
		KeyExchangesPerIPMinute int `json:",default=300"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
		// MaxInFlight bounds the requests one connection has in progress;
		// its next messages wait.
		MaxInFlight int `json:",default=16"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
		// IdleTimeout closes a connection silent for that long.
		IdleTimeout time.Duration `json:",default=5m"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	}

	Store struct {
		// DataDir holds the files, their index, the node's RSA key and the
		// traffic count (CDN_DATA_DIR).
		DataDir string
		// CacheBytes is the ceiling of the files held (CDN_CACHE_BYTES).
		CacheBytes int64
		// IdleTTL drops a file not served for that long (CDN_IDLE_TTL).
		IdleTTL time.Duration `json:",default=720h"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
		// DiskReserveBytes is the free space the volume keeps: below it
		// Put is refused and Status reports disk_low.
		DiskReserveBytes int64 `json:",default=2147483648"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	}

	Control struct {
		// ListenOn is the address of the gRPC control API (CDN_CONTROL_LISTEN).
		ListenOn string
		// CertFile and KeyFile are the node's certificate; ClientCAFile
		// signs the certificate of the home.
		CertFile     string
		KeyFile      string
		ClientCAFile string
	}

	// MetricsListenOn serves /metrics; empty turns it off.
	MetricsListenOn string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// Validate checks what the node cannot start without.
func (c *Config) Validate() error {
	if c.DcId < 2 || c.DcId >= 1000 {
		return fmt.Errorf("config: DcId %d: a CDN DC is 2-999 (1 is the home; tdesktop takes 1000 and above for temporary DCs)", c.DcId)
	}
	if _, _, err := net.SplitHostPort(c.MTProto.ListenOn); err != nil {
		return fmt.Errorf("config: MTProto.ListenOn: %w", err)
	}
	if _, err := c.ParseSecret(); err != nil {
		return err
	}
	if c.MTProto.MaxAuthKeys < 1 || c.MTProto.MaxConnsPerIP < 1 || c.MTProto.KeyExchangesPerIPMinute < 1 ||
		c.MTProto.MaxInFlight < 1 || c.MTProto.IdleTimeout <= 0 {
		return errors.New("config: the MTProto limits must be positive")
	}
	if c.Store.DataDir == "" {
		return errors.New("config: Store.DataDir is required")
	}
	if c.Store.CacheBytes <= 0 {
		return errors.New("config: Store.CacheBytes (CDN_CACHE_BYTES) must be positive: it is the disk the node may fill")
	}
	if c.Store.IdleTTL <= 0 || c.Store.DiskReserveBytes < 0 {
		return errors.New("config: Store.IdleTTL must be positive and Store.DiskReserveBytes not negative")
	}
	if c.Control.ListenOn == "" || c.Control.CertFile == "" || c.Control.KeyFile == "" || c.Control.ClientCAFile == "" {
		return errors.New("config: Control needs ListenOn, CertFile, KeyFile and ClientCAFile: the API is served over mutual TLS only")
	}
	if _, _, err := net.SplitHostPort(c.Control.ListenOn); err != nil {
		return fmt.Errorf("config: Control.ListenOn: %w", err)
	}
	return nil
}

// ParseSecret returns the obfuscation secret of the client port.
func (c *Config) ParseSecret() (codec.Secret, error) {
	s, err := codec.ParseSecretHex(c.MTProto.Secret)
	if err != nil {
		return s, fmt.Errorf("config: MTProto.Secret (CDN_SECRET): %w", err)
	}
	return s, nil
}
