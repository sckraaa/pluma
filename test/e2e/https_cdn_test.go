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

//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// The HTTPS CDN of large files (task 107, ADR 0071): bff redirects a file
// of 1 MiB and more to test/e2e/fakecdn, the stand's Gcore resource, once
// it holds the file's hashes, and only for a client that sent pluma_cdn in
// initConnection. The client downloads the cipher from the URL in the PLH1
// file_token over HTTP with Range, as the patched clients do
// (pkg/mtclient/cdnhttp.go), decrypts it and checks it against the hashes
// of the home. fakecdn pulls the cipher from bff's origin in slices of
// 10 MB and caches them; its control API breaks it for the tests.
//
// The tests that need fakecdn as it is, or switch it, run one at a time
// (no t.Parallel): a switch is global to the stand. Each resets fakecdn
// before and after itself.

const (
	// httpsCdnDC is the delivery of the stand (CDN_ROUTES "*:201").
	httpsCdnDC = 201
	// fakeCDNHost is fakecdn as bff names it in the URLs: the tests reach
	// it at its loopback port instead, as a hosts entry would.
	fakeCDNHost = "fakecdn:8080"
	// cdnRedirectWithin is how long a redirect of a new file may take: its
	// hashes are made after the first request (task 107, "Тесты").
	cdnRedirectWithin = 30 * time.Second
	// cdnCapsTTL is how long bff keeps the pluma_cdn of a key
	// (app/bff/files/internal/svc/cdn_caps.go, capsTTL).
	cdnCapsTTL = time.Minute
)

// cdnStand are the intervals of the stand's CDN, from run.sh.
type cdnStand struct {
	urlTTL, healthEvery, avoidTTL time.Duration
}

func httpsCdnStand(t *testing.T) cdnStand {
	t.Helper()
	var s cdnStand
	for _, v := range []struct {
		env string
		d   *time.Duration
	}{
		{"PLUMA_E2E_CDN_URL_TTL", &s.urlTTL},
		{"PLUMA_E2E_CDN_HEALTH_EVERY", &s.healthEvery},
		{"PLUMA_E2E_CDN_AVOID_TTL", &s.avoidTTL},
	} {
		d, err := time.ParseDuration(os.Getenv(v.env))
		if err != nil || d <= 0 {
			t.Fatalf("%s is not a duration (set by test/e2e/run.sh): %v", v.env, err)
		}
		*v.d = d
	}
	return s
}

// healthWithin is how long the redirects take to stop or resume after the
// CDN fails or comes back: a check every healthEvery, a good one valid
// three intervals, and the read of the checks cached for a moment in bff.
func (s cdnStand) healthWithin() time.Duration { return 3*s.healthEvery + 15*time.Second }

// fakeCDNPort is where this machine reaches port of fakecdn, read anew
// each time: a recreated container has another ephemeral port.
func fakeCDNPort(t *testing.T, port string) string {
	t.Helper()
	out, err := exec.Command(runScript, "compose", "port", "fakecdn", port).Output()
	addr := strings.TrimSpace(string(out))
	if err != nil || addr == "" {
		t.Fatalf("the port %s of fakecdn: %v", port, err)
	}
	return addr
}

// cdnFetcher is the HTTP side of a patched client on this machine.
func cdnFetcher(t *testing.T) *mtclient.HttpsFetcher {
	t.Helper()
	addr := fakeCDNPort(t, "8080")
	d := &net.Dialer{}
	return &mtclient.HttpsFetcher{HTTP: mtclient.NewCdnHTTPClient(func(ctx context.Context, network, a string) (net.Conn, error) {
		if a == fakeCDNHost {
			a = addr
		}
		return d.DialContext(ctx, network, a)
	})}
}

