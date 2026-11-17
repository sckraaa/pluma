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
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
)

// What a patched client does with a redirect to the HTTPS CDN (task 107,
// ADR 0071 §8, the client contract of docs/tasks/107-https-cdn.md), written
// apart from pkg/cdn so that the tests check the server against what the
// clients accept rather than against itself:
//
//  1. initConnection carries params {"pluma_cdn": 1} (PlumaCdnParams);
//  2. upload.fileCdnRedirect with a file_token of "PLH1" | id[32] |
//     exp:uint32be | url is downloaded from url (ParseHttpsRedirect);
//  3. GET url with one Range, no cache, no redirects, Accept-Encoding:
//     identity; only a 206 with the exact Content-Range of the range and
//     the size of the file is taken, and a network failure, a timeout or a
//     5xx is retried once after 300-800 ms (HttpsFetcher);
//  4. the cipher is decrypted by the IV rule of the MTProto CDN and every
//     128 KB piece checked against its SHA-256 before a byte is used;
//     missing hashes come from upload.getCdnFileHashes at the home
//     (HttpsDownload);
//  5. 403 or 410 asks for a new URL (upload.getFile with cdn_supported), at
//     most MaxCdnURLRefreshes times; anything else makes the client fetch
//     the rest from home (upload.getFile without cdn_supported).
//
// No URL, file token, key or hash is put into an error or a log line.

// CdnHashChunk is the piece of a file one hash covers: 128 KB of the
// decrypted file at an offset divisible by it (Android cdnChunkCheckSize
// and its check, FileLoadOperation.java:2016-2048; tdesktop checks each
// 128 KB part, download_manager_mtproto.cpp:700-716).
const CdnHashChunk = 128 << 10

const (
	// PlumaCdnParam is the key of the params of initConnection by which a
	// patched client asks for redirects to the HTTPS CDN (contract p. 1).
	PlumaCdnParam = "pluma_cdn"
	// MaxCdnURLRefreshes is how many new URLs one download asks for after
	// a 403 or 410 (contract p. 6).
	MaxCdnURLRefreshes = 3

	httpsTokenMagic  = "PLH1"
	httpsTokenIDSize = 32
	httpsTokenHead   = len(httpsTokenMagic) + httpsTokenIDSize + 4

	// The timeouts of contract p. 4: the connection, a silence while the
	// answer is read, the whole request.
	cdnConnectTimeout = 5 * time.Second
	cdnSilenceTimeout = 10 * time.Second
	cdnRequestTimeout = 30 * time.Second
	// The pause before the one retry of a range, from cdnRetryMin to
	// cdnRetryMax.
	cdnRetryMin = 300 * time.Millisecond
	cdnRetryMax = 800 * time.Millisecond
	// cdnUserAgent is the constant User-Agent of the native clients
	// (contract p. 10): no model, no system version.
	cdnUserAgent = "Pluma"
)

// PlumaCdnParams returns the params of initConnection of a patched client:
// {"pluma_cdn": 1}, a jsonNumber.
func PlumaCdnParams() *mtproto.JSONValue {
	return mtproto.MakeTLJsonObject(&mtproto.JSONValue{Value_VECTORJSONOBJECTVALUE: []*mtproto.JSONObjectValue{
		mtproto.MakeTLJsonObjectValue(&mtproto.JSONObjectValue{
			Key:   PlumaCdnParam,
			Value: mtproto.MakeTLJsonNumber(&mtproto.JSONValue{Value_FLOAT64: 1}).To_JSONValue(),
		}).To_JSONObjectValue(),
	}}).To_JSONValue()
}

// HttpsRedirect is upload.fileCdnRedirect to the HTTPS CDN as a patched
// client takes it.
type HttpsRedirect struct {
	// DC is dc_id, the delivery; the client never connects to it.
	DC int32
	// FileToken is the whole file_token, which upload.getCdnFileHashes
	// takes back.
	FileToken []byte
	// ID is the id of the file in the token.
	ID []byte
	// Expires is when the URL stops working.
	Expires time.Time
	// URL is where the cipher of the file is.
	URL string
	// Key and IV decrypt it: AES-256-CTR, 32 and 16 bytes.
	Key, IV []byte
	// Hashes are those the redirect carries.
	Hashes []*mtproto.FileHash
}

