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

package config

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/teamgram/proto/mtproto"
)

// homeDcId is the DC of the server itself, this_dc of config.json.
const homeDcId = 1

// DcOptions returns dc_options of help.getConfig (task 102, ADR 0065 p. 7):
// DC 1 at each of Dc1Addresses, in their order, and the CDN DC when the CDN
// is on. Nothing configured: none, and the clients keep the address built
// into them. A wrong value is an error, and the service must not start with
// it: once dc_options is not empty, tdesktop replaces its whole list of DCs
// with it (Config::apply, mtproto/mtproto_config.cpp:285-289;
// DcOptions::processFromList with overwrite, mtproto_dc_options.cpp:198-240),
// so a list without a working DC 1 would cut it off the server until it is
// reinstalled. Android and iOS replace the addresses of each DC listed
// (ConnectionsManager.cpp:3486-3514; ManagedConfigurationUpdates.swift:22-38).
//
// The answer is shared by every request and is never changed.
func (c Config) DcOptions() ([]*mtproto.DcOption, error) {
	home, err := parseDcAddresses(c.Dc1Addresses)
	if err != nil {
		return nil, err
	}
	cdnSettings, err := c.Cdn.Parse()
	if err != nil {
		return nil, err
	}
	if cdnSettings != nil && len(home) == 0 {
		return nil, errors.New("dc_options: the CDN DC needs PLUMA_DC1_ADDRESSES, the addresses of DC 1: " +
			"with a list that lacks DC 1 the desktop app loses the server")
	}

	options := make([]*mtproto.DcOption, 0, len(home)+1)
	for _, ap := range home {
		// static, as the address built into the desktop app
		// (applyOneGuarded(1, Flag::f_static ...), clients/patches/desktop.md
		// D-002): with a proxy on, tdesktop keeps only the static endpoints
		// of a DC that has them (mtproto_dc_options.cpp:696-697).
		options = append(options, dcOption(homeDcId, ap, func(o *mtproto.DcOption) {
			o.Static = true
		}))
	}
	if cdnSettings != nil {
		// Parse has checked the form; the address must also be one a
		// client can reach, as those of DC 1.
		ap, err := netip.ParseAddrPort(cdnSettings.DcAddress)
		if err != nil || !reachable(ap) {
			return nil, fmt.Errorf("dc_options: CDN_DC_ADDRESS: %q is not an address the clients can reach", cdnSettings.DcAddress)
		}
		secret := cdnSettings.DcSecret.Bytes()
		options = append(options, dcOption(cdnSettings.DcId, unmap(ap), func(o *mtproto.DcOption) {
			// cdn: tdesktop connects to a CDN DC only through endpoints
			// with the flag (mtproto_dc_options.cpp:677-678), Android
			// keeps them in the address groups of a CDN DC
			// (ConnectionsManager.cpp:3452-3454, 3510-3513).
			o.Cdn = true
			// tcpo_only: no HTTP transport for it in tdesktop
			// (mtproto_dc_options.cpp:689-691); the node speaks TCP only.
			o.TcpoOnly = true
			// The secret: with it Android tries only the port given
			// (Datacenter.cpp:337-345) and speaks the padded transport
			// (Connection.cpp:463-464); without it Android would try 443
			// and 5222 too, which lead home on Pluma's address.
			o.Secret = secret
		}))
	}
	return options, nil
}

// parseDcAddresses parses ip:port separated by commas. The clients do not
// resolve names in dc_options (docs/ops/clients.md), so a host name is
// refused, as are a port of 0, an address no client can reach and the same
// address twice.
func parseDcAddresses(list string) ([]netip.AddrPort, error) {
	list = strings.TrimSpace(list)
	if list == "" {
		return nil, nil
	}
	var (
		out  []netip.AddrPort
		seen = map[netip.AddrPort]bool{}
	)
	for _, item := range strings.Split(list, ",") {
		item = strings.TrimSpace(item)
		ap, err := netip.ParseAddrPort(item)
		if err != nil || !reachable(ap) {
			return nil, fmt.Errorf("dc_options: PLUMA_DC1_ADDRESSES: %q is not an IP address and a port "+
				"(IPv6 in brackets, the addresses separated by commas)", item)
		}
		ap = unmap(ap)
		if seen[ap] {
			return nil, fmt.Errorf("dc_options: PLUMA_DC1_ADDRESSES: %s is given twice", ap)
		}
		seen[ap] = true
		out = append(out, ap)
	}
	return out, nil
}

// reachable reports whether a client can connect to ap: a port, no IPv6
// zone, neither an unspecified nor a multicast address.
func reachable(ap netip.AddrPort) bool {
	a := ap.Addr()
	return ap.Port() != 0 && a.Zone() == "" && !a.IsUnspecified() && !a.IsMulticast()
}

// unmap turns an IPv4-mapped IPv6 address into the IPv4 address it is.
func unmap(ap netip.AddrPort) netip.AddrPort {
	return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
}

// dcOption is the dcOption of DC id at ap, ipv6 for an IPv6 address, with
// the flags set sets.
func dcOption(id int32, ap netip.AddrPort, set func(*mtproto.DcOption)) *mtproto.DcOption {
	o := &mtproto.DcOption{
		Ipv6:      ap.Addr().Is6(),
		Id:        id,
		IpAddress: ap.Addr().String(),
		Port:      int32(ap.Port()),
	}
	set(o)
	return mtproto.MakeTLDcOption(o).To_DcOption()
}