// fakeCDN sends a request to the control API of fakecdn.
func fakeCDN(t *testing.T, method, path string) []byte {
	t.Helper()
	req, err := http.NewRequest(method, "http://"+fakeCDNPort(t, "8081")+"/_ctl/"+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("fakecdn %s: %v", path, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode/100 != 2 {
		t.Fatalf("fakecdn %s: %d %s, %v", path, res.StatusCode, body, err)
	}
	return body
}

// fakeCDNSwitch turns a switch of fakecdn on or off (tamper, down,
// badrange, full200, notoken); extra is more of the query.
func fakeCDNSwitch(t *testing.T, name string, on bool, extra string) {
	t.Helper()
	q := "?on=0"
	if on {
		q = "?on=1"
	}
	fakeCDN(t, http.MethodPost, name+q+extra)
}

// isolateFakeCDN resets fakecdn (switches off, cache and counters empty)
// now and when the test ends.
func isolateFakeCDN(t *testing.T) {
	t.Helper()
	fakeCDN(t, http.MethodPost, "reset")
	t.Cleanup(func() { fakeCDN(t, http.MethodPost, "reset") })
}

// fakeCDNStats are the counters of fakecdn.
type fakeCDNStats struct {
	OriginRequests int64                      `json:"origin_requests"`
	OriginBytes    int64                      `json:"origin_bytes"`
	Slices         map[string]map[int64]int64 `json:"slices"`
}

func fakeCDNCounters(t *testing.T) fakeCDNStats {
	t.Helper()
	var s fakeCDNStats
	if err := json.Unmarshal(fakeCDN(t, http.MethodGet, "stats"), &s); err != nil {
		t.Fatalf("fakecdn stats: %v", err)
	}
	return s
}

// loginPatched logs in from a patched client: its initConnection carries
// {"pluma_cdn": 1}.
func loginPatched(ctx context.Context, t *testing.T, phone, firstName string) *user {
	t.Helper()
	cfg := config(t, "e2e "+firstName)
	cfg.Params = mtclient.PlumaCdnParams()
	return loginWith(ctx, t, phone, firstName, cfg)
}

func randomBlob(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

// uploadBigFile uploads data by parts of 512 KB as a big file.
func uploadBigFile(ctx context.Context, t *testing.T, u *user, data []byte, name string) *mtproto.InputFile {
	t.Helper()
	const part = 512 << 10
	id := randomID()
	parts := int32((len(data) + part - 1) / part)
	for n := int32(0); n < parts; n++ {
		if _, err := u.c.Invoke(ctx, &mtproto.TLUploadSaveBigFilePart{
			Constructor:    u.c.Constructor(mtproto.Predicate_upload_saveBigFilePart),
			FileId:         id,
			FilePart:       n,
			FileTotalParts: parts,
			Bytes:          data[int(n)*part : min(int(n+1)*part, len(data))],
		}); err != nil {
			t.Fatalf("upload.saveBigFilePart %d: %v", n, err)
		}
	}
	return mtproto.MakeTLInputFileBig(&mtproto.InputFile{Id_INT64: id, Parts: parts, Name: name}).To_InputFile()
}

// sendBigDocument sends data as a file to peer and returns the document as
// the sender has it.
func sendBigDocument(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, data []byte) *mtproto.Document {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendMedia{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_sendMedia),
		Peer:        peer,
		Media: mtproto.MakeTLInputMediaUploadedDocument(&mtproto.InputMedia{
			File:     uploadBigFile(ctx, t, u, data, "data.bin"),
			MimeType: "application/octet-stream",
			Attributes: []*mtproto.DocumentAttribute{
				mtproto.MakeTLDocumentAttributeFilename(&mtproto.DocumentAttribute{FileName: "data.bin"}).To_DocumentAttribute(),
			},
		}).To_InputMedia(),
		RandomId: randomID(),
	})
	if err != nil {
		t.Fatalf("messages.sendMedia: %v", err)
	}
	for _, m := range sentMessages(t, o) {
		if d := m.GetMedia().GetDocument(); d.GetId() != 0 {
			if d.GetSize2_INT64() != int64(len(data)) {
				t.Fatalf("a document of %d bytes, %d sent", d.GetSize2_INT64(), len(data))
			}
			return d
		}
	}
	t.Fatal("messages.sendMedia returned no document")
	return nil
}

// sendSelfDocument sends u a file of n random bytes in Saved Messages and
// returns the bytes and the location.
func sendSelfDocument(ctx context.Context, t *testing.T, u *user, n int) ([]byte, *mtproto.InputFileLocation) {
	t.Helper()
	data := randomBlob(t, n)
	self := &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerSelf)}
	return data, documentFileLocation(sendBigDocument(ctx, t, u, self, data))
}

// receiveDocument waits for the message with the document id in to.
func receiveDocument(ctx context.Context, t *testing.T, to *user, id int64) *mtproto.Document {
	t.Helper()
	var doc *mtproto.Document
	if _, err := to.c.WaitUpdate(ctx, func(u *mtproto.Updates) bool {
		for _, m := range newMessages(u) {
			if d := m.GetMedia().GetDocument(); d.GetId() == id {
				doc = d
				return true
			}
		}
		return false
	}); err != nil {
		t.Fatalf("the message with the file: %v", err)
	}
	return doc
}

func documentFileLocation(d *mtproto.Document) *mtproto.InputFileLocation {
	return mtproto.MakeTLInputDocumentFileLocation(&mtproto.InputFileLocation{
		Id: d.GetId(), AccessHash: d.GetAccessHash(), FileReference: d.GetFileReference(),
	}).To_InputFileLocation()
}

// getFilePart is upload.getFile; cdn sets cdn_supported.
func getFilePart(ctx context.Context, u *user, loc *mtproto.InputFileLocation, offset int64, limit int32, cdn bool) (*mtproto.Upload_File, error) {
	o, err := u.c.Invoke(ctx, &mtproto.TLUploadGetFile{
		Constructor:  u.c.Constructor(mtproto.Predicate_upload_getFile),
		CdnSupported: cdn,
		Location:     loc,
		Offset_INT64: offset,
		Limit:        limit,
	})
	if err != nil {
		return nil, err
	}
	f, ok := o.(interface{ To_Upload_File() *mtproto.Upload_File })
	if !ok {
		return nil, fmt.Errorf("upload.getFile returned %T", o)
	}
	return f.To_Upload_File(), nil
}

// downloadFromHome reads a file of size bytes by 1 MB without
// cdn_supported, as a patched client falls back to the home. A file still
// being stored is empty for a moment, so an empty first part is asked
// again.
func downloadFromHome(ctx context.Context, t *testing.T, u *user, loc *mtproto.InputFileLocation, size int) []byte {
	t.Helper()
	var out []byte
	for off := 0; off < size; {
		f, err := getFilePart(ctx, u, loc, int64(off), 1<<20, false)
		if err != nil {
			t.Fatalf("upload.getFile at %d: %v", off, err)
		}
		if f.GetPredicateName() != mtproto.Predicate_upload_file {
			t.Fatalf("upload.getFile without cdn_supported: %s", f.GetPredicateName())
		}
		if len(f.GetBytes()) == 0 {
			if off != 0 {
				t.Fatalf("upload.getFile: the file ends at %d of %d", off, size)
			}
			sleepCtx(ctx, t, 100*time.Millisecond)
			continue
		}
		out = append(out, f.GetBytes()...)
		off += len(f.GetBytes())
	}
	return out
}