// ParseHttpsRedirect checks a redirect as the patched clients do (contract
// p. 3): a key of 32 bytes and an IV of 16, or Android fails the download
// (FileLoadOperation.java:2602-2611) and tdesktop cancels it
// (download_manager_mtproto.cpp:1012-1020); a dc_id other than 0, which
// tdesktop takes for "not a CDN" (download_manager_mtproto.cpp:517-519);
// and a PLH1 token whose URL is https://, or http:// with allowHTTP, as
// development builds accept it for the stand (contract p. 11).
func ParseHttpsRedirect(f *mtproto.Upload_File, allowHTTP bool) (*HttpsRedirect, error) {
	if f.GetPredicateName() != mtproto.Predicate_upload_fileCdnRedirect {
		return nil, fmt.Errorf("mtclient: %s is not upload.fileCdnRedirect", f.GetPredicateName())
	}
	if len(f.GetEncryptionKey()) != 32 || len(f.GetEncryptionIv()) != 16 {
		return nil, fmt.Errorf("mtclient: a CDN redirect with a key of %d bytes and an IV of %d", len(f.GetEncryptionKey()), len(f.GetEncryptionIv()))
	}
	if f.GetDcId() == 0 {
		return nil, errors.New("mtclient: a CDN redirect without a dc_id")
	}
	id, exp, rawURL, err := ParseHttpsToken(f.GetFileToken(), allowHTTP)
	if err != nil {
		return nil, err
	}
	return &HttpsRedirect{
		DC:        f.GetDcId(),
		FileToken: slices.Clone(f.GetFileToken()),
		ID:        id,
		Expires:   time.Unix(int64(exp), 0),
		URL:       rawURL,
		Key:       slices.Clone(f.GetEncryptionKey()),
		IV:        slices.Clone(f.GetEncryptionIv()),
		Hashes:    f.GetFileHashes(),
	}, nil
}

