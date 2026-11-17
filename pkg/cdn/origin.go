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
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/zeromicro/go-zero/core/logx"
)

// OriginAuthHeader is the header the CDN adds to every request to the
// origin (a request header of the CDN resource), with CDN_ORIGIN_AUTH as
// its value (task 107, "Источник").
const OriginAuthHeader = "X-Pluma-Origin-Auth"

// Limits of the origin (task 107, "Пределы").
const (
	// OriginReadHeaderTimeout and OriginWriteTimeout bound a request to
	// the origin (http.Server).
	OriginReadHeaderTimeout = 10 * time.Second
	OriginWriteTimeout      = 120 * time.Second
	// objectMaxAge is how long a CDN keeps an object of the gcore-md5 form:
	// its bytes never change (a file has one key for good).
	objectMaxAge = 30 * 24 * time.Hour
)

// The canary of the origin (CanaryPath): a fixed object of canarySize
// bytes, SHA-256(canarySeed ‖ uint32_be(i)) for i = 0, 1, ..., which the
// checks of the deliveries fetch a range of (health.go).
const (
	canarySize = 256 << 10
	canarySeed = "pluma-cdn-canary-v1"
	// canaryETag names the one version of the canary.
	canaryETag = `"canary-v1"`
)

// canaryBytes is the canary.
var canaryBytes = func() []byte {
	b := make([]byte, 0, canarySize)
	var i [4]byte
	for n := uint32(0); len(b) < canarySize; n++ {
		binary.BigEndian.PutUint32(i[:], n)
		sum := sha256.Sum256(append([]byte(canarySeed), i[:]...))
		b = append(b, sum[:]...)
	}
	return b
}()

// The paths of the origin. The query string is never looked at: a CDN may
// pass the md5 and expires of its own token on (not verified for Gcore).
var (
	objectPathRe = regexp.MustCompile(`^/o/[0-9a-f]{64}$`)
	signedPathRe = regexp.MustCompile(`^/t/[0-9]{1,10}/[A-Za-z0-9_-]{22}/(o/[0-9a-f]{64}|c/v1)$`)
)

// errOriginRead is a read of a file that failed mid-answer.
var errOriginRead = errors.New("cdn: origin read failed")

// UsageCounter counts the bytes the origin served for a delivery (Usage).
type UsageCounter interface {
	CountOrigin(dc int32, n int64)
}

// Origin is the home origin of the HTTPS CDN (ADR 0071 §3, task 107
// "Источник"): the CDNs pull the cipher of the prepared files from it,
// encrypted on the fly from the plaintext dfs keeps, by range. Nothing is
// written anywhere: a CDN keeps what it pulled.
//
// Every request must carry OriginAuthHeader with the value of the config,
// compared in constant time before the path is looked at, so that without
// it an object that exists is not told from one that does not. The paths:
//
//   - /o/<64 hex>: the cipher of the file whose cdn_files.file_token is the
//     hex, while a delivery whose CDN checks its own token (gcore-md5)
//     pulls from the origin;
//   - /t/<bucket>/<sig>/o/<64 hex>: the same, signed by a path-hmac
//     delivery, whose signature the origin checks itself (403, 410 for an
//     expired one);
//   - /c/v1 and /t/<bucket>/<sig>/c/v1: the canary.
//
// GET and HEAD answer by http.ServeContent: one range is 206 with
// Content-Range, none 200, several multipart/byteranges, one past the end
// 416; If-None-Match, If-Modified-Since and If-Range follow RFC 9110. The
// headers of an object never change, so a CDN that slices a file into
// ranges gets the same ETag and type with each.
type Origin struct {
	store  Store
	set    *Settings
	reader FileReader
	usage  UsageCounter
	now    func() time.Time
	auth   []byte
	// busy bounds the requests that read files (MaxConcurrent): past it,
	// 503.
	busy chan struct{}
	// plain is the delivery /o/ paths are counted for; nil when no
	// delivery's CDN checks its own token, and /o/ is refused.
	plain *Delivery
	// signed are the path-hmac deliveries.
	signed []signedDelivery
}

type signedDelivery struct {
	id     int32
	signer *PathHMACSigner
}

