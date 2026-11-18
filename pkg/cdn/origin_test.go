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
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// memReader is a FileReader over files in memory with the rules of dfs: a
// part at a multiple of its limit.
type memReader struct {
	mu    sync.Mutex
	files map[FileKey][]byte
	// failAt makes the read of the part at that offset fail (-1: none);
	// shortAt answers it with fewer bytes.
	failAt  int64
	shortAt int64
	// block, when set, holds every read until it is closed.
	block chan struct{}
	// delay makes every read take so long.
	delay time.Duration
	// started is told of every read that starts.
	started chan struct{}
	reads   int
}

func newMemReader() *memReader {
	return &memReader{files: map[FileKey][]byte{}, failAt: -1, shortAt: -1}
}

func (m *memReader) add(key FileKey, b []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[key] = b
}

func (m *memReader) readCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reads
}

func (m *memReader) ReadPart(ctx context.Context, loc *mtproto.InputFileLocation, offset int64, limit int32) ([]byte, error) {
	m.mu.Lock()
	block, started, delay := m.block, m.started, m.delay
	m.mu.Unlock()
	if started != nil {
		started <- struct{}{}
	}
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reads++
	if limit != ReadChunkSize || offset < 0 || offset%int64(limit) != 0 {
		return nil, fmt.Errorf("a part of %d at %d", limit, offset)
	}
	key, ok := Key(loc)
	b, found := m.files[key]
	if !ok || !found {
		return nil, errors.New("no such file")
	}
	if offset == m.failAt {
		return nil, errors.New("dfs is down")
	}
	if offset >= int64(len(b)) {
		return []byte{}, nil
	}
	part := bytes.Clone(b[offset:min(offset+int64(limit), int64(len(b)))])
	if offset == m.shortAt {
		part = part[:len(part)/2]
	}
	return part, nil
}

// usageCounts is a UsageCounter in memory.
type usageCounts struct {
	mu sync.Mutex
	n  map[int32]int64
}

func (u *usageCounts) CountOrigin(dc int32, n int64) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.n == nil {
		u.n = map[int32]int64{}
	}
	u.n[dc] += n
}

func (u *usageCounts) of(dc int32) int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.n[dc]
}

