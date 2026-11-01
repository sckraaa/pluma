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
	"net/url"
	"os"
	"strconv"
	"strings"
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
	}
	u, err := url.Parse(c.publicURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" {
		return c, errors.New("PUBLIC_URL must be the site's https:// address, as https://pluma.chat")
	}
	c.pageURL = or(c.pageURL, c.publicURL+"/iphone/install")
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
	if c.perAddressDay, err = positive(getenv("LIMIT_ADDRESS_DAY"), 5); err != nil {
		return c, fmt.Errorf("LIMIT_ADDRESS_DAY: %w", err)
	}
	if c.devicesDay, err = positive(getenv("LIMIT_DEVICES_DAY"), 10); err != nil {
		return c, fmt.Errorf("LIMIT_DEVICES_DAY: %w", err)
	}
	return c, nil
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
