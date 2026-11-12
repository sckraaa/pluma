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

package twofa

import (
	"net/mail"
	"strings"
)

// MaxEmailLength is the longest recovery email, in bytes (RFC 5321's path
// limit less the brackets).
const MaxEmailLength = 254

// normalizeEmail returns address as the server keeps it, or ok false if it
// is not one bare ASCII address (ADR 0058, p. 4): no display name, no
// brackets, no spaces or control characters, at most MaxEmailLength bytes,
// a domain with a dot. The domain is lower-cased; the local part is kept as
// written, since a server may tell its case.
func normalizeEmail(address string) (string, bool) {
	address = strings.TrimSpace(address)
	if address == "" || len(address) > MaxEmailLength {
		return "", false
	}
	for i := 0; i < len(address); i++ {
		if c := address[i]; c <= ' ' || c >= 0x7f {
			return "", false
		}
	}
	a, err := mail.ParseAddress(address)
	if err != nil || a.Name != "" || a.Address != address {
		return "", false
	}
	at := strings.LastIndexByte(address, '@')
	if at <= 0 {
		return "", false
	}
	local, domain := address[:at], strings.ToLower(address[at+1:])
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") || strings.Contains(domain, "..") {
		return "", false
	}
	return local + "@" + domain, true
}

// emailPattern is how account.password and auth.passwordRecovery show
// address (ADR 0058, p. 10): the first two characters of the local part
// (one if it is shorter than four), "***", "@" and the domain:
// ab***@gmail.com. It has "*", which Android hides under a spoiler
// (LoginActivity.java:5693-5701), and no space, which tdesktop and Web K
// take for a placeholder rather than an address
// (settings_privacy_security.cpp:694, emailSetupSuggestion.ts:27-28).
func emailPattern(address string) string {
	at := strings.LastIndexByte(address, '@')
	if at < 0 {
		return "***"
	}
	local, domain := address[:at], address[at+1:]
	keep := 2
	if len(local) < 4 {
		keep = 1
	}
	keep = min(keep, len(local))
	return local[:keep] + "***@" + domain
}