func sleepCtx(ctx context.Context, t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-time.After(d):
	}
}

// waitCdnAnswer asks upload.getFile with cdn_supported at offset every
// second until the answer is a redirect (redirect) or the file itself
// (!redirect), for within.
func waitCdnAnswer(ctx context.Context, t *testing.T, u *user, loc *mtproto.InputFileLocation, offset int64, redirect bool, within time.Duration) *mtproto.Upload_File {
	t.Helper()
	deadline := time.Now().Add(within)
	// A part within one megabyte, as the clients ask: 1 MB at a megabyte,
	// 128 KB elsewhere (the rules of upload.getFile).
	limit := int32(1 << 20)
	if offset%int64(limit) != 0 {
		limit = 128 << 10
	}
	for {
		f, err := getFilePart(ctx, u, loc, offset, limit, true)
		if err != nil {
			t.Fatalf("upload.getFile with cdn_supported: %v", err)
		}
		if got := f.GetPredicateName() == mtproto.Predicate_upload_fileCdnRedirect; got == redirect {
			if !redirect && len(f.GetBytes()) == 0 {
				t.Fatal("upload.getFile with cdn_supported: an empty file")
			}
			return f
		}
		if time.Now().After(deadline) {
			t.Fatalf("upload.getFile with cdn_supported still answers %s after %v", f.GetPredicateName(), within)
		}
		sleepCtx(ctx, t, time.Second)
	}
}

// httpsRedirect waits for the redirect of loc at offset and checks it as a
// patched client does: delivery 201, a PLH1 token with the URL of the
// file's id, and the hashes from the piece of offset on.
func httpsRedirect(ctx context.Context, t *testing.T, s cdnStand, u *user, loc *mtproto.InputFileLocation, offset int64) *mtclient.HttpsRedirect {
	t.Helper()
	return httpsRedirectWithin(ctx, t, s, u, loc, offset, max(cdnRedirectWithin, s.healthWithin()))
}

