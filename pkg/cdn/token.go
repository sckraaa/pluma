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
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net/url"
	"unicode/utf8"
)

// The file_token of upload.fileCdnRedirect to an HTTPS delivery (ADR 0071
// §2, task 107 "Формат редиректа"):
//
//	"PLH1" | id[32] | exp:uint32be | url (UTF-8 to the end)
//
// id is cdn_files.file_token, exp the unix second the URL stops working and
// url the signed URL of the file's cipher. The patched clients recognise
// the prefix and download url over HTTPS instead of connecting to dc_id;
// the home takes the same token back in upload.getCdnFileHashes.
const (
	// RedirectTokenMagic starts every file_token of an HTTPS delivery.
	RedirectTokenMagic = "PLH1"
	// redirectTokenHead is the size of the token before the URL.
	redirectTokenHead = len(RedirectTokenMagic) + TokenSize + 4
	// maxRedirectURL bounds the URL of a token. A signed URL of Gcore is
	// about 160 bytes; the bound only keeps a token of junk from being
	// taken for one.
	maxRedirectURL = 2048
)

var errRedirectToken = errors.New("cdn: not a PLH1 file_token")

// EncodeRedirectToken returns the file_token of the file whose
// cdn_files.file_token is id, downloaded by rawURL until exp (unix
// seconds).
func EncodeRedirectToken(id []byte, exp int64, rawURL string) ([]byte, error) {
	if len(id) != TokenSize {
		return nil, fmt.Errorf("cdn: redirect token id of %d bytes, want %d", len(id), TokenSize)
	}
	if exp <= 0 || exp > math.MaxUint32 {
		return nil, fmt.Errorf("cdn: redirect token expiry %d out of range", exp)
	}
	if err := checkRedirectURL(rawURL); err != nil {
		return nil, err
	}
	b := make([]byte, 0, redirectTokenHead+len(rawURL))
	b = append(b, RedirectTokenMagic...)
	b = append(b, id...)
	b = binary.BigEndian.AppendUint32(b, uint32(exp))
	return append(b, rawURL...), nil
}

// ParseRedirectToken splits a file_token made by EncodeRedirectToken. It is
// strict: the prefix, the length, a non-empty valid UTF-8 URL with a scheme
// of HTTP(S) and a host. id is a copy.
func ParseRedirectToken(b []byte) (id []byte, exp int64, rawURL string, err error) {
	if len(b) <= redirectTokenHead || !bytes.HasPrefix(b, []byte(RedirectTokenMagic)) {
		return nil, 0, "", errRedirectToken
	}
	id = bytes.Clone(b[len(RedirectTokenMagic) : len(RedirectTokenMagic)+TokenSize])
	exp = int64(binary.BigEndian.Uint32(b[len(RedirectTokenMagic)+TokenSize:]))
	rawURL = string(b[redirectTokenHead:])
	if exp == 0 {
		return nil, 0, "", errRedirectToken
	}
	if err = checkRedirectURL(rawURL); err != nil {
		return nil, 0, "", err
	}
	return id, exp, rawURL, nil
}

// checkRedirectURL checks the URL of a token. http:// is let through here:
// the stand of the e2e tests serves its fake CDN over it
// (CDN_DEV_ALLOW_HTTP); whether a delivery may use it is the config's
// decision, and a release client refuses it (task 107, client contract
// p. 3).
func checkRedirectURL(rawURL string) error {
	if rawURL == "" || len(rawURL) > maxRedirectURL || !utf8.ValidString(rawURL) {
		return errRedirectToken
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.Fragment != "" {
		return errRedirectToken
	}
	return nil
}