// settled waits up to a second for the count of dc to reach want and returns
// the last count: the origin counts the bytes of an answer after it is
// written, so the client may read the body before the count lands.
func (u *usageCounts) settled(dc int32, want int64) int64 {
	deadline := time.Now().Add(time.Second)
	for {
		got := u.of(dc)
		if got == want || time.Now().After(deadline) {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// prepared makes the row of a file of key with data, its secrets and its
// hashes, as a preparation for the origin leaves it.
func prepared(t *testing.T, s Store, key FileKey, data []byte) *File {
	t.Helper()
	ctx := context.Background()
	f, err := s.Ensure(ctx, &File{Key: key, Size: int64(len(data)), Eligible: true, CreatedAt: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC).UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	if f, err = s.SetKey(ctx, f.RowId, mustSecrets(t)); err != nil {
		t.Fatal(err)
	}
	if err = s.Enqueue(ctx, f.RowId, TargetHomeOrigin, 0, 0); err != nil {
		t.Fatal(err)
	}
	c, err := s.Claim(ctx, TargetHomeOrigin, time.Minute, 0)
	if err != nil || c == nil || c.File.RowId != f.RowId {
		t.Fatalf("Claim: %v, %v", c, err)
	}
	h := NewHasher()
	_, _ = h.Write(data)
	if ok, err := s.MarkReady(ctx, c, h.Sum(), 0); !ok || err != nil {
		t.Fatalf("MarkReady: %v, %v", ok, err)
	}
	if f, err = s.Get(ctx, key); err != nil {
		t.Fatal(err)
	}
	return f
}

// originEnv is an Origin over a MemStore and files in memory, served by an
// HTTP server.
type originEnv struct {
	t      *testing.T
	set    *Settings
	store  *MemStore
	reader *memReader
	usage  *usageCounts
	clock  *clock
	origin *Origin
	srv    *httptest.Server
	auth   string
}

func newOriginEnv(t *testing.T, set *Settings) *originEnv {
	t.Helper()
	e := &originEnv{t: t, set: set, store: NewMemStore(), reader: newMemReader(), usage: &usageCounts{}, clock: newClock()}
	var err error
	if e.origin, err = NewOrigin(e.store, set, e.reader, e.usage); err != nil {
		t.Fatal(err)
	}
	e.origin.now = e.clock.now
	e.auth = string(set.Origin.AuthValue)
	e.srv = httptest.NewServer(e.origin)
	t.Cleanup(e.srv.Close)
	return e
}

// file adds a prepared file of data under key and returns its row.
func (e *originEnv) file(key FileKey, data []byte) *File {
	e.t.Helper()
	e.reader.add(key, data)
	return prepared(e.t, e.store, key, data)
}

// answer is the status and the headers of a response whose body do has
// read and closed.
type answer struct {
	StatusCode int
	Header     http.Header
}

// do sends a request to the origin with the auth header (unless header
// sets it) and returns the answer with its body read.
func (e *originEnv) do(method, path string, header map[string]string) (answer, []byte) {
	e.t.Helper()
	req, err := http.NewRequest(method, e.srv.URL+path, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set(OriginAuthHeader, e.auth)
	for k, v := range header {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	res, err := e.srv.Client().Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		e.t.Fatalf("%s %s: body: %v", method, path, err)
	}
	return answer{StatusCode: res.StatusCode, Header: res.Header}, body
}

func docKey(id int64) FileKey { return FileKey{Kind: KindDocument, Id: id} }

// cipherOf is the cipher of the whole file f with data.
func cipherOf(t *testing.T, f *File, data []byte) []byte {
	t.Helper()
	return encryptAll(t, f.Secrets.Key, f.Secrets.IV, data, len(data))
}

func TestOriginAuth(t *testing.T) {
	e := newOriginEnv(t, testSettings(t))
	f := e.file(docKey(1), refFile())
	known, unknown := ObjectPath(f.Secrets.Token), ObjectPath(bytes.Repeat([]byte{7}, TokenSize))

	wrong := strings.Repeat("0", originAuthDigits)
	if wrong == e.auth {
		wrong = strings.Repeat("1", originAuthDigits)
	}
	// Without the header, or with another value, a file that exists and
	// one that does not get the same answer.
	for _, auth := range []string{"", wrong, e.auth[:originAuthDigits-1], e.auth + "0", strings.ToUpper(e.auth)} {
		if auth == e.auth {
			continue
		}
		var bodies []string
		for _, path := range []string{known, unknown, CanaryPath, "/nowhere"} {
			res, body := e.do(http.MethodGet, path, map[string]string{OriginAuthHeader: auth})
			if res.StatusCode != http.StatusForbidden {
				t.Errorf("auth %q, %s: %d, want 403", auth, path, res.StatusCode)
			}
			bodies = append(bodies, string(body))
		}
		for _, b := range bodies[1:] {
			if b != bodies[0] {
				t.Errorf("auth %q: the refusals differ: %q", auth, bodies)
			}
		}
	}
	if res, _ := e.do(http.MethodGet, unknown, nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("unknown id with the header: %d, want 404", res.StatusCode)
	}
	if res, _ := e.do(http.MethodGet, known, nil); res.StatusCode != http.StatusOK {
		t.Errorf("known id with the header: %d, want 200", res.StatusCode)
	}
}

func TestOriginMethods(t *testing.T) {
	e := newOriginEnv(t, testSettings(t))
	f := e.file(docKey(1), refFile())
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions, http.MethodPatch} {
		res, _ := e.do(m, ObjectPath(f.Secrets.Token), nil)
		if res.StatusCode != http.StatusMethodNotAllowed || res.Header.Get("Allow") != "GET, HEAD" {
			t.Errorf("%s: %d, Allow %q", m, res.StatusCode, res.Header.Get("Allow"))
		}
	}
}

func TestOriginNotFound(t *testing.T) {
	e := newOriginEnv(t, testSettings(t))
	ctx := context.Background()
	f := e.file(docKey(1), refFile())
	hexId := hex.EncodeToString(f.Secrets.Token)

	// A file with its key and no hashes: not prepared.
	unprepared, err := e.store.Ensure(ctx, &File{Key: docKey(2), Size: refSize, Eligible: true})
	if err != nil {
		t.Fatal(err)
	}
	if unprepared, err = e.store.SetKey(ctx, unprepared.RowId, mustSecrets(t)); err != nil {
		t.Fatal(err)
	}
	// A secret chat file is never served, prepared or not.
	secret := e.file(FileKey{Kind: KindEncrypted, Id: 3}, refFile())
	// A photo while CDN_KINDS has documents only.
	photoSet := *e.set
	photoSet.Kinds = []FileKind{KindDocument}
	photo := e.file(FileKey{Kind: KindPhoto, Id: 4, Thumb: "y"}, refFile())
	po, err := NewOrigin(e.store, &photoSet, e.reader, e.usage)
	if err != nil {
		t.Fatal(err)
	}
	psrv := httptest.NewServer(po)
	defer psrv.Close()

	for _, path := range []string{
		ObjectPath(bytes.Repeat([]byte{9}, TokenSize)),
		ObjectPath(unprepared.Secrets.Token),
		ObjectPath(secret.Secrets.Token),
		"/o/" + strings.ToUpper(hexId),
		"/o/" + hexId[:62],
		"/o/" + hexId + "/",
		"/o/" + hexId + "0",
		"/o//" + hexId,
		"/o/%3" + hexId[1:],
		"/c/v2",
		"/",
		"/t/1/" + strings.Repeat("A", 22) + "/o/" + hexId, // no path-hmac delivery
	} {
		if res, _ := e.do(http.MethodGet, path, nil); res.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", path, res.StatusCode)
		}
	}
	if e.reader.readCount() != 0 {
		t.Errorf("%d reads of dfs for refused paths", e.reader.readCount())
	}

	req, _ := http.NewRequest(http.MethodGet, psrv.URL+ObjectPath(photo.Secrets.Token), nil)
	req.Header.Set(OriginAuthHeader, e.auth)
	res, err := psrv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("a photo outside CDN_KINDS: %d, want 404", res.StatusCode)
	}
}

// TestOriginRanges checks that the bytes of any range, at any offset, are
// that range of the cipher of the whole file, as the clients decrypt it.
func TestOriginRanges(t *testing.T) {
	e := newOriginEnv(t, testSettings(t))
	data := refFile()
	f := e.file(docKey(1), data)
	full := cipherOf(t, f, data)
	path := ObjectPath(f.Secrets.Token)
	size := int64(len(data))
	etag := `"` + hex.EncodeToString(f.Secrets.Token) + `"`
	lastModified := time.UnixMilli(f.CreatedAt).UTC().Format(http.TimeFormat)

	cases := []struct {
		rng        string
		start, end int64 // inclusive
	}{
		{"bytes=0-0", 0, 0},
		{"bytes=0-131071", 0, 131071},
		{"bytes=15-16", 15, 16},
		{"bytes=7-100", 7, 100},
		{"bytes=1048570-1048580", 1048570, 1048580},   // across two parts of dfs
		{"bytes=1000003-3000000", 1000003, 3000000},   // across three
		{"bytes=131072-135167", 131072, 135167},       // the range of the checks
		{"bytes=3145728-", 3145728, size - 1},         // the tail, a-
		{"bytes=-100", size - 100, size - 1},          // the last bytes, -n
		{"bytes=3145700-99999999", 3145700, size - 1}, // past the end, cut
		{fmt.Sprintf("bytes=%d-%d", size-1, size-1), size - 1, size - 1},
	}
	for _, c := range cases {
		res, body := e.do(http.MethodGet, path, map[string]string{"Range": c.rng})
		if res.StatusCode != http.StatusPartialContent {
			t.Errorf("%s: %d, want 206", c.rng, res.StatusCode)
			continue
		}
		if got, want := res.Header.Get("Content-Range"), fmt.Sprintf("bytes %d-%d/%d", c.start, c.end, size); got != want {
			t.Errorf("%s: Content-Range %q, want %q", c.rng, got, want)
		}
		if got, want := res.Header.Get("Content-Length"), strconv.FormatInt(c.end-c.start+1, 10); got != want {
			t.Errorf("%s: Content-Length %s, want %s", c.rng, got, want)
		}
		if !bytes.Equal(body, full[c.start:c.end+1]) {
			t.Errorf("%s: other bytes than the cipher of the file", c.rng)
		}
		checkObjectHeaders(t, c.rng, res.Header, etag, lastModified)
	}

	res, body := e.do(http.MethodGet, path, nil)
	if res.StatusCode != http.StatusOK || !bytes.Equal(body, full) || res.Header.Get("Content-Length") != strconv.FormatInt(size, 10) {
		t.Errorf("no Range: %d, %d bytes, Content-Length %s", res.StatusCode, len(body), res.Header.Get("Content-Length"))
	}
	checkObjectHeaders(t, "no Range", res.Header, etag, lastModified)

	// The decrypted bytes are the file, by the IV rule of the clients.
	part := bytes.Clone(full[2<<20 : 2<<20+HashPartSize])
	if err := DecryptAt(f.Secrets.Key, f.Secrets.IV, 2<<20, part); err != nil || !bytes.Equal(part, data[2<<20:2<<20+HashPartSize]) {
		t.Errorf("a part does not decrypt to the file: %v", err)
	}

	for _, rng := range []string{fmt.Sprintf("bytes=%d-", size), fmt.Sprintf("bytes=%d-%d", size+10, size+20)} {
		res, _ := e.do(http.MethodGet, path, map[string]string{"Range": rng})
		if res.StatusCode != http.StatusRequestedRangeNotSatisfiable || res.Header.Get("Content-Range") != fmt.Sprintf("bytes */%d", size) {
			t.Errorf("%s: %d, Content-Range %q, want 416 bytes */%d", rng, res.StatusCode, res.Header.Get("Content-Range"), size)
		}
		if strings.Contains(res.Header.Get("Cache-Control"), "immutable") {
			t.Errorf("%s: a refusal may be cached: %q", rng, res.Header.Get("Cache-Control"))
		}
	}
}

func checkObjectHeaders(t *testing.T, what string, h http.Header, etag, lastModified string) {
	t.Helper()
	want := map[string]string{
		"Content-Type":     "application/octet-stream",
		"Accept-Ranges":    "bytes",
		"ETag":             etag,
		"Last-Modified":    lastModified,
		"Cache-Control":    "public, max-age=2592000, immutable",
		"Content-Encoding": "",
	}
	for k, v := range want {
		if got := h.Get(k); got != v {
			t.Errorf("%s: %s %q, want %q", what, k, got, v)
		}
	}
}

func TestOriginMultipleRanges(t *testing.T) {
	e := newOriginEnv(t, testSettings(t))
	data := refFile()
	f := e.file(docKey(1), data)
	full := cipherOf(t, f, data)

	res, body := e.do(http.MethodGet, ObjectPath(f.Secrets.Token), map[string]string{"Range": "bytes=5-20,1048570-1048590,-3"})
	mt, params, err := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if res.StatusCode != http.StatusPartialContent || err != nil || mt != "multipart/byteranges" {
		t.Fatalf("%d, Content-Type %q: %v", res.StatusCode, res.Header.Get("Content-Type"), err)
	}
	want := [][2]int64{{5, 20}, {1048570, 1048590}, {refSize - 3, refSize - 1}}
	r := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	for i := 0; ; i++ {
		p, err := r.NextPart()
		if errors.Is(err, io.EOF) {
			if i != len(want) {
				t.Errorf("%d parts, want %d", i, len(want))
			}
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if i >= len(want) {
			t.Fatalf("more than %d parts", len(want))
		}
		got, _ := io.ReadAll(p)
		w := want[i]
		if p.Header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", w[0], w[1], refSize) || !bytes.Equal(got, full[w[0]:w[1]+1]) {
			t.Errorf("part %d: %q, other bytes than the cipher", i, p.Header.Get("Content-Range"))
		}
	}
}

func TestOriginQueryIgnored(t *testing.T) {
	e := newOriginEnv(t, testSettings(t))
	f := e.file(docKey(1), refFile())
	path := ObjectPath(f.Secrets.Token)
	res1, body1 := e.do(http.MethodGet, path, map[string]string{"Range": "bytes=100-5000"})
	res2, body2 := e.do(http.MethodGet, path+"?md5=AAAAAAAAAAAAAAAAAAAAAA&expires=1", map[string]string{"Range": "bytes=100-5000"})
	res3, body3 := e.do(http.MethodGet, path+"?anything=%2e%2e", map[string]string{"Range": "bytes=100-5000"})
	for i, r := range []answer{res2, res3} {
		if r.StatusCode != res1.StatusCode || r.Header.Get("ETag") != res1.Header.Get("ETag") || r.Header.Get("Content-Range") != res1.Header.Get("Content-Range") {
			t.Errorf("query %d changed the answer: %d %q", i, r.StatusCode, r.Header.Get("Content-Range"))
		}
	}
	if !bytes.Equal(body1, body2) || !bytes.Equal(body1, body3) {
		t.Error("a query string changed the bytes")
	}
}

func TestOriginHead(t *testing.T) {
	e := newOriginEnv(t, testSettings(t))
	f := e.file(docKey(1), refFile())
	path := ObjectPath(f.Secrets.Token)
	for _, rng := range []string{"", "bytes=10-20000"} {
		get, _ := e.do(http.MethodGet, path, map[string]string{"Range": rng})
		reads := e.reader.readCount()
		head, body := e.do(http.MethodHead, path, map[string]string{"Range": rng})
		if head.StatusCode != get.StatusCode || len(body) != 0 {
			t.Errorf("HEAD %q: %d with %d bytes, GET %d", rng, head.StatusCode, len(body), get.StatusCode)
		}
		for _, k := range []string{"Content-Length", "Content-Range", "Content-Type", "ETag", "Last-Modified", "Cache-Control", "Accept-Ranges"} {
			if head.Header.Get(k) != get.Header.Get(k) {
				t.Errorf("HEAD %q: %s %q, GET %q", rng, k, head.Header.Get(k), get.Header.Get(k))
			}
		}
		if e.reader.readCount() != reads {
			t.Errorf("HEAD %q read dfs", rng)
		}
	}
}

func TestOriginConditional(t *testing.T) {
	e := newOriginEnv(t, testSettings(t))
	data := refFile()
	f := e.file(docKey(1), data)
	full := cipherOf(t, f, data)
	path := ObjectPath(f.Secrets.Token)
	etag := `"` + hex.EncodeToString(f.Secrets.Token) + `"`

	if res, body := e.do(http.MethodGet, path, map[string]string{"If-None-Match": etag}); res.StatusCode != http.StatusNotModified || len(body) != 0 {
		t.Errorf("If-None-Match: %d, %d bytes, want 304", res.StatusCode, len(body))
	}
	since := time.UnixMilli(f.CreatedAt).Add(time.Hour).UTC().Format(http.TimeFormat)
	if res, _ := e.do(http.MethodGet, path, map[string]string{"If-Modified-Since": since}); res.StatusCode != http.StatusNotModified {
		t.Errorf("If-Modified-Since: %d, want 304", res.StatusCode)
	}
	if res, _ := e.do(http.MethodGet, path, map[string]string{"If-None-Match": `"other"`}); res.StatusCode != http.StatusOK {
		t.Errorf("If-None-Match of another: %d, want 200", res.StatusCode)
	}
	res, body := e.do(http.MethodGet, path, map[string]string{"Range": "bytes=10-20", "If-Range": `"other"`})
	if res.StatusCode != http.StatusOK || !bytes.Equal(body, full) {
		t.Errorf("If-Range of another: %d, %d bytes, want 200 with the whole file", res.StatusCode, len(body))
	}
	res, body = e.do(http.MethodGet, path, map[string]string{"Range": "bytes=10-20", "If-Range": etag})
	if res.StatusCode != http.StatusPartialContent || !bytes.Equal(body, full[10:21]) {
		t.Errorf("If-Range of this: %d, want 206", res.StatusCode)
	}
}

func TestOriginCanary(t *testing.T) {
	e := newOriginEnv(t, testSettings(t))
	if len(canaryBytes) != canarySize {
		t.Fatalf("canary of %d bytes", len(canaryBytes))
	}
	// SHA-256(seed ‖ uint32_be(i)), one after another.
	for _, i := range []uint32{0, 1, 4095, 8191} {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], i)
		sum := sha256.Sum256(append([]byte("pluma-cdn-canary-v1"), n[:]...))
		if !bytes.Equal(canaryBytes[i*32:i*32+32], sum[:]) {
			t.Errorf("block %d of the canary", i)
		}
	}
	res, body := e.do(http.MethodGet, CanaryPath, map[string]string{"Range": "bytes=131072-135167"})
	if res.StatusCode != http.StatusPartialContent || res.Header.Get("Content-Range") != "bytes 131072-135167/262144" || !bytes.Equal(body, canaryBytes[131072:135168]) {
		t.Errorf("canary range: %d %q", res.StatusCode, res.Header.Get("Content-Range"))
	}
	if res.Header.Get("Cache-Control") != "public, max-age=2592000, immutable" || res.Header.Get("ETag") != canaryETag {
		t.Errorf("canary headers: %v", res.Header)
	}
	if res, body = e.do(http.MethodGet, CanaryPath, nil); res.StatusCode != http.StatusOK || !bytes.Equal(body, canaryBytes) {
		t.Errorf("whole canary: %d, %d bytes", res.StatusCode, len(body))
	}
	if got := e.usage.settled(testDc, 4096+canarySize); got != 4096+canarySize {
		t.Errorf("counted %d bytes for the delivery, want %d", got, 4096+canarySize)
	}
}