// httpsRedirectWithin is httpsRedirect waiting for within.
func httpsRedirectWithin(ctx context.Context, t *testing.T, s cdnStand, u *user, loc *mtproto.InputFileLocation, offset int64, within time.Duration) *mtclient.HttpsRedirect {
	t.Helper()
	f := waitCdnAnswer(ctx, t, u, loc, offset, true, within)
	r, err := mtclient.ParseHttpsRedirect(f, true)
	if err != nil {
		t.Fatal(err)
	}
	if r.DC != httpsCdnDC {
		t.Fatalf("redirected to %d, want %d", r.DC, httpsCdnDC)
	}
	if r.IV[12] != 0 || r.IV[13] != 0 || r.IV[14] != 0 || r.IV[15] != 0 {
		t.Error("the last 4 bytes of the IV of a redirect are not zero")
	}
	u2, err := url.Parse(r.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u2.Query()
	if u2.Path != "/o/"+hex.EncodeToString(r.ID) || q.Get("md5") == "" || q.Get("expires") != strconv.FormatInt(r.Expires.Unix(), 10) {
		t.Errorf("the URL of the redirect has the path of another file or no Secure Token")
	}
	if left := time.Until(r.Expires); left <= 0 || left > s.urlTTL+5*time.Second {
		t.Errorf("the URL of the redirect expires in %v, CDN_URL_TTL is %v", left, s.urlTTL)
	}
	if len(r.Hashes) == 0 || mtclient.FileHashOffset(r.Hashes[0]) != offset/mtclient.CdnHashChunk*mtclient.CdnHashChunk {
		t.Fatalf("the hashes of a redirect at %d start elsewhere (%d of them)", offset, len(r.Hashes))
	}
	return r
}

// stackSQL runs one query in the stand's MySQL and returns its output
// without the column names.
func stackSQL(t *testing.T, query string) string {
	t.Helper()
	return strings.TrimSpace(stackOut(t, "exec", "-T", "mysql", "sh", "-c",
		`MYSQL_PWD="$MYSQL_PASSWORD" mysql -N -B -u "$MYSQL_USER" "$MYSQL_DATABASE" -e "$0"`, query))
}

// notInLogs fails the test if the logs of bff or fakecdn hold one of the
// strings, or of the byte strings in hex, base64 or as Go prints a byte
// slice.
func notInLogs(t *testing.T, texts map[string]string, secrets map[string][]byte) {
	t.Helper()
	logs := stackOut(t, "logs", "--no-color", "bff", "fakecdn")
	for name, s := range texts {
		if s != "" && strings.Contains(logs, s) {
			t.Errorf("the logs of bff or fakecdn hold the %s", name)
		}
	}
	for name, secret := range secrets {
		for _, form := range []string{
			hex.EncodeToString(secret), strings.ToUpper(hex.EncodeToString(secret)),
			base64.RawStdEncoding.EncodeToString(secret), base64.RawURLEncoding.EncodeToString(secret),
			strings.Trim(fmt.Sprintf("%d", secret), "[]"),
		} {
			if strings.Contains(logs, form) {
				t.Errorf("the logs of bff or fakecdn hold the %s", name)
			}
		}
	}
}

// TestHttpsCdnDownload: Alice sends Bob a file of 3 MB and 5 bytes. Bob's
// patched client asks for it with cdn_supported: first the file itself,
// while the home makes its hashes, then within 30 s a redirect to delivery
// 201 with a PLH1 token. He downloads the cipher by Range from the URL, by
// 128 KB as tdesktop and Android do and by 512 KB as iOS does, decrypts it
// and checks every piece: the redirect at 1 MB carries the hashes from
// 1 MB on, those before come from upload.getCdnFileHashes. The result is
// the file sent, and no secret of it or of the CDN is in the logs.
func TestHttpsCdnDownload(t *testing.T) {
	s := httpsCdnStand(t)
	isolateFakeCDN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := loginPatched(ctx, t, phones[0], "Alice")
	bob := loginPatched(ctx, t, phones[1], "Bob")
	getState(ctx, t, bob)

	data := randomBlob(t, 3<<20+5)
	sent := sendBigDocument(ctx, t, alice, peerOf(ctx, t, alice, bob), data)
	loc := documentFileLocation(receiveDocument(ctx, t, bob, sent.GetId()))

	var first *mtproto.Upload_File
	for {
		f, err := getFilePart(ctx, bob, loc, 1<<20, 1<<20, true)
		if err != nil {
			t.Fatalf("the first upload.getFile with cdn_supported: %v", err)
		}
		if len(f.GetBytes()) > 0 || f.GetPredicateName() != mtproto.Predicate_upload_file {
			first = f
			break
		}
		sleepCtx(ctx, t, 100*time.Millisecond) // dfs is still storing the file
	}
	if first.GetPredicateName() != mtproto.Predicate_upload_file || !bytes.Equal(first.GetBytes(), data[1<<20:2<<20]) {
		t.Fatalf("the first request of a new file: %s, want the file from the home", first.GetPredicateName())
	}
	r := httpsRedirect(ctx, t, s, bob, loc, 1<<20)

	fetcher := cdnFetcher(t)
	for _, part := range []int{128 << 10, 512 << 10} {
		got, err := mtclient.NewHttpsDownload(bob.c, fetcher, r, int64(len(data))).All(ctx, part)
		if err != nil {
			t.Fatalf("by %d KB through the CDN: %v", part>>10, err)
		}
		if !bytes.Equal(got, data) {
			t.Fatalf("by %d KB through the CDN: %d bytes, not the file", part>>10, len(got))
		}
	}
	hashes, err := bob.c.CdnFileHashes(ctx, r.FileToken, 0)
	if err != nil || len(hashes) != 25 || mtclient.FileHashOffset(hashes[0]) != 0 {
		t.Errorf("upload.getCdnFileHashes from 0: %d hashes, %v", len(hashes), err)
	}

	// Alice, who sent it, is redirected to the same file.
	ra := httpsRedirect(ctx, t, s, alice, documentFileLocation(sent), 0)
	if !bytes.Equal(ra.ID, r.ID) || !bytes.Equal(ra.Key, r.Key) {
		t.Error("the sender was redirected to another copy of the file")
	}

	u, err := url.Parse(r.URL)
	if err != nil {
		t.Fatal(err)
	}
	originAuth := strings.TrimSpace(stackOut(t, "exec", "-T", "bff", "printenv", "CDN_ORIGIN_AUTH"))
	tokenKey := strings.TrimSpace(stackOut(t, "exec", "-T", "bff", "printenv", "CDN_GCORE_TOKEN_KEY"))
	if len(originAuth) != 64 || tokenKey == "" {
		t.Fatal("bff has no CDN_ORIGIN_AUTH or CDN_GCORE_TOKEN_KEY of the run")
	}
	notInLogs(t, map[string]string{
		"signed URL": r.URL, "URL signature": u.Query().Get("md5"), "path of the file": u.Path,
		"origin header": originAuth, "Secure Token key": tokenKey,
	}, map[string][]byte{"file_token": r.FileToken, "file id": r.ID, "key": r.Key, "IV": r.IV[:12]})
}

// TestHttpsCdnLargeSlices: a file of 25 MB lies in three slices of 10 MB
// of the CDN. Alice's client reads a range in each, Bob's then downloads
// the whole file through another URL of the same object: the origin
// served each slice once, as the cache of the resource is keyed by the
// path without the query (task 107, "Ignore query string").
func TestHttpsCdnLargeSlices(t *testing.T) {
	s := httpsCdnStand(t)
	isolateFakeCDN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := loginPatched(ctx, t, phones[0], "Alice")
	bob := loginPatched(ctx, t, phones[1], "Bob")
	getState(ctx, t, bob)

	data := randomBlob(t, 25<<20)
	sent := sendBigDocument(ctx, t, alice, peerOf(ctx, t, alice, bob), data)
	fetcher := cdnFetcher(t)

	ra := httpsRedirect(ctx, t, s, alice, documentFileLocation(sent), 0)
	da := mtclient.NewHttpsDownload(alice.c, fetcher, ra, int64(len(data)))
	for _, off := range []int64{0, 10<<20 + 512<<10, 25<<20 - 512<<10} {
		got, err := da.Read(ctx, off, 512<<10)
		if err != nil || !bytes.Equal(got, data[off:off+512<<10]) {
			t.Fatalf("Alice's range at %d: %d bytes, %v", off, len(got), err)
		}
	}

	rb := httpsRedirect(ctx, t, s, bob, documentFileLocation(receiveDocument(ctx, t, bob, sent.GetId())), 0)
	if rb.URL == ra.URL {
		// Signed in the same second: still the path is what is cached.
		t.Log("Bob got the URL Alice got")
	}
	got, err := mtclient.NewHttpsDownload(bob.c, fetcher, rb, int64(len(data))).All(ctx, 512<<10)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("Bob's download: %d bytes, %v", len(got), err)
	}

	pulls := fakeCDNCounters(t).Slices["/o/"+hex.EncodeToString(ra.ID)]
	if len(pulls) != 3 || pulls[0] != 1 || pulls[1] != 1 || pulls[2] != 1 {
		t.Errorf("the origin served the slices of the file %v times, want each once", pulls)
	}
}

