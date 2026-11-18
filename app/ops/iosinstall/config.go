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
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/teamgram/teamgram-server/pkg/iosupdate"
)

type config struct {
	publicURL, pageURL        string
	ascKeyID, ascIssuer       string
	ascKeyFile                string
	certificateID             string
	p12File, p12PasswordFile  string
	uploadToken               string
	dataDir, rcodesign        string
	listen                    string
	perAddressDay, devicesDay int
	// updateSecret signs the update tickets of help.getAppUpdate
	// (pkg/iosupdate); the bff holds the same. Empty: no ticket is taken.
	updateSecret string
	// releasesURL is releases.json of the releases service, which names
	// the current release of the iPhone app.
	releasesURL string
	// mirrors are the host names of the mirrors of the site (task 113,
	// MIRROR_HOSTS): a request that came to one of them is answered with
	// the addresses of that mirror, not of publicURL, so that the whole
	// install, and the update from the app, stays on the mirror.
	mirrors []string
}

func configFromEnv(getenv func(string) string) (config, error) {
	c := config{
		publicURL:       strings.TrimRight(getenv("PUBLIC_URL"), "/"),
		pageURL:         getenv("PAGE_URL"),
		ascKeyID:        getenv("ASC_KEY_ID"),
		ascIssuer:       getenv("ASC_ISSUER"),
		ascKeyFile:      getenv("ASC_KEY_FILE"),
		certificateID:   getenv("SIGN_CERTIFICATE_ID"),
		p12File:         getenv("SIGN_P12_FILE"),
		p12PasswordFile: getenv("SIGN_P12_PASSWORD_FILE"),
		dataDir:         or(getenv("DATA_DIR"), "/data"),
		rcodesign:       or(getenv("RCODESIGN"), "/usr/local/bin/rcodesign"),
		listen:          or(getenv("LISTEN"), ":9098"),
		updateSecret:    getenv("UPDATE_TICKET_SECRET"),
		releasesURL:     or(getenv("RELEASES_JSON_URL"), "http://releases:9097/dl/releases.json"),
	}
	u, err := url.Parse(c.publicURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" {
		return c, errors.New("PUBLIC_URL must be the site's https:// address, as https://pluma.chat")
	}
	c.pageURL = or(c.pageURL, c.publicURL+"/iphone/install")
	for h := range strings.SplitSeq(getenv("MIRROR_HOSTS"), ",") {
		if h = strings.TrimSpace(h); h == "" {
			continue
		}
		if !validHost(h) || h == u.Host {
			return c, fmt.Errorf("MIRROR_HOSTS: %q is not a lower-case host name other than that of PUBLIC_URL", h)
		}
		c.mirrors = append(c.mirrors, h)
	}
	for name, v := range map[string]string{
		"ASC_KEY_ID": c.ascKeyID, "ASC_ISSUER": c.ascIssuer, "ASC_KEY_FILE": c.ascKeyFile,
		"SIGN_CERTIFICATE_ID": c.certificateID, "SIGN_P12_FILE": c.p12File, "SIGN_P12_PASSWORD_FILE": c.p12PasswordFile,
		"UPLOAD_TOKEN_FILE": getenv("UPLOAD_TOKEN_FILE"),
	} {
		if v == "" {
			return c, fmt.Errorf("%s is required", name)
		}
	}
	tok, err := os.ReadFile(getenv("UPLOAD_TOKEN_FILE"))
	if err != nil {
		return c, fmt.Errorf("UPLOAD_TOKEN_FILE: %w", err)
	}
	// CI holds the same token: long and random (openssl rand -hex 32).
	c.uploadToken = strings.TrimSpace(string(tok))
	if len(c.uploadToken) < 32 {
		return c, errors.New("UPLOAD_TOKEN_FILE: a random token of at least 32 characters is required")
	}
	if c.updateSecret != "" && !iosupdate.ValidSecret(c.updateSecret) {
		return c, fmt.Errorf("UPDATE_TICKET_SECRET: a random secret of at least %d characters is required", iosupdate.MinSecret)
	}
	if u, err := url.Parse(c.releasesURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return c, errors.New("RELEASES_JSON_URL must be the address of releases.json of the releases service")
	}
	if c.perAddressDay, err = positive(getenv("LIMIT_ADDRESS_DAY"), 5); err != nil {
		return c, fmt.Errorf("LIMIT_ADDRESS_DAY: %w", err)
	}
	if c.devicesDay, err = positive(getenv("LIMIT_DEVICES_DAY"), 10); err != nil {
		return c, fmt.Errorf("LIMIT_DEVICES_DAY: %w", err)
	}
	return c, nil
}

// validHost is whether h is a lower-case DNS name with a dot: the name of
// a mirror, as a browser sends it in Host.
func validHost(h string) bool {
	if len(h) > 253 || !strings.Contains(h, ".") {
		return false
	}
	for label := range strings.SplitSeq(h, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}

// site is the site a request came to: the mirror of its Host when that is
// one of mirrors (task 113), publicURL otherwise. Host is the browser's,
// and only a name of the list makes it count.
func (c config) site(r *http.Request) string {
	host := strings.ToLower(r.Host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if slices.Contains(c.mirrors, host) {
		return "https://" + host
	}
	return c.publicURL
}

// page is the install page of the site of r: pageURL, on the mirror when r
// came to one and pageURL is a page of publicURL.
func (c config) page(r *http.Request) string {
	site := c.site(r)
	if site == c.publicURL {
		return c.pageURL
	}
	if rest, ok := strings.CutPrefix(c.pageURL, c.publicURL+"/"); ok {
		return site + "/" + rest
	}
	return c.pageURL
}

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func positive(v string, def int) (int, error) {
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, errors.New("a positive number is required")
	}
	return n, nil
}