func TestOriginUsage(t *testing.T) {
	e := newOriginEnv(t, testSettings(t))
	f := e.file(docKey(1), refFile())
	e.do(http.MethodGet, ObjectPath(f.Secrets.Token), map[string]string{"Range": "bytes=0-999"})
	e.do(http.MethodHead, ObjectPath(f.Secrets.Token), nil)
	e.do(http.MethodGet, ObjectPath(bytes.Repeat([]byte{1}, TokenSize)), nil)
	if got := e.usage.settled(testDc, 1000); got != 1000 {
		t.Errorf("counted %d bytes, want 1000: only the bodies of objects", got)
	}
}

func TestOriginBusy(t *testing.T) {
	set := testSettings(t)
	set.Origin.MaxConcurrent = 1
	e := newOriginEnv(t, set)
	f := e.file(docKey(1), refFile())
	path := ObjectPath(f.Secrets.Token)

	block := make(chan struct{})
	started := make(chan struct{}, 4)
	e.reader.mu.Lock()
	e.reader.block, e.reader.started = block, started
	e.reader.mu.Unlock()
	done := make(chan int, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodGet, e.srv.URL+path, nil)
		req.Header.Set(OriginAuthHeader, e.auth)
		res, err := e.srv.Client().Do(req)
		if err != nil {
			done <- 0
			return
		}
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
		done <- res.StatusCode
	}()
	<-started

	res, _ := e.do(http.MethodGet, path, nil)
	if res.StatusCode != http.StatusServiceUnavailable || res.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("past MaxConcurrent: %d, Cache-Control %q, want 503 no-store", res.StatusCode, res.Header.Get("Cache-Control"))
	}
	// The canary reads no file and is not bound.
	if res, _ := e.do(http.MethodGet, CanaryPath, nil); res.StatusCode != http.StatusOK {
		t.Errorf("canary while busy: %d", res.StatusCode)
	}
	e.reader.mu.Lock()
	e.reader.started = nil
	e.reader.mu.Unlock()
	close(block)
	if code := <-done; code != http.StatusOK {
		t.Errorf("the first request: %d", code)
	}
	if res, _ := e.do(http.MethodGet, path, map[string]string{"Range": "bytes=0-0"}); res.StatusCode != http.StatusPartialContent {
		t.Errorf("after the first ended: %d, want 206", res.StatusCode)
	}
}