// TestHttpsCdnNeedsParam: a client that does not send pluma_cdn is never
// redirected, though it sends cdn_supported and the file is prepared: an
// unpatched Android would wait forever for a DC it does not know.
func TestHttpsCdnNeedsParam(t *testing.T) {
	s := httpsCdnStand(t)
	isolateFakeCDN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := loginPatched(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, bob)

	data := randomBlob(t, 2<<20)
	sent := sendBigDocument(ctx, t, alice, peerOf(ctx, t, alice, bob), data)
	httpsRedirect(ctx, t, s, alice, documentFileLocation(sent), 0) // prepared and routed
	loc := documentFileLocation(receiveDocument(ctx, t, bob, sent.GetId()))

	for deadline := time.Now().Add(40 * time.Second); time.Now().Before(deadline); sleepCtx(ctx, t, 4*time.Second) {
		f, err := getFilePart(ctx, bob, loc, 0, 1<<20, true)
		if err != nil {
			t.Fatalf("upload.getFile with cdn_supported: %v", err)
		}
		if f.GetPredicateName() != mtproto.Predicate_upload_file || !bytes.Equal(f.GetBytes(), data[:1<<20]) {
			t.Fatalf("a client without pluma_cdn got %s", f.GetPredicateName())
		}
	}
}

// TestHttpsCdnParamChange: the same key connects again with the same
// app_version, now with pluma_cdn: the session stores the new params, and
// once bff's cache of the key lapses the redirects come; without the param
// again they stop.
func TestHttpsCdnParamChange(t *testing.T) {
	s := httpsCdnStand(t)
	isolateFakeCDN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	u := login(ctx, t, testPhones(t, 1)[0], "Alice")
	data, loc := sendSelfDocument(ctx, t, u, 2<<20)
	downloadFromHome(ctx, t, u, loc, len(data))
	waitCdnAnswer(ctx, t, u, loc, 0, false, 0)

	reconnect := func(params *mtproto.JSONValue) *user {
		t.Helper()
		cfg := config(t, "e2e Alice")
		cfg.Params = params
		c, err := mtclient.DialKey(ctx, cfg, u.c.AuthKey())
		if err != nil {
			t.Fatalf("connect with the key again: %v", err)
		}
		closeChecked(t, "Alice again", c)
		if _, err = c.InitConnection(ctx); err != nil {
			t.Fatalf("initConnection: %v", err)
		}
		return &user{c: c, phone: u.phone, self: u.self}
	}
	within := cdnCapsTTL + max(cdnRedirectWithin, s.healthWithin())

	patched := reconnect(mtclient.PlumaCdnParams())
	httpsRedirectWithin(ctx, t, s, patched, loc, 0, within)
	plain := reconnect(nil)
	waitCdnAnswer(ctx, t, plain, loc, 0, false, within)
	// The first connection, of the same key, is not redirected either.
	waitCdnAnswer(ctx, t, u, loc, 0, false, 0)
}

// TestHttpsCdnTampered: a byte of the file changed by the CDN fails the
// check of its piece, and nothing of the piece is used; the client then
// gets the file from home without cdn_supported.
func TestHttpsCdnTampered(t *testing.T) {
	s := httpsCdnStand(t)
	isolateFakeCDN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	u := loginPatched(ctx, t, testPhones(t, 1)[0], "Alice")
	data, loc := sendSelfDocument(ctx, t, u, 2<<20)
	r := httpsRedirect(ctx, t, s, u, loc, 0)

	// The first byte of the file: the canary of the checks of bff is
	// further on, so the delivery stays healthy.
	fakeCDNSwitch(t, "tamper", true, "&offset=0")
	got, err := mtclient.NewHttpsDownload(u.c, cdnFetcher(t), r, int64(len(data))).Read(ctx, 0, 128<<10)
	if !errors.Is(err, mtclient.ErrCdnHashMismatch) || got != nil {
		t.Fatalf("a changed byte: %d bytes, %v; want the hash mismatch", len(got), err)
	}
	if home := downloadFromHome(ctx, t, u, loc, len(data)); !bytes.Equal(home, data) {
		t.Fatal("the file from the home after the fallback differs")
	}
}

// TestHttpsCdnBadResponses: an answer to a Range with a Content-Range of
// other bytes, or a 200 with the whole file, is refused before a byte of
// it is used, and the file comes from home without cdn_supported.
func TestHttpsCdnBadResponses(t *testing.T) {
	s := httpsCdnStand(t)
	isolateFakeCDN(t)
	for _, sw := range []string{"badrange", "full200"} {
		t.Run(sw, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			u := loginPatched(ctx, t, testPhones(t, 1)[0], "Alice")
			data, loc := sendSelfDocument(ctx, t, u, 2<<20)
			r := httpsRedirect(ctx, t, s, u, loc, 0)

			fakeCDNSwitch(t, sw, true, "")
			got, err := mtclient.NewHttpsDownload(u.c, cdnFetcher(t), r, int64(len(data))).Read(ctx, 128<<10, 128<<10)
			fakeCDNSwitch(t, sw, false, "")
			var fe *mtclient.CdnFetchError
			if !errors.As(err, &fe) || fe.Refresh() || got != nil {
				t.Fatalf("%s: %d bytes, %v; want a refused answer and a fallback", sw, len(got), err)
			}
			if home := downloadFromHome(ctx, t, u, loc, len(data)); !bytes.Equal(home, data) {
				t.Fatal("the file from the home after the fallback differs")
			}
		})
	}
}

