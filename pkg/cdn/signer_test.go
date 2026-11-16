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
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func readVectors(t *testing.T, name string, v any) {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

// TestGcoreMD5Reference checks the Secure Token against the command of the
// page of Gcore run with OpenSSL (testdata/gcore-md5.json).
func TestGcoreMD5Reference(t *testing.T) {
	var ref struct {
		Vectors []struct {
			BaseURL string `json:"base_url"`
			Path    string `json:"path"`
			Key     string `json:"key"`
			Expires int64  `json:"expires"`
			MD5     string `json:"md5"`
			URL     string `json:"url"`
		} `json:"vectors"`
	}
	readVectors(t, "gcore-md5.json", &ref)
	if len(ref.Vectors) == 0 {
		t.Fatal("no vectors")
	}
	const ttl = 6 * time.Hour
	for _, v := range ref.Vectors {
		if got := gcoreToken(strconv.FormatInt(v.Expires, 10), v.Path, v.Key); got != v.MD5 {
			t.Errorf("%s: md5 %s, want %s", v.Path, got, v.MD5)
		}
		s, err := NewGcoreSigner(v.BaseURL, v.Key, ttl)
		if err != nil {
			t.Fatal(err)
		}
		url, exp, err := s.SignURL(v.Path, time.Unix(v.Expires, 0).Add(-ttl))
		if err != nil || url != v.URL || exp != v.Expires {
			t.Errorf("%s: %s until %d, %v; want %s", v.Path, url, exp, err, v.URL)
		}
	}
}

// TestGcoreMD5Properties: a change of the path, the time or the key
// changes the token.
func TestGcoreMD5Properties(t *testing.T) {
	const exp, path, key = "1763300000", "/o/00", "Zx9kQ2mT7pLw4vRb"
	base := gcoreToken(exp, path, key)
	for name, got := range map[string]string{
		"path":    gcoreToken(exp, "/o/01", key),
		"expires": gcoreToken("1763300001", path, key),
		"key":     gcoreToken(exp, path, "Zx9kQ2mT7pLw4vRc"),
	} {
		if got == base {
			t.Errorf("a change of the %s kept the token", name)
		}
	}
	if len(base) != 22 || strings.ContainsAny(base, "+/=") {
		t.Errorf("token %q is not unpadded base64url of 16 bytes", base)
	}
}

func TestGcoreKey(t *testing.T) {
	for key, ok := range map[string]bool{
		"abcdef":                            true,
		"a1b2c3d4e5f60718293a4b5c6d7e8f90":  true,
		"abcde":                             false,
		"a1b2c3d4e5f60718293a4b5c6d7e8f901": false,
		"abc def":                           false,
		"abcdef\t":                          false,
		"абвгдежз":                          false,
		"":                                  false,
	} {
		if _, err := NewGcoreSigner("https://cdn.example.com", key, time.Hour); (err == nil) != ok {
			t.Errorf("key %q: %v", key, err)
		}
	}
}

type pathHMACVectors struct {
	KeyHex    string `json:"key_hex"`
	KeyBase64 string `json:"key_base64"`
	Vectors   []struct {
		BaseURL    string `json:"base_url"`
		Path       string `json:"path"`
		TTLSeconds int64  `json:"ttl_seconds"`
		Now        int64  `json:"now"`
		Bucket     int64  `json:"bucket"`
		Sig        string `json:"sig"`
		URL        string `json:"url"`
	} `json:"vectors"`
}

// TestPathHMACReference checks the URLs against OpenSSL
// (testdata/path-hmac.json), and that the origin takes each of them.
func TestPathHMACReference(t *testing.T) {
	var ref pathHMACVectors
	readVectors(t, "path-hmac.json", &ref)
	key, err := ParsePathHMACKey(ref.KeyBase64)
	if err != nil || hex.EncodeToString(key) != ref.KeyHex {
		t.Fatalf("key: %v", err)
	}
	if len(ref.Vectors) == 0 {
		t.Fatal("no vectors")
	}
	for _, v := range ref.Vectors {
		s, err := NewPathHMACSigner(v.BaseURL, key, time.Duration(v.TTLSeconds)*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Unix(v.Now, 0)
		url, exp, err := s.SignURL(v.Path, now)
		if err != nil || url != v.URL || exp != v.Bucket {
			t.Errorf("%s at %d: %s until %d, %v; want %s", v.Path, v.Now, url, exp, err, v.URL)
			continue
		}
		object, bucket, err := s.Verify(strings.TrimPrefix(url, v.BaseURL), now)
		if err != nil || object != v.Path || bucket != v.Bucket {
			t.Errorf("%s: verified as %q until %d, %v", url, object, bucket, err)
		}
	}
}

// TestPathHMACWindow: two URLs in one window are the same, in the next
// window they differ; a URL works from ttl to 2·ttl. A window of now is
// (k·ttl, (k+1)·ttl].
func TestPathHMACWindow(t *testing.T) {
	const ttl = 6 * time.Hour
	s, err := NewPathHMACSigner("https://cdn.example.com", randomBytes(t, PathHMACKeySize), ttl)
	if err != nil {
		t.Fatal(err)
	}
	path := ObjectPath(randomBytes(t, TokenSize))
	start := time.Unix(1763337600, 0) // a multiple of ttl
	a, expA, _ := s.SignURL(path, start.Add(time.Second))
	b, expB, _ := s.SignURL(path, start.Add(ttl))
	if a != b || expA != expB || expA != start.Add(2*ttl).Unix() {
		t.Fatalf("one window, two URLs: %s, %s", a, b)
	}
	c, expC, _ := s.SignURL(path, start.Add(ttl+time.Second))
	if c == a || expC != expA+int64(ttl/time.Second) {
		t.Fatalf("the next window kept the URL: %s", c)
	}
	for _, at := range []time.Time{start.Add(time.Second), start.Add(ttl)} {
		_, exp, _ := s.SignURL(path, at)
		if left := exp - at.Unix(); left < int64(ttl/time.Second) || left > 2*int64(ttl/time.Second) {
			t.Errorf("at %v the URL works %d s", at, left)
		}
	}
}

// TestPathHMACVerify: what the origin refuses, and with which error.
func TestPathHMACVerify(t *testing.T) {
	const ttl = time.Hour
	key := randomBytes(t, PathHMACKeySize)
	s, err := NewPathHMACSigner("https://cdn.example.com", key, ttl)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1763300000, 0)
	path := ObjectPath(randomBytes(t, TokenSize))
	url, bucket, _ := s.SignURL(path, now)
	p := strings.TrimPrefix(url, "https://cdn.example.com")
	b := strconv.FormatInt(bucket, 10)
	sig := strings.Split(p, "/")[3]

	other, _ := NewPathHMACSigner("https://cdn.example.com", randomBytes(t, PathHMACKeySize), ttl)
	otherURL, _, _ := other.SignURL(path, now)
	// A signature of the home for a bucket too far ahead.
	far := bucket + 3*int64(ttl/time.Second)
	farPath := "/t/" + strconv.FormatInt(far, 10) + "/" + s.sig(strings.TrimPrefix(path, "/"), strconv.FormatInt(far, 10)) + path

	flip := []byte(sig)
	flip[len(flip)-1] ^= 1
	for name, c := range map[string]struct {
		path string
		at   time.Time
		want error
	}{
		"own":                 {p, now, nil},
		"at the bucket":       {p, time.Unix(bucket, 0), nil},
		"expired":             {p, time.Unix(bucket+1, 0), ErrURLExpired},
		"another key":         {strings.TrimPrefix(otherURL, "https://cdn.example.com"), now, ErrURLInvalid},
		"changed signature":   {"/t/" + b + "/" + string(flip) + path, now, ErrURLInvalid},
		"another object":      {"/t/" + b + "/" + sig + ObjectPath(randomBytes(t, TokenSize)), now, ErrURLInvalid},
		"another bucket":      {"/t/" + strconv.FormatInt(bucket+3600, 10) + "/" + sig + path, now, ErrURLInvalid},
		"leading zero":        {"/t/0" + b + "/" + sig + path, now, ErrURLInvalid},
		"too far ahead":       {farPath, now, ErrURLInvalid},
		"short signature":     {"/t/" + b + "/" + sig[:21] + path, now, ErrURLInvalid},
		"no object":           {"/t/" + b + "/" + sig, now, ErrURLInvalid},
		"no /t/":              {path, now, ErrURLInvalid},
		"signed bare /o/":     {"/o/" + b + "/" + sig + path, now, ErrURLInvalid},
		"expired and changed": {"/t/" + b + "/" + string(flip) + path, time.Unix(bucket+1, 0), ErrURLInvalid},
	} {
		object, _, err := s.Verify(c.path, c.at)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
		if c.want == nil && object != path {
			t.Errorf("%s: object %q", name, object)
		}
	}
}

func TestPathHMACKey(t *testing.T) {
	for in, ok := range map[string]bool{
		"jxwtPkpbbH2On6CxwtPk9QYXKDlKW2x9jp+gscLT5PU=": true,
		"jxwtPkpbbH2On6CxwtPk9QYXKDlKW2x9jp+gscLT5PU":  true,
		"jxwtPkpbbH2On6CxwtPk9QYXKDlKW2x9jp+gscLT5A==": false, // 31 bytes
		"not base64!": false,
		"":            false,
	} {
		if _, err := ParsePathHMACKey(in); (err == nil) != ok {
			t.Errorf("%q: %v", in, err)
		}
	}
	if _, err := NewPathHMACSigner("https://cdn.example.com", make([]byte, 16), time.Hour); err == nil {
		t.Error("a 16-byte key was taken")
	}
	s, _ := NewPathHMACSigner("https://cdn.example.com", make([]byte, PathHMACKeySize), time.Hour)
	if _, _, err := s.SignURL("o/x", time.Now()); err == nil {
		t.Error("a path without a slash was signed")
	}
}