// TestOriginReadFailureCutsConnection checks that a read of dfs that fails
// mid-answer cuts the connection, so a CDN never keeps a short object.
func TestOriginReadFailureCutsConnection(t *testing.T) {
	for _, mode := range []string{"error", "short"} {
		t.Run(mode, func(t *testing.T) {
			e := newOriginEnv(t, testSettings(t))
			f := e.file(docKey(1), refFile())
			if mode == "error" {
				e.reader.failAt = 2 << 20
			} else {
				e.reader.shortAt = 2 << 20
			}
			req, _ := http.NewRequest(http.MethodGet, e.srv.URL+ObjectPath(f.Secrets.Token), nil)
			req.Header.Set(OriginAuthHeader, e.auth)
			res, err := e.srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			n, err := io.Copy(io.Discard, res.Body)
			if err == nil || n >= refSize {
				t.Errorf("read %d bytes of %d, error %v: want the connection cut", n, refSize, err)
			}
			if n > 2<<20 {
				t.Errorf("read %d bytes, more than the 2 MiB before the failed part", n)
			}
		})
	}
}

// TestOriginClientGone checks that a read ended by a CDN that dropped its
// request is not taken for a failed read of dfs (code "aborted", an error
// in the log).
func TestOriginClientGone(t *testing.T) {
	e := newOriginEnv(t, testSettings(t))
	f := e.file(docKey(1), refFile())
	e.reader.block = make(chan struct{}) // never opened: a read ends with its request
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, ObjectPath(f.Secrets.Token), nil)
	req.Header.Set(OriginAuthHeader, e.auth)
	if _, err := e.origin.serve(httptest.NewRecorder(), req); err != nil {
		t.Errorf("a request the CDN dropped: %v, want no failed read", err)
	}
	if e.reader.readCount() != 0 {
		t.Errorf("%d reads of dfs completed", e.reader.readCount())
	}
}