// TestHttpsCdnUrlExpiry: once CDN_URL_TTL passes, the CDN refuses the URL
// with 403, which makes the client ask upload.getFile for a new one; the
// new URL works, and the hashes kept so far stay.
func TestHttpsCdnUrlExpiry(t *testing.T) {
	s := httpsCdnStand(t)
	isolateFakeCDN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute+s.urlTTL)
	defer cancel()
	u := loginPatched(ctx, t, testPhones(t, 1)[0], "Alice")
	data, loc := sendSelfDocument(ctx, t, u, 2<<20)
	r := httpsRedirect(ctx, t, s, u, loc, 0)
	d := mtclient.NewHttpsDownload(u.c, cdnFetcher(t), r, int64(len(data)))
	if got, err := d.Read(ctx, 0, 128<<10); err != nil || !bytes.Equal(got, data[:128<<10]) {
		t.Fatalf("a fresh URL: %d bytes, %v", len(got), err)
	}

	sleepCtx(ctx, t, time.Until(r.Expires)+2*time.Second)
	got, err := d.Read(ctx, 128<<10, 128<<10)
	var fe *mtclient.CdnFetchError
	if !errors.As(err, &fe) || !fe.Refresh() || got != nil {
		t.Fatalf("an expired URL: %d bytes, %v; want 403 or 410", len(got), err)
	}
	r2 := httpsRedirect(ctx, t, s, u, loc, 128<<10)
	if !r2.Expires.After(r.Expires) {
		t.Errorf("the new URL expires at %v, the old one at %v", r2.Expires, r.Expires)
	}
	d.Refreshed(r2)
	if got, err := d.Read(ctx, 128<<10, 128<<10); err != nil || !bytes.Equal(got, data[128<<10:256<<10]) {
		t.Fatalf("the new URL: %d bytes, %v", len(got), err)
	}
}

// TestHttpsCdnFallbackSignal: a patched client asks without cdn_supported
// for a file it was redirected to: it gave up on the delivery, and its key
// is not redirected there until CDN_AVOID_TTL passes, while another
// user's key still is.
func TestHttpsCdnFallbackSignal(t *testing.T) {
	s := httpsCdnStand(t)
	isolateFakeCDN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute+s.avoidTTL)
	defer cancel()
	phones := testPhones(t, 2)
	alice := loginPatched(ctx, t, phones[0], "Alice")
	bob := loginPatched(ctx, t, phones[1], "Bob")
	getState(ctx, t, bob)
	data := randomBlob(t, 2<<20)
	sent := sendBigDocument(ctx, t, alice, peerOf(ctx, t, alice, bob), data)
	loc := documentFileLocation(receiveDocument(ctx, t, bob, sent.GetId()))
	httpsRedirect(ctx, t, s, bob, loc, 0)

	if home := downloadFromHome(ctx, t, bob, loc, len(data)); !bytes.Equal(home, data) {
		t.Fatal("the file from the home differs")
	}
	avoided := time.Now()
	waitCdnAnswer(ctx, t, bob, loc, 0, false, 0)
	httpsRedirect(ctx, t, s, alice, documentFileLocation(sent), 0)
	sleepCtx(ctx, t, s.avoidTTL/3)
	waitCdnAnswer(ctx, t, bob, loc, 0, false, 0)

	httpsRedirectWithin(ctx, t, s, bob, loc, 0, s.avoidTTL+cdnRedirectWithin)
	if d := time.Since(avoided); d < s.avoidTTL-2*time.Second {
		t.Errorf("Bob's key was redirected again after %v, CDN_AVOID_TTL is %v", d, s.avoidTTL)
	}
}

// TestHttpsCdnUnhealthy: while the CDN answers 502, the checks of bff take
// the delivery out and the files come from home; back up, the redirects
// resume. A CDN that serves a URL with a broken signature is out too, with
// the reason token_off: its URLs stopped expiring.
func TestHttpsCdnUnhealthy(t *testing.T) {
	s := httpsCdnStand(t)
	isolateFakeCDN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	u := loginPatched(ctx, t, testPhones(t, 1)[0], "Alice")
	_, loc := sendSelfDocument(ctx, t, u, 2<<20)
	httpsRedirect(ctx, t, s, u, loc, 0)

	reason := func() string {
		t.Helper()
		return stackSQL(t, fmt.Sprintf("SELECT unhealthy_reason FROM cdn_deliveries WHERE dc_id = %d", httpsCdnDC))
	}
	for _, c := range []struct{ sw, reason string }{{"down", "status"}, {"notoken", "token_off"}} {
		fakeCDNSwitch(t, c.sw, true, "")
		waitCdnAnswer(ctx, t, u, loc, 0, false, s.healthWithin())
		if got := reason(); got != c.reason {
			t.Errorf("%s: the delivery is out with the reason %q, want %q", c.sw, got, c.reason)
		}
		fakeCDNSwitch(t, c.sw, false, "")
		httpsRedirect(ctx, t, s, u, loc, 0)
		if got := reason(); got != "" {
			t.Errorf("%s off: the delivery is back with the reason %q", c.sw, got)
		}
	}
}