// ParseHttpsToken splits a PLH1 file_token: "PLH1" | id[32] | exp:uint32be
// | url, the URL valid UTF-8 with a host and the scheme https (or http with
// allowHTTP). Any other token is an error: the client then downloads the
// file from home without cdn_supported (contract p. 3).
func ParseHttpsToken(token []byte, allowHTTP bool) (id []byte, exp uint32, rawURL string, err error) {
	if len(token) <= httpsTokenHead || !bytes.HasPrefix(token, []byte(httpsTokenMagic)) {
		return nil, 0, "", errors.New("mtclient: the file_token is not a PLH1 token")
	}
	id = slices.Clone(token[len(httpsTokenMagic) : len(httpsTokenMagic)+httpsTokenIDSize])
	exp = binary.BigEndian.Uint32(token[len(httpsTokenMagic)+httpsTokenIDSize:])
	rawURL = string(token[httpsTokenHead:])
	if !utf8.ValidString(rawURL) {
		return nil, 0, "", errors.New("mtclient: the URL of a PLH1 token is not UTF-8")
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || u.User != nil {
		return nil, 0, "", errors.New("mtclient: the URL of a PLH1 token does not parse")
	}
	if !strings.HasPrefix(rawURL, "https://") && (!allowHTTP || !strings.HasPrefix(rawURL, "http://")) {
		return nil, 0, "", fmt.Errorf("mtclient: the URL of a PLH1 token has the scheme %q", u.Scheme)
	}
	return id, exp, rawURL, nil
}

// CdnFetchError is a range the CDN did not serve as the contract requires.
type CdnFetchError struct {
	// Status is the HTTP status of the answer; 0 when there was none.
	Status int
	// Reason says what was wrong: "status", "content-range", "length",
	// "network", "timeout".
	Reason string
}

func (e *CdnFetchError) Error() string {
	if e.Status == 0 {
		return "mtclient: the CDN did not answer: " + e.Reason
	}
	return fmt.Sprintf("mtclient: the CDN answered %d: %s", e.Status, e.Reason)
}

// Refresh reports whether the client asks for a new URL (403 or 410,
// contract p. 6) rather than fall back to the home.
func (e *CdnFetchError) Refresh() bool {
	return e.Status == http.StatusForbidden || e.Status == http.StatusGone
}

// Transport reports whether the failure counts toward the pause of the
// HTTPS CDN (contract p. 8): no answer, or a 5xx after the retry.
func (e *CdnFetchError) Transport() bool {
	return e.Status == 0 || e.Status >= 500
}

// retryable is a failure the range is asked again for (contract p. 4).
func (e *CdnFetchError) retryable() bool { return e.Transport() }

// NewCdnHTTPClient returns an HTTP client set as the patched clients set
// theirs (contract p. 4, 10): a connection within 5 s, no proxy of the
// environment, no cookies, no redirects followed, no compression. dial,
// if set, makes the connections (a test reaches a host of the stand by
// it).
func NewCdnHTTPClient(dial func(ctx context.Context, network, addr string) (net.Conn, error)) *http.Client {
	if dial == nil {
		dial = (&net.Dialer{Timeout: cdnConnectTimeout}).DialContext
	}
	return &http.Client{
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				ctx, cancel := context.WithTimeout(ctx, cdnConnectTimeout)
				defer cancel()
				return dial(ctx, network, addr)
			},
			TLSHandshakeTimeout:   cdnConnectTimeout,
			ResponseHeaderTimeout: cdnSilenceTimeout,
			DisableCompression:    true,
			ForceAttemptHTTP2:     true,
			MaxIdleConnsPerHost:   4,
			IdleConnTimeout:       90 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// HttpsFetcher fetches ranges of a file's cipher from the CDN.
type HttpsFetcher struct {
	// HTTP makes the requests; nil: NewCdnHTTPClient(nil).
	HTTP *http.Client

	once sync.Once
}

func (f *HttpsFetcher) client() *http.Client {
	f.once.Do(func() {
		if f.HTTP == nil {
			f.HTTP = NewCdnHTTPClient(nil)
		}
	})
	return f.HTTP
}

// FetchRange returns bytes [offset, offset+limit) of the cipher at rawURL,
// cut at total, the size of the file the client knows. Only a 206 with
// Content-Range "bytes offset-end/total" and a body of exactly that length
// is taken (contract p. 4); the other answers are a *CdnFetchError. A
// network failure, a timeout or a 5xx is retried once after 300-800 ms.
func (f *HttpsFetcher) FetchRange(ctx context.Context, rawURL string, offset, limit, total int64) ([]byte, error) {
	if offset < 0 || limit <= 0 || offset >= total {
		return nil, fmt.Errorf("mtclient: a range of %d bytes at %d of a file of %d", limit, offset, total)
	}
	end := min(offset+limit, total) - 1
	b, err := f.fetch(ctx, rawURL, offset, end, total)
	var fe *CdnFetchError
	if err == nil || !errors.As(err, &fe) || !fe.retryable() {
		return b, err
	}
	pause := cdnRetryMin + rand.N(cdnRetryMax-cdnRetryMin+1) //nolint:gosec // G404: retry jitter, not a secret
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(pause):
	}
	return f.fetch(ctx, rawURL, offset, end, total)
}

func (f *HttpsFetcher) fetch(ctx context.Context, rawURL string, offset, end, total int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, cdnRequestTimeout)
	defer cancel()
	// silent cancels the request when the answer stops coming for
	// cdnSilenceTimeout; every read pushes it back.
	silent := time.AfterFunc(cdnSilenceTimeout, cancel)
	defer silent.Stop()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, errors.New("mtclient: the URL of the CDN does not parse")
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, end))
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("User-Agent", cdnUserAgent)
	res, err := f.client().Do(req)
	if err != nil {
		return nil, transportFailure(err)
	}
	defer res.Body.Close()
	silent.Reset(cdnSilenceTimeout)
	if res.StatusCode != http.StatusPartialContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
		return nil, &CdnFetchError{Status: res.StatusCode, Reason: "status"}
	}
	if res.Header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", offset, end, total) {
		return nil, &CdnFetchError{Status: res.StatusCode, Reason: "content-range"}
	}
	want := end - offset + 1
	if cl := res.Header.Get("Content-Length"); cl != "" && cl != strconv.FormatInt(want, 10) {
		return nil, &CdnFetchError{Status: res.StatusCode, Reason: "length"}
	}
	body := make([]byte, 0, want)
	buf := make([]byte, 32<<10)
	for {
		n, err := res.Body.Read(buf)
		if n > 0 {
			silent.Reset(cdnSilenceTimeout)
			if int64(len(body)+n) > want {
				return nil, &CdnFetchError{Status: res.StatusCode, Reason: "length"}
			}
			body = append(body, buf[:n]...)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, transportFailure(err)
		}
	}
	if int64(len(body)) != want {
		return nil, &CdnFetchError{Status: res.StatusCode, Reason: "length"}
	}
	return body, nil
}

