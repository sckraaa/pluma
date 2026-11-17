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

package mtclient

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

func plh1(id []byte, exp uint32, rawURL string) []byte {
	b := append([]byte("PLH1"), id...)
	b = binary.BigEndian.AppendUint32(b, exp)
	return append(b, rawURL...)
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseHttpsToken(t *testing.T) {
	id := bytes.Repeat([]byte{0xab}, 32)
	const exp = 1_900_000_000
	for _, c := range []struct {
		name      string
		token     []byte
		allowHTTP bool
		ok        bool
	}{
		{"https", plh1(id, exp, "https://x.gcorecdn.com/o/ab?md5=a&expires=1"), false, true},
		{"http refused", plh1(id, exp, "http://fakecdn:8080/o/ab"), false, false},
		{"http of a development build", plh1(id, exp, "http://fakecdn:8080/o/ab"), true, true},
		{"no URL", plh1(id, exp, ""), false, false},
		{"cut", plh1(id, exp, "https://x/o")[:20], false, false},
		{"another magic", append([]byte("PLH2"), plh1(id, exp, "https://x/o")[4:]...), false, false},
		{"MTProto token", randomBytes(t, 32), false, false},
		{"not UTF-8", plh1(id, exp, "https://x/\xff"), false, false},
		{"no host", plh1(id, exp, "https:///o/ab"), false, false},
		{"user info", plh1(id, exp, "https://u:p@x/o/ab"), false, false},
		{"other scheme", plh1(id, exp, "ftp://x/o/ab"), true, false},
		{"upper case scheme", plh1(id, exp, "HTTPS://x/o/ab"), false, false},
	} {
		gotID, gotExp, gotURL, err := ParseHttpsToken(c.token, c.allowHTTP)
		if (err == nil) != c.ok {
			t.Errorf("%s: err %v, want ok %v", c.name, err, c.ok)
			continue
		}
		if c.ok && (!bytes.Equal(gotID, id) || gotExp != exp || !strings.HasSuffix(string(c.token), gotURL)) {
			t.Errorf("%s: %x, %d, %q", c.name, gotID, gotExp, gotURL)
		}
		if err != nil && strings.Contains(err.Error(), "gcorecdn") {
			t.Errorf("%s: the error names the URL: %v", c.name, err)
		}
	}
}

func TestParseHttpsRedirect(t *testing.T) {
	token := plh1(bytes.Repeat([]byte{1}, 32), 1_900_000_000, "https://x/o/01")
	redirect := func(dc int32, key, iv int) *mtproto.Upload_File {
		return mtproto.MakeTLUploadFileCdnRedirect(&mtproto.Upload_File{
			DcId: dc, FileToken: token, EncryptionKey: make([]byte, key), EncryptionIv: make([]byte, iv),
		}).To_Upload_File()
	}
	r, err := ParseHttpsRedirect(redirect(201, 32, 16), false)
	if err != nil || r.DC != 201 || r.URL != "https://x/o/01" || r.Expires.Unix() != 1_900_000_000 || !bytes.Equal(r.FileToken, token) {
		t.Fatalf("ParseHttpsRedirect: %+v, %v", r, err)
	}
	for _, f := range []*mtproto.Upload_File{redirect(0, 32, 16), redirect(201, 16, 16), redirect(201, 32, 12),
		mtproto.MakeTLUploadFile(&mtproto.Upload_File{Bytes: []byte{1}}).To_Upload_File()} {
		if _, err := ParseHttpsRedirect(f, true); err == nil {
			t.Errorf("ParseHttpsRedirect took %v", f)
		}
	}
}

func TestPlumaCdnParams(t *testing.T) {
	kv := PlumaCdnParams().GetValue_VECTORJSONOBJECTVALUE()
	if len(kv) != 1 || kv[0].GetKey() != "pluma_cdn" || kv[0].GetValue().GetPredicateName() != mtproto.Predicate_jsonNumber || kv[0].GetValue().GetValue_FLOAT64() != 1 {
		t.Fatalf("PlumaCdnParams = %v", PlumaCdnParams())
	}
}

// fakeCDN serves content by Range with http.ServeContent, or misbehaves as
// mode says.
type fakeCDN struct {
	content []byte
	mu      sync.Mutex
	mode    string
	fails   int // answers of 502 before the good ones
	hits    atomic.Int32
	headers http.Header
}

func (f *fakeCDN) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.hits.Add(1)
	f.mu.Lock()
	mode := f.mode
	f.headers = r.Header.Clone()
	fail := f.fails > 0
	if fail {
		f.fails--
	}
	// The test replaces content between requests, never changes it in
	// place.
	content := f.content
	f.mu.Unlock()
	if fail {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	switch mode {
	case "403", "410", "404":
		var code int
		_, _ = fmt.Sscan(mode, &code)
		w.WriteHeader(code)
		return
	case "full200":
		w.Header().Set("Content-Length", fmt.Sprint(len(content)))
		_, _ = w.Write(content)
		return
	case "redirect":
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
		return
	case "badrange", "badtotal", "short", "long":
		var a, b int64
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &a, &b); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		body := content[a : b+1]
		total := int64(len(content))
		switch mode {
		case "badrange":
			a, b = a+16, b+16
		case "badtotal":
			total++
		case "short":
			body = body[:len(body)-1]
		case "long":
			body = append(append([]byte(nil), body...), 0)
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", a, b, total))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body)
		return
	}
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(content))
}