// originStatus asks the origin of bff for path from inside the stand,
// from the container of service at host, with the header lines headers
// (each ending in CRLF), and returns the status code.
func originStatus(t *testing.T, service, host, method, path, headers string) int {
	t.Helper()
	out := stackOut(t, "exec", "-T", service, "bash", "-c",
		`exec 3<>"/dev/tcp/$0/20480" && printf '%s %s HTTP/1.1\r\nHost: origin\r\nConnection: close\r\n%s\r\n' "$1" "$2" "$3" >&3 && head -n 1 <&3`,
		host, method, path, headers)
	f := strings.Fields(out)
	if len(f) < 2 || !strings.HasPrefix(f[0], "HTTP/1.") {
		t.Fatalf("the origin answered %q", out)
	}
	code, err := strconv.Atoi(f[1])
	if err != nil {
		t.Fatalf("the origin answered %q", out)
	}
	return code
}

// TestHttpsCdnOriginAuth: the origin in bff serves only the CDN, which
// sends X-Pluma-Origin-Auth: without it or with another value the canary
// and any file are 403 alike; with it the canary is there, an unknown file
// is not, and a method other than GET and HEAD is refused.
func TestHttpsCdnOriginAuth(t *testing.T) {
	t.Parallel()
	unknown := "/o/" + hex.EncodeToString(randomBlob(t, 32))
	wrong := "X-Pluma-Origin-Auth: " + hex.EncodeToString(randomBlob(t, 32)) + "\r\n"
	for _, path := range []string{"/c/v1", unknown} {
		for name, h := range map[string]string{"no header": "", "another value": wrong} {
			if code := originStatus(t, "session", "bff", "GET", path, h); code != http.StatusForbidden {
				t.Errorf("%s with %s: %d, want 403", path, name, code)
			}
		}
	}
	// bff's own value, which stays in its container.
	right := func(method, path string) int {
		t.Helper()
		out := stackOut(t, "exec", "-T", "bff", "bash", "-c",
			`exec 3<>/dev/tcp/127.0.0.1/20480 && printf '%s %s HTTP/1.1\r\nHost: origin\r\nConnection: close\r\nX-Pluma-Origin-Auth: %s\r\n\r\n' "$0" "$1" "$CDN_ORIGIN_AUTH" >&3 && head -n 1 <&3`,
			method, path)
		f := strings.Fields(out)
		if len(f) < 2 {
			t.Fatalf("the origin answered %q", out)
		}
		code, _ := strconv.Atoi(f[1])
		return code
	}
	for _, c := range []struct {
		method, path string
		want         int
	}{
		{"GET", "/c/v1", http.StatusOK},
		{"HEAD", "/c/v1", http.StatusOK},
		{"GET", unknown, http.StatusNotFound},
		{"GET", "/elsewhere", http.StatusNotFound},
		{"POST", "/c/v1", http.StatusMethodNotAllowed},
	} {
		if code := right(c.method, c.path); code != c.want {
			t.Errorf("%s %s with the header: %d, want %d", c.method, c.path, code, c.want)
		}
	}
}

