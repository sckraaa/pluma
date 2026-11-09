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
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
)

// config is admin.yaml (docs/ops/admin-panel.md). Listen must be a loopback
// address: tailscale serve is the only way in (ADR 0040).
type config struct {
	Listen            string
	Origin            string
	Mysql             sqlx.Config
	BizServiceClient  zrpc.RpcClientConf
	MsgClient         zrpc.RpcClientConf
	AuthSessionClient zrpc.RpcClientConf
	DfsClient         zrpc.RpcClientConf
	SyncClient        struct{ Topic string }
	Log               logx.LogConf
}

func (c *config) check() error {
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	if ip, err := netip.ParseAddr(host); err != nil || !ip.IsLoopback() {
		return errors.New("listen must be a loopback address (ADR 0040)")
	}
	u, err := url.Parse(c.Origin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" {
		return errors.New("origin must be the panel's https:// address in the tailnet")
	}
	if strings.Contains(u.Host, ":") {
		return errors.New("origin must have no port")
	}
	return nil
}

// rpID is the WebAuthn relying party id: the host of Origin.
func (c *config) rpID() string {
	u, _ := url.Parse(c.Origin)
	return u.Hostname()
}