// setContent replaces what f serves.
func (f *fakeCDN) setContent(content []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.content = content
}

func TestFetchRange(t *testing.T) {
	content := randomBytes(t, 300<<10)
	cdn := &fakeCDN{content: content}
	srv := httptest.NewServer(cdn)
	defer srv.Close()
	f := &HttpsFetcher{}
	ctx := context.Background()
	total := int64(len(content))

	got, err := f.FetchRange(ctx, srv.URL+"/o/ab", 128<<10, 128<<10, total)
	if err != nil || !bytes.Equal(got, content[128<<10:256<<10]) {
		t.Fatalf("a range: %d bytes, %v", len(got), err)
	}
	cdn.mu.Lock()
	h := cdn.headers
	cdn.mu.Unlock()
	if h.Get("Range") != "bytes=131072-262143" || h.Get("Accept-Encoding") != "identity" || h.Get("User-Agent") != "Pluma" || h.Get("Cookie") != "" {
		t.Errorf("request headers %v", h)
	}
	// The last range is cut at the end of the file.
	if got, err = f.FetchRange(ctx, srv.URL, 256<<10, 128<<10, total); err != nil || !bytes.Equal(got, content[256<<10:]) {
		t.Fatalf("the last range: %d bytes, %v", len(got), err)
	}

	for _, c := range []struct {
		mode      string
		status    int
		reason    string
		refresh   bool
		transport bool
	}{
		{"403", 403, "status", true, false},
		{"410", 410, "status", true, false},
		{"404", 404, "status", false, false},
		{"full200", 200, "status", false, false},
		{"redirect", 302, "status", false, false},
		{"badrange", 206, "content-range", false, false},
		{"badtotal", 206, "content-range", false, false},
		{"short", 206, "", false, false},
		{"long", 206, "", false, false},
	} {
		cdn.mu.Lock()
		cdn.mode = c.mode
		cdn.mu.Unlock()
		cdn.hits.Store(0)
		got, err := f.FetchRange(ctx, srv.URL, 0, 128<<10, total)
		var fe *CdnFetchError
		if !errors.As(err, &fe) || got != nil {
			t.Errorf("%s: %d bytes, %v; want a CdnFetchError", c.mode, len(got), err)
			continue
		}
		if fe.Status != c.status || (c.reason != "" && fe.Reason != c.reason) || fe.Refresh() != c.refresh || fe.Transport() != c.transport {
			t.Errorf("%s: %+v", c.mode, fe)
		}
		if n := cdn.hits.Load(); n != 1 {
			t.Errorf("%s: %d requests, want 1: no retry and no redirect followed", c.mode, n)
		}
		if strings.Contains(err.Error(), srv.URL) {
			t.Errorf("%s: the error names the URL: %v", c.mode, err)
		}
	}

	// A 502 is asked again once: a second 502 is the failure, a good answer
	// the range.
	cdn.mu.Lock()
	cdn.mode, cdn.fails = "", 2
	cdn.mu.Unlock()
	cdn.hits.Store(0)
	start := time.Now()
	_, err = f.FetchRange(ctx, srv.URL, 0, 128<<10, total)
	var fe *CdnFetchError
	if !errors.As(err, &fe) || fe.Status != 502 || !fe.Transport() || cdn.hits.Load() != 2 {
		t.Errorf("two 502: %v after %d requests", err, cdn.hits.Load())
	}
	if d := time.Since(start); d < cdnRetryMin {
		t.Errorf("the retry came after %v", d)
	}
	cdn.mu.Lock()
	cdn.fails = 1
	cdn.mu.Unlock()
	if got, err = f.FetchRange(ctx, srv.URL, 0, 128<<10, total); err != nil || !bytes.Equal(got, content[:128<<10]) {
		t.Errorf("a 502 and then the range: %d bytes, %v", len(got), err)
	}

	// No server: a transport failure, retried.
	srv.Close()
	_, err = f.FetchRange(ctx, srv.URL, 0, 128<<10, total)
	if !errors.As(err, &fe) || fe.Status != 0 || !fe.Transport() || fe.Refresh() {
		t.Errorf("no server: %v", err)
	}
}