// TestHttpsCdnSecretChatNotRedirected: a file of a secret chat of 2 MB is
// never redirected, though both clients are patched: it gets no row of
// the CDN, so the origin has nothing to serve for it.
func TestHttpsCdnSecretChatNotRedirected(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := loginPatched(ctx, t, phones[0], "Alice")
	bob := loginPatched(ctx, t, phones[1], "Bob")
	for _, u := range []*user{alice, bob} {
		getState(ctx, t, u)
	}
	toBob := peerOf(ctx, t, alice, bob)

	cfg, ok := dhConfig(ctx, t, alice, 0).(*mtproto.TLMessagesDhConfig)
	if !ok {
		t.Fatal("messages.getDhConfig returned no parameters")
	}
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesRequestEncryption{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_requestEncryption),
		UserId: &mtproto.InputUser{PredicateName: mtproto.Predicate_inputUser, Constructor: alice.c.Constructor(mtproto.Predicate_inputUser),
			UserId: toBob.GetUserId(), AccessHash: toBob.GetAccessHash()},
		RandomId: int32(randomID()),
		GA:       secretG(t, cfg),
	})
	if err != nil {
		t.Fatalf("messages.requestEncryption: %v", err)
	}
	waiting, ok := o.(*mtproto.TLEncryptedChatWaiting)
	if !ok {
		t.Fatalf("messages.requestEncryption returned %T", o)
	}
	encryptionUpdate(ctx, t, bob, waiting.GetId(), mtproto.Predicate_encryptedChatRequested)
	peer := mtproto.MakeTLInputEncryptedChat(&mtproto.InputEncryptedChat{ChatId: waiting.GetId(), AccessHash: waiting.GetAccessHash()}).To_InputEncryptedChat()
	if _, err = bob.c.Invoke(ctx, &mtproto.TLMessagesAcceptEncryption{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_acceptEncryption), Peer: peer, GB: secretG(t, cfg), KeyFingerprint: 7,
	}); err != nil {
		t.Fatalf("messages.acceptEncryption: %v", err)
	}
	encryptionUpdate(ctx, t, alice, waiting.GetId(), mtproto.Predicate_encryptedChat)

	// The bytes are the clients' ciphertext; the server never reads them.
	data := randomBlob(t, 2<<20)
	const part = 512 << 10
	fileId := randomID()
	for n := 0; n*part < len(data); n++ {
		if _, err = alice.c.Invoke(ctx, &mtproto.TLUploadSaveFilePart{
			Constructor: alice.c.Constructor(mtproto.Predicate_upload_saveFilePart),
			FileId:      fileId, FilePart: int32(n), Bytes: data[n*part : (n+1)*part],
		}); err != nil {
			t.Fatalf("upload.saveFilePart %d: %v", n, err)
		}
	}
	sum := md5.Sum(data)
	randomId := randomID()
	if _, err = alice.c.Invoke(ctx, &mtproto.TLMessagesSendEncryptedFile{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendEncryptedFile),
		Peer:        peer,
		RandomId:    randomId,
		Data:        []byte("the message, encrypted by the clients"),
		File: mtproto.MakeTLInputEncryptedFileUploaded(&mtproto.InputEncryptedFile{
			Id: fileId, Parts: int32(len(data) / part), Md5Checksum: hex.EncodeToString(sum[:]), KeyFingerprint: 7,
		}).To_InputEncryptedFile(),
	}); err != nil {
		t.Fatalf("messages.sendEncryptedFile: %v", err)
	}
	m, _ := newEncryptedMessage(ctx, t, bob, randomId)
	file := m.GetFile()
	loc := mtproto.MakeTLInputEncryptedFileLocation(&mtproto.InputFileLocation{Id: file.GetId(), AccessHash: file.GetAccessHash()}).To_InputFileLocation()
	if got := downloadFromHome(ctx, t, bob, loc, len(data)); !bytes.Equal(got, data) {
		t.Fatal("the file from the home differs")
	}
	for i := range 5 {
		f, err := getFilePart(ctx, bob, loc, 0, 1<<20, true)
		if err != nil {
			t.Fatalf("upload.getFile with cdn_supported: %v", err)
		}
		if f.GetPredicateName() != mtproto.Predicate_upload_file || !bytes.Equal(f.GetBytes(), data[:1<<20]) {
			t.Fatalf("a file of a secret chat, request %d: %s", i, f.GetPredicateName())
		}
		sleepCtx(ctx, t, 2*time.Second)
	}
	if n := stackSQL(t, fmt.Sprintf("SELECT COUNT(*) FROM cdn_files WHERE file_kind = 3 AND file_id = %d", file.GetId())); n != "0" {
		t.Errorf("a file of a secret chat has %s rows of the CDN", n)
	}
}

// TestHttpsCdnLegacyMethods: what is left of the MTProto CDN sends a
// client back to upload.getFile: upload.getCdnFile at the home and
// upload.reuploadCdnFile are FILE_TOKEN_INVALID, as are the hashes of a
// token that is not a PLH1 token of a file; help.getCdnConfig has no keys,
// and help.getConfig no dc_options.
func TestHttpsCdnLegacyMethods(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	u := loginPatched(ctx, t, testPhones(t, 1)[0], "Alice")
	token := randomBlob(t, 32)

	if _, err := u.c.Invoke(ctx, &mtproto.TLUploadGetCdnFile{
		Constructor: u.c.Constructor(mtproto.Predicate_upload_getCdnFile), FileToken: token, Limit: 128 << 10,
	}); !mtclient.IsRPCError(err, "FILE_TOKEN_INVALID") {
		t.Errorf("upload.getCdnFile at the home: %v, want FILE_TOKEN_INVALID", err)
	}
	if _, err := u.c.InvokeRaw(ctx, &mtproto.TLUploadReuploadCdnFile{
		Constructor: u.c.Constructor(mtproto.Predicate_upload_reuploadCdnFile), FileToken: token, RequestToken: randomBlob(t, 32),
	}); !mtclient.IsRPCError(err, "FILE_TOKEN_INVALID") {
		t.Errorf("upload.reuploadCdnFile: %v, want FILE_TOKEN_INVALID", err)
	}
	unknownPLH1 := append(append([]byte("PLH1"), randomBlob(t, 32)...), 0x7f, 0, 0, 0)
	unknownPLH1 = append(unknownPLH1, "http://fakecdn:8080/o/00"...)
	for name, tok := range map[string][]byte{"an MTProto token": token, "an unknown PLH1 token": unknownPLH1} {
		if _, err := u.c.CdnFileHashes(ctx, tok, 0); !mtclient.IsRPCError(err, "FILE_TOKEN_INVALID") {
			t.Errorf("upload.getCdnFileHashes of %s: %v, want FILE_TOKEN_INVALID", name, err)
		}
	}

	o, err := u.c.Invoke(ctx, &mtproto.TLHelpGetCdnConfig{Constructor: u.c.Constructor(mtproto.Predicate_help_getCdnConfig)})
	if err != nil {
		t.Fatalf("help.getCdnConfig: %v", err)
	}
	cc, ok := o.(interface{ To_CdnConfig() *mtproto.CdnConfig })
	if !ok || len(cc.To_CdnConfig().GetPublicKeys()) != 0 {
		t.Errorf("help.getCdnConfig: %v, want no keys", o)
	}
	o, err = u.c.Invoke(ctx, &mtproto.TLHelpGetConfig{Constructor: u.c.Constructor(mtproto.Predicate_help_getConfig)})
	if err != nil {
		t.Fatalf("help.getConfig: %v", err)
	}
	if c, ok := o.(interface{ To_Config() *mtproto.Config }); !ok || len(c.To_Config().GetDcOptions()) != 0 {
		t.Errorf("help.getConfig: dc_options are not empty")
	}
}