// NewOrigin returns the origin of the home-origin deliveries of set,
// reading files by reader and counting what it serves in usage.
func NewOrigin(store Store, set *Settings, reader FileReader, usage UsageCounter) (*Origin, error) {
	if len(set.Origin.AuthValue) != originAuthDigits || set.Origin.MaxConcurrent < 1 {
		return nil, errors.New("cdn: the origin has no auth value or concurrency")
	}
	o := &Origin{
		store:  store,
		set:    set,
		reader: reader,
		usage:  usage,
		now:    time.Now,
		auth:   []byte(set.Origin.AuthValue),
		busy:   make(chan struct{}, set.Origin.MaxConcurrent),
	}
	for i := range set.Deliveries {
		d := &set.Deliveries[i]
		if d.Source != SourceHomeOrigin {
			continue
		}
		if s, ok := d.Signer.(*PathHMACSigner); ok {
			o.signed = append(o.signed, signedDelivery{id: d.ID, signer: s})
			continue
		}
		if o.plain == nil {
			o.plain = d
		} else {
			// Both CDNs ask for /o/<hex> with the same header; the origin
			// cannot tell them apart and counts their bytes for the first.
			logx.Errorf("cdn: deliveries %d and %d both pull /o/ from the origin: its bytes are counted for %d", o.plain.ID, d.ID, o.plain.ID)
		}
	}
	if o.plain == nil && len(o.signed) == 0 {
		return nil, errors.New("cdn: no delivery pulls from the origin")
	}
	return o, nil
}

// ServeHTTP serves a request of a CDN. The log names the method, the
// status, the length and the time, never the path (it carries the file's
// id) or a header.
func (o *Origin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	metricOriginInflight.Inc()
	defer metricOriginInflight.Dec()

	cw := &countingWriter{ResponseWriter: w}
	dc, err := o.serve(cw, r)
	status := strconv.Itoa(cw.statusCode())
	if err != nil {
		status = "aborted"
	}
	metricOriginRequests.Inc(status)
	if cw.n > 0 {
		metricOriginBytes.Add(float64(cw.n))
		if dc != 0 {
			o.usage.CountOrigin(dc, cw.n)
		}
	}
	logx.WithContext(r.Context()).WithDuration(time.Since(start)).Infof("cdn origin: %s %s, %d bytes", r.Method, status, cw.n)
	if err != nil {
		logx.WithContext(r.Context()).Errorf("cdn origin: connection cut mid-answer: %v", err)
		// Cut the connection: a CDN must not take a short body for the
		// object (net/http closes it without logging the panic).
		panic(http.ErrAbortHandler)
	}
}