// encryptCTR is the cipher the origin serves: AES-256-CTR from the IV,
// whose counter is the offset in 16-byte blocks.
func encryptCTR(t *testing.T, key, iv, plain []byte) []byte {
	t.Helper()
	b, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, len(plain))
	cipher.NewCTR(b, iv).XORKeyStream(out, plain)
	return out
}

func hashesOf(plain []byte) []*mtproto.FileHash {
	var out []*mtproto.FileHash
	for off := 0; off < len(plain); off += CdnHashChunk {
		piece := plain[off:min(off+CdnHashChunk, len(plain))]
		sum := sha256.Sum256(piece)
		out = append(out, mtproto.MakeTLFileHash(&mtproto.FileHash{Offset_INT64: int64(off), Limit: int32(len(piece)), Hash: sum[:]}).To_FileHash())
	}
	return out
}

func TestHttpsDownload(t *testing.T) {
	plain := randomBytes(t, 1<<20+5)
	key, iv := randomBytes(t, 32), append(randomBytes(t, 12), 0, 0, 0, 0)
	cdn := &fakeCDN{content: encryptCTR(t, key, iv, plain)}
	srv := httptest.NewServer(cdn)
	defer srv.Close()
	r := &HttpsRedirect{DC: 201, URL: srv.URL + "/o/ab", Key: key, IV: iv, Hashes: hashesOf(plain)}
	ctx := context.Background()

	for _, part := range []int{128 << 10, 512 << 10} {
		got, err := NewHttpsDownload(nil, &HttpsFetcher{}, r, int64(len(plain))).All(ctx, part)
		if err != nil || !bytes.Equal(got, plain) {
			t.Fatalf("by %d: %d bytes, %v", part, len(got), err)
		}
	}
	if _, err := NewHttpsDownload(nil, &HttpsFetcher{}, r, int64(len(plain))).Read(ctx, 1000, 128<<10); err == nil {
		t.Error("a range that is not of whole pieces was read")
	}

	// A byte changed on the way: the piece is refused, nothing returned.
	good := cdn.content
	tampered := slices.Clone(good)
	tampered[200<<10] ^= 1
	cdn.setContent(tampered)
	got, err := NewHttpsDownload(nil, &HttpsFetcher{}, r, int64(len(plain))).Read(ctx, 0, 512<<10)
	if !errors.Is(err, ErrCdnHashMismatch) || got != nil {
		t.Fatalf("a changed byte: %d bytes, %v", len(got), err)
	}
	cdn.setContent(good)

	// A cipher made with the wrong IV rule (the counter from 0 at every
	// offset) is refused past the first piece.
	wrong := &HttpsRedirect{URL: r.URL, Key: key, IV: iv, Hashes: r.Hashes}
	d := NewHttpsDownload(nil, &HttpsFetcher{}, wrong, int64(len(plain)))
	cdn.setContent(append(make([]byte, 128<<10), encryptCTR(t, key, iv, plain[128<<10:])...))
	if _, err := d.Read(ctx, 128<<10, 128<<10); !errors.Is(err, ErrCdnHashMismatch) {
		t.Fatalf("a cipher with the counter from 0 at 128 KB: %v", err)
	}
}

func TestCdnDecryptCounter(t *testing.T) {
	key, iv := randomBytes(t, 32), append(randomBytes(t, 12), 0, 0, 0, 0)
	plain := randomBytes(t, 4096)
	enc := encryptCTR(t, key, iv, plain)
	for _, off := range []int64{0, 16, 1024, 4080} {
		got, err := CdnDecrypt(key, iv, off, enc[off:])
		if err != nil || !bytes.Equal(got, plain[off:]) {
			t.Fatalf("at %d: %v", off, err)
		}
	}
	if _, err := CdnDecrypt(key, iv, 8, enc[8:]); err == nil {
		t.Error("CdnDecrypt took an offset not divisible by 16")
	}
}
