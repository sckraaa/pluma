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
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/teamgram/teamgram-server/pkg/cdn"
	"github.com/teamgram/teamgram-server/pkg/conf"
)

// cdnConfig is the part of bff.yaml that cdn canary-url reads.
type cdnConfig struct {
	Cdn cdn.Config `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// canaryURLTTL is how long the URL of cdn canary-url works: long enough
// for the owner's checks by hand (docs/ops/cdn.md), short enough that a
// URL left in a shell history is soon dead. A config whose CDN_URL_TTL is
// shorter keeps its own (a path-hmac URL more than 2·CDN_URL_TTL ahead is
// refused by the origin).
const canaryURLTTL = time.Hour

// The range of the canary the check fetches, as the health check of bff
// does (pkg/cdn/health.go): 4 KB inside the 256 KB object, so that a CDN
// that answers with the whole object or another range is seen.
const (
	canaryRangeFrom = 131072
	canaryRangeTo   = 135167
)

// cdnCommand: cdn canary-url [-expired] <dc> prints a signed URL of the
// canary of the origin (/c/v1) through delivery dc, valid for
// canaryURLTTL, and the curl line that checks it (task 107, the owner's
// checks in docs/ops/cdn.md); with -expired, a URL whose time has already
// passed, which the CDN (gcore-md5) or the origin (path-hmac) must refuse.
// It reads the deliveries from bff.yaml as bff does, with the same checks,
// so run it where bff's environment is: in the bff container.
func cdnCommand(args []string, configFile string, stdout io.Writer, now time.Time) error {
	if len(args) < 2 || args[1] != "canary-url" {
		return errUsage
	}
	flags := flag.NewFlagSet("cdn canary-url", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	expired := flags.Bool("expired", false, "a URL whose time has passed")
	if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 1 {
		return errUsage
	}
	dc, err := strconv.ParseInt(flags.Arg(0), 10, 32)
	if err != nil {
		return errUsage
	}
	var c cdnConfig
	if err := conf.Load(configFile, &c); err != nil {
		return fmt.Errorf("config %s: %w", configFile, err)
	}
	set, err := c.Cdn.Parse()
	if err != nil {
		return err
	}
	if set == nil {
		return errors.New("the CDN is off: no delivery has a BaseURL (CDN_GCORE_BASE_URL is empty)")
	}
	if set.URLTTL > canaryURLTTL {
		// The same config with the TTL of the check: Parse makes the
		// signers, and its checks hold for any TTL it accepts.
		c.Cdn.UrlTTL = canaryURLTTL.String()
		if set, err = c.Cdn.Parse(); err != nil {
			return err
		}
	}
	d, ok := set.Delivery(int32(dc))
	if !ok {
		ids := make([]string, 0, len(set.Deliveries))
		for _, d := range set.Deliveries {
			ids = append(ids, strconv.Itoa(int(d.ID)))
		}
		return fmt.Errorf("delivery %d is unknown or off; the deliveries on: %s", dc, strings.Join(ids, ", "))
	}
	at, expect := now, fmt.Sprintf("206 %d", canaryRangeTo-canaryRangeFrom+1)
	if *expired {
		// Signed two of its TTLs ago, the URL stopped working at least one
		// TTL ago, for either signer (a path-hmac window ends at most one
		// TTL after it began).
		at, expect = now.Add(-2*set.URLTTL), "403 or 410"
	}
	signed, exp, err := d.Signer.SignURL(cdn.CanaryPath, at)
	if err != nil {
		return fmt.Errorf("delivery %d: sign the canary: %w", d.ID, err)
	}
	until := time.Unix(exp, 0).UTC().Format("2006-01-02 15:04:05 UTC")
	if *expired {
		fmt.Fprintf(stdout, "Delivery %d (%s, %s), the canary of the origin, expired at %s:\n%s\n\n", d.ID, d.Name, d.Signer.Name(), until, signed)
	} else {
		fmt.Fprintf(stdout, "Delivery %d (%s, %s), the canary of the origin until %s:\n%s\n\n", d.ID, d.Name, d.Signer.Name(), until, signed)
	}
	fmt.Fprintf(stdout, "Expect \"%s\":\ncurl -sS -r %d-%d -o /dev/null -w '%%{http_code} %%{size_download}\\n' '%s'\n",
		expect, canaryRangeFrom, canaryRangeTo, signed)
	if d.InsecureHTTP {
		fmt.Fprintln(stdout, "\nThe delivery is http:// (CDN_DEV_ALLOW_HTTP=1): a test stand only, released clients refuse it.")
	}
	return nil
}