// serve answers r; it returns the delivery the bytes are counted for (0
// for none) and an error when the read of the file failed mid-answer.
func (o *Origin) serve(w http.ResponseWriter, r *http.Request) (int32, error) {
	h := w.Header()
	h.Set("Cache-Control", "no-store") // an answer that is not an object is never kept
	if subtle.ConstantTimeCompare([]byte(r.Header.Get(OriginAuthHeader)), o.auth) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return 0, nil
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return 0, nil
	}
	// A path with %-escapes is not one the signers make.
	path := r.URL.Path
	if r.URL.RawPath != "" {
		http.NotFound(w, r)
		return 0, nil
	}

	var dc int32
	cacheControl := "public, max-age=" + strconv.Itoa(int(objectMaxAge/time.Second)) + ", immutable"
	switch {
	case path == CanaryPath:
		dc = o.plainID()
	case objectPathRe.MatchString(path):
		if o.plain == nil {
			http.NotFound(w, r)
			return 0, nil
		}
		dc = o.plain.ID
	case len(o.signed) > 0 && signedPathRe.MatchString(path):
		var bucket int64
		var status int
		dc, path, bucket, status = o.verify(path)
		if status != 0 {
			http.Error(w, http.StatusText(status), status)
			return 0, nil
		}
		// The URL stops working at bucket: the CDN keeps the object as
		// long, not longer.
		cacheControl = "public, max-age=" + strconv.FormatInt(max(bucket-o.now().Unix(), 0), 10)
	default:
		http.NotFound(w, r)
		return 0, nil
	}

	h.Set("Content-Type", "application/octet-stream") // set, so that ServeContent does not sniff; http.Error sets its own
	if path == CanaryPath {
		h.Set("Cache-Control", cacheControl)
		h.Set("ETag", canaryETag)
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(canaryBytes))
		return dc, nil
	}

	select {
	case o.busy <- struct{}{}:
		defer func() { <-o.busy }()
	default:
		h.Set("Retry-After", "1")
		http.Error(w, "busy", http.StatusServiceUnavailable)
		return 0, nil
	}
	id, _ := hex.DecodeString(path[len("/o/"):]) // the pattern checked it
	f, err := o.store.ByToken(r.Context(), id)
	if err != nil {
		logx.WithContext(r.Context()).Errorf("cdn origin: find the file: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return 0, nil
	}
	// A file that is not prepared, or whose kind no longer goes through
	// the CDN (a secret chat file never does), is not there.
	if f == nil || f.Hashes == nil || !f.HasKey() || !o.set.AllowsKind(f.Key.Kind) || f.Size <= 0 || f.Size > MaxCipherSize {
		http.NotFound(w, r)
		return 0, nil
	}
	cr, err := newCipherReader(r.Context(), o.reader, f)
	if err != nil {
		logx.WithContext(r.Context()).Errorf("cdn origin: file row %d: %v", f.RowId, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return 0, nil
	}
	h.Set("Cache-Control", cacheControl)
	h.Set("ETag", `"`+hex.EncodeToString(id)+`"`)
	http.ServeContent(w, r, "", time.UnixMilli(f.CreatedAt), cr)
	if err = cr.failure(); err != nil {
		if r.Context().Err() != nil {
			// The CDN dropped the request, which ended the read: nothing
			// failed here, and net/http closes the connection of a short
			// body by itself.
			return dc, nil
		}
		return dc, fmt.Errorf("file row %d: %w", f.RowId, err)
	}
	return dc, nil
}

// plainID is the delivery the canary of /c/v1 is counted for.
func (o *Origin) plainID() int32 {
	if o.plain != nil {
		return o.plain.ID
	}
	return 0
}

// verify checks a signed path with the path-hmac deliveries: the delivery,
// the path of the object, the bucket, and 0; or the status of the refusal
// (410 for a URL a delivery made whose time passed, 403 for any other).
func (o *Origin) verify(path string) (int32, string, int64, int) {
	now := o.now()
	status := http.StatusForbidden
	for _, d := range o.signed {
		object, bucket, err := d.signer.Verify(path, now)
		switch {
		case err == nil:
			return d.id, object, bucket, 0
		case errors.Is(err, ErrURLExpired):
			status = http.StatusGone
		}
	}
	return 0, "", 0, status
}

// cipherReader is the cipher of a file as an io.ReadSeeker for
// http.ServeContent: it reads the plaintext from dfs a ReadChunkSize part
// at a time, at a multiple of it, and encrypts the part by the IV rule of
// the clients (DecryptAt) from its offset, so that any range, at any
// offset, is the same bytes as that range of the cipher of the whole file.
// One part is kept; a range is never buffered whole.
type cipherReader struct {
	ctx    context.Context
	reader FileReader
	loc    *mtproto.InputFileLocation
	key    []byte
	iv     []byte
	size   int64
	pos    int64

	part    []byte // the cipher of the part at partAt
	partAt  int64
	hasPart bool

	// ServeContent reads several ranges in a goroutine of its own, which
	// may outlive the handler.
	mu  sync.Mutex
	err error
}

func newCipherReader(ctx context.Context, reader FileReader, f *File) (*cipherReader, error) {
	loc := Location(f.Key)
	if loc == nil {
		return nil, fmt.Errorf("no location for kind %d", f.Key.Kind)
	}
	if !validKeyIV(f.Secrets.Key, f.Secrets.IV) {
		return nil, errSecrets
	}
	return &cipherReader{ctx: ctx, reader: reader, loc: loc, key: f.Secrets.Key, iv: f.Secrets.IV, size: f.Size}, nil
}

func (c *cipherReader) Read(p []byte) (int, error) {
	if c.pos >= c.size {
		return 0, io.EOF
	}
	at := c.pos / ReadChunkSize * ReadChunkSize
	if !c.hasPart || c.partAt != at {
		if err := c.load(at); err != nil {
			c.fail(err)
			return 0, err
		}
	}
	n := copy(p, c.part[c.pos-at:])
	c.pos += int64(n)
	return n, nil
}

// load reads and encrypts the part at at.
func (c *cipherReader) load(at int64) error {
	want := min(int64(ReadChunkSize), c.size-at)
	b, err := c.reader.ReadPart(c.ctx, c.loc, at, ReadChunkSize)
	if err != nil {
		return fmt.Errorf("%w: at %d: %w", errOriginRead, at, err)
	}
	// dfs answers a part it failed to read with no bytes (reader.go).
	if int64(len(b)) != want {
		return fmt.Errorf("%w: %d bytes at %d, want %d", errOriginRead, len(b), at, want)
	}
	if err = DecryptAt(c.key, c.iv, at, b); err != nil { // encryption is the same operation
		return err
	}
	c.part, c.partAt, c.hasPart = b, at, true
	return nil
}

func (c *cipherReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += c.pos
	case io.SeekEnd:
		offset += c.size
	default:
		return 0, errors.New("cdn: bad whence")
	}
	if offset < 0 {
		return 0, errors.New("cdn: negative position")
	}
	c.pos = offset
	return offset, nil
}

func (c *cipherReader) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil {
		c.err = err
	}
}

// failure is the first read that failed, nil if none did.
func (c *cipherReader) failure() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// countingWriter counts the bytes of the body and keeps the status.
type countingWriter struct {
	http.ResponseWriter
	status int
	n      int64
}

func (w *countingWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *countingWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(p)
	w.n += int64(n)
	return n, err
}

// statusCode is the status sent, 200 if none was set.
func (w *countingWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}