// pathHMACSettings are settings with one path-hmac delivery.
func pathHMACSettings(t *testing.T) *Settings {
	t.Helper()
	c := testConfig(t)
	c.Deliveries[0].Signer = SignerPathHMAC
	c.Deliveries[0].Key = base64.StdEncoding.EncodeToString(randomBytes(t, PathHMACKeySize))
	c.UrlTTL = "6h"
	s, err := c.Parse()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOriginPathHMAC(t *testing.T) {
	set := pathHMACSettings(t)
	e := newOriginEnv(t, set)
	data := refFile()
	f := e.file(docKey(1), data)
	full := cipherOf(t, f, data)
	signer := set.Deliveries[0].Signer
	signedURL, bucket, err := signer.SignURL(ObjectPath(f.Secrets.Token), e.clock.now())
	if err != nil {
		t.Fatal(err)
	}
	path := strings.TrimPrefix(signedURL, set.Deliveries[0].BaseURL)

	res, body := e.do(http.MethodGet, path, map[string]string{"Range": "bytes=1000-2000"})
	if res.StatusCode != http.StatusPartialContent || !bytes.Equal(body, full[1000:2001]) {
		t.Fatalf("signed path: %d", res.StatusCode)
	}
	if got, want := res.Header.Get("Cache-Control"), "public, max-age="+strconv.FormatInt(bucket-e.clock.now().Unix(), 10); got != want {
		t.Errorf("Cache-Control %q, want %q", got, want)
	}
	if got := e.usage.settled(testDc, 1001); got != 1001 {
		t.Errorf("counted %d bytes for the delivery", got)
	}

	canaryURL, _, _ := signer.SignURL(CanaryPath, e.clock.now())
	if res, body := e.do(http.MethodGet, strings.TrimPrefix(canaryURL, set.Deliveries[0].BaseURL), nil); res.StatusCode != http.StatusOK || !bytes.Equal(body, canaryBytes) {
		t.Errorf("signed canary: %d", res.StatusCode)
	}

	// The CDN does not check the signature: /o/ without one is refused.
	if res, _ := e.do(http.MethodGet, ObjectPath(f.Secrets.Token), nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("unsigned /o/: %d, want 404", res.StatusCode)
	}
	broken, err := breakSignature(SignerPathHMAC, signedURL)
	if err != nil {
		t.Fatal(err)
	}
	if res, _ := e.do(http.MethodGet, strings.TrimPrefix(broken, set.Deliveries[0].BaseURL), nil); res.StatusCode != http.StatusForbidden {
		t.Errorf("broken signature: %d, want 403", res.StatusCode)
	}
	e.clock.add(time.Duration(bucket-e.clock.now().Unix()+1) * time.Second)
	if res, _ := e.do(http.MethodGet, path, nil); res.StatusCode != http.StatusGone {
		t.Errorf("expired: %d, want 410", res.StatusCode)
	}
	// A bucket more than 2·ttl ahead, signed with the key, is refused.
	far, _, _ := signer.SignURL(ObjectPath(f.Secrets.Token), e.clock.now().Add(3*set.URLTTL))
	if res, _ := e.do(http.MethodGet, strings.TrimPrefix(far, set.Deliveries[0].BaseURL), nil); res.StatusCode != http.StatusForbidden {
		t.Errorf("a bucket too far ahead: %d, want 403", res.StatusCode)
	}
}

// TestOriginTwoDeliveries: delivery 201 of Gcore and delivery 202 of the
// Russian node pull the same /o/<hex> with their own values of the header
// (task 109, "Выдача 202"); the origin serves both and counts the bytes for
// the delivery whose value came, the canary too. A value of neither is
// refused as before.
func TestOriginTwoDeliveries(t *testing.T) {
	c := testConfig(t)
	c.Deliveries = append(c.Deliveries, ruDelivery(t))
	c.Routes = "RU:202,201;*:201"
	set, err := c.Parse()
	if err != nil {
		t.Fatal(err)
	}
	e := newOriginEnv(t, set)
	data := refFile()
	f := e.file(docKey(1), data)
	full := cipherOf(t, f, data)
	ru := c.Deliveries[1].OriginAuth

	res, body := e.do(http.MethodGet, ObjectPath(f.Secrets.Token), map[string]string{"Range": "bytes=0-999"})
	if res.StatusCode != http.StatusPartialContent || !bytes.Equal(body, full[:1000]) {
		t.Fatalf("201: %d", res.StatusCode)
	}
	res, body = e.do(http.MethodGet, ObjectPath(f.Secrets.Token), map[string]string{OriginAuthHeader: ru, "Range": "bytes=1000-2999"})
	if res.StatusCode != http.StatusPartialContent || !bytes.Equal(body, full[1000:3000]) {
		t.Fatalf("202: %d", res.StatusCode)
	}
	if got := e.usage.settled(201, 1000); got != 1000 {
		t.Errorf("counted %d bytes for 201, want 1000", got)
	}
	if got := e.usage.settled(202, 2000); got != 2000 {
		t.Errorf("counted %d bytes for 202, want 2000", got)
	}
	if res, _ = e.do(http.MethodGet, CanaryPath, map[string]string{OriginAuthHeader: ru, "Range": "bytes=0-99"}); res.StatusCode != http.StatusPartialContent {
		t.Fatalf("canary of 202: %d", res.StatusCode)
	}
	if got := e.usage.settled(202, 2100); got != 2100 {
		t.Errorf("the canary of 202 counted for it: %d bytes, want 2100", got)
	}
	if got := e.usage.of(201); got != 1000 {
		t.Errorf("the bytes of 202 counted for 201: %d", got)
	}
	for _, auth := range []string{ru[:originAuthDigits-1], ru + "0", strings.ToUpper(ru), ""} {
		if auth == ru {
			continue
		}
		if res, _ := e.do(http.MethodGet, ObjectPath(f.Secrets.Token), map[string]string{OriginAuthHeader: auth}); res.StatusCode != http.StatusForbidden {
			t.Errorf("auth %q: %d, want 403", auth, res.StatusCode)
		}
	}

	// Settings that Parse would refuse do not make an origin either.
	bad := *set
	bad.Deliveries = slices.Clone(set.Deliveries)
	bad.Deliveries[1].OriginAuth = bad.Deliveries[0].OriginAuth
	if _, err := NewOrigin(e.store, &bad, e.reader, e.usage); err == nil {
		t.Error("an origin of two gcore-md5 deliveries with one value")
	}
}