// transportFailure names a request that got no answer, without the URL
// net/http puts into its errors.
func transportFailure(err error) *CdnFetchError {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || (errors.As(err, &ne) && ne.Timeout()) {
		return &CdnFetchError{Reason: "timeout"}
	}
	return &CdnFetchError{Reason: "network"}
}

// ErrCdnHashMismatch is a piece of the file whose SHA-256 is not the
// home's: the client stops using the CDN for the download (contract p. 7).
var ErrCdnHashMismatch = errors.New("mtclient: a piece from the CDN does not match its hash")

// CdnDecrypt decrypts the cipher at offset, a multiple of 16: AES-256-CTR
// with the IV whose last 4 bytes are offset/16, big-endian (Android
// FileLoadOperation.java:1900-1906; tdesktop
// download_manager_mtproto.cpp:693-701; the same rule as the MTProto CDN).
func CdnDecrypt(key, iv []byte, offset int64, data []byte) ([]byte, error) {
	if len(key) != 32 || len(iv) != 16 {
		return nil, errors.New("mtclient: CdnDecrypt needs a key of 32 bytes and an IV of 16")
	}
	if offset < 0 || offset%16 != 0 || offset/16 > 1<<32-1 {
		return nil, fmt.Errorf("mtclient: CdnDecrypt at offset %d", offset)
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	ctr := slices.Clone(iv)
	binary.BigEndian.PutUint32(ctr[12:], uint32(offset/16))
	out := make([]byte, len(data))
	cipher.NewCTR(b, ctr).XORKeyStream(out, data)
	return out, nil
}

// CdnHashes are the hashes of a file the client has, by offset.
type CdnHashes struct {
	byOffset map[int64]*mtproto.FileHash
}

// NewCdnHashes returns the hashes of list.
func NewCdnHashes(list []*mtproto.FileHash) *CdnHashes {
	h := &CdnHashes{byOffset: map[int64]*mtproto.FileHash{}}
	h.Add(list)
	return h
}

// Add keeps the hashes of list, as the clients keep every hash they are
// given.
func (h *CdnHashes) Add(list []*mtproto.FileHash) {
	for _, fh := range list {
		h.byOffset[FileHashOffset(fh)] = fh
	}
}

// Len is the number of hashes kept.
func (h *CdnHashes) Len() int { return len(h.byOffset) }

// FileHashOffset is the offset of fh, whichever layer's constructor
// carries it.
func FileHashOffset(fh *mtproto.FileHash) int64 {
	if fh.GetOffset_INT64() == 0 {
		return int64(fh.GetOffset_INT32())
	}
	return fh.GetOffset_INT64()
}

// Missing returns the offset of the first piece of [offset, offset+n)
// without a hash, and false when every piece has one.
func (h *CdnHashes) Missing(offset int64, n int) (int64, bool) {
	for p := offset / CdnHashChunk * CdnHashChunk; p < offset+int64(n); p += CdnHashChunk {
		if h.byOffset[p] == nil {
			return p, true
		}
	}
	return 0, false
}

// Check checks the decrypted bytes plain of the file at offset, divisible
// by CdnHashChunk: each piece of CdnHashChunk bytes, and a shorter last
// one, must have a hash of its length and its SHA-256. A mismatch wraps
// ErrCdnHashMismatch.
func (h *CdnHashes) Check(offset int64, plain []byte) error {
	if offset%CdnHashChunk != 0 {
		return fmt.Errorf("mtclient: the hashes are checked from a multiple of %d, not %d", CdnHashChunk, offset)
	}
	for start := 0; start < len(plain); start += CdnHashChunk {
		piece := plain[start:min(start+CdnHashChunk, len(plain))]
		at := offset + int64(start)
		fh := h.byOffset[at]
		if fh == nil {
			return fmt.Errorf("mtclient: no hash of the piece at %d", at)
		}
		sum := sha256.Sum256(piece)
		if int(fh.GetLimit()) != len(piece) || !bytes.Equal(fh.GetHash(), sum[:]) {
			return fmt.Errorf("%w: the piece at %d (%d bytes, hash of %d)", ErrCdnHashMismatch, at, len(piece), fh.GetLimit())
		}
	}
	return nil
}

// CdnFileHashes asks the home for the hashes of the file of token from
// offset (upload.getCdnFileHashes).
func (c *Client) CdnFileHashes(ctx context.Context, token []byte, offset int64) ([]*mtproto.FileHash, error) {
	raw, err := c.InvokeRaw(ctx, &mtproto.TLUploadGetCdnFileHashes{
		Constructor:  c.Constructor(mtproto.Predicate_upload_getCdnFileHashes),
		FileToken:    token,
		Offset_INT64: offset,
	})
	if err != nil {
		return nil, err
	}
	objs, err := DecodeVector(raw)
	if err != nil {
		return nil, err
	}
	out := make([]*mtproto.FileHash, 0, len(objs))
	for _, o := range objs {
		h, ok := o.(interface{ To_FileHash() *mtproto.FileHash })
		if !ok {
			return nil, fmt.Errorf("mtclient: upload.getCdnFileHashes returned a %T in the vector", o)
		}
		out = append(out, h.To_FileHash())
	}
	return out, nil
}

// HttpsDownload downloads one file through the HTTPS CDN as a patched
// client does: by ranges of whole 128 KB pieces, each decrypted and
// checked before it is returned.
type HttpsDownload struct {
	home    *Client
	fetcher *HttpsFetcher
	r       *HttpsRedirect
	size    int64
	hashes  *CdnHashes
}

// NewHttpsDownload starts the download of a file of size bytes from the
// redirect r; home answers upload.getCdnFileHashes.
func NewHttpsDownload(home *Client, fetcher *HttpsFetcher, r *HttpsRedirect, size int64) *HttpsDownload {
	return &HttpsDownload{home: home, fetcher: fetcher, r: r, size: size, hashes: NewCdnHashes(r.Hashes)}
}

// Refreshed takes the redirect a new upload.getFile gave after a 403 or
// 410; the hashes kept so far stay.
func (d *HttpsDownload) Refreshed(r *HttpsRedirect) {
	d.r = r
	d.hashes.Add(r.Hashes)
}

// Read returns the decrypted, checked bytes [offset, offset+limit) of the
// file, cut at its end; offset and limit are multiples of CdnHashChunk
// (contract p. 5). It returns a *CdnFetchError for an answer the CDN
// should not have given and an error wrapping ErrCdnHashMismatch for a
// piece that is not the file's; no byte of the range is returned then.
func (d *HttpsDownload) Read(ctx context.Context, offset int64, limit int) ([]byte, error) {
	if offset%CdnHashChunk != 0 || limit <= 0 || limit%CdnHashChunk != 0 {
		return nil, fmt.Errorf("mtclient: a range of %d bytes at %d is not of whole pieces", limit, offset)
	}
	data, err := d.fetcher.FetchRange(ctx, d.r.URL, offset, int64(limit), d.size)
	if err != nil {
		return nil, err
	}
	plain, err := CdnDecrypt(d.r.Key, d.r.IV, offset, data)
	if err != nil {
		return nil, err
	}
	for {
		at, missing := d.hashes.Missing(offset, len(plain))
		if !missing {
			break
		}
		more, err := d.home.CdnFileHashes(ctx, d.r.FileToken, at)
		if err != nil {
			return nil, fmt.Errorf("mtclient: upload.getCdnFileHashes at %d: %w", at, err)
		}
		if len(more) == 0 || FileHashOffset(more[0]) != at {
			return nil, fmt.Errorf("mtclient: upload.getCdnFileHashes at %d gave %d hashes from elsewhere", at, len(more))
		}
		d.hashes.Add(more)
	}
	if err = d.hashes.Check(offset, plain); err != nil {
		return nil, err
	}
	return plain, nil
}

// All downloads the whole file by ranges of part bytes.
func (d *HttpsDownload) All(ctx context.Context, part int) ([]byte, error) {
	out := make([]byte, 0, d.size)
	for offset := int64(0); offset < d.size; offset += int64(part) {
		b, err := d.Read(ctx, offset, part)
		if err != nil {
			return nil, err
		}
		out = append(out, b...)
	}
	return out, nil
}
