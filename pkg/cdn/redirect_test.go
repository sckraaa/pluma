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
	"context"
	"crypto/sha256"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/grpc/status"
)

func docLoc(id int64) *mtproto.InputFileLocation {
	return mtproto.MakeTLInputDocumentFileLocation(&mtproto.InputFileLocation{Id: id, AccessHash: 7, FileReference: []byte{1}}).To_InputFileLocation()
}

// testKey is the permanent auth key of a client that sent pluma_cdn 1,
// otherKey of one that sent none.
const (
	testKey  = int64(0x7e57_0000_0000_0001)
	otherKey = int64(0x7e57_0000_0000_0002)
)

// fakeCaps is the pluma_cdn of each key; err fails every read.
type fakeCaps struct {
	mu       sync.Mutex
	versions map[int64]int
	err      error
	reads    int
}

func (c *fakeCaps) PlumaCdn(_ context.Context, key int64) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads++
	if c.err != nil {
		return 0, c.err
	}
	return c.versions[key], nil
}

func (c *fakeCaps) set(key int64, v int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.versions[key] = v
}

// homeEnv is a Home over a MemStore with files in memory.
type homeEnv struct {
	t     *testing.T
	ctx   context.Context
	set   *Settings
	store *MemStore
	files *fakeFiles
	caps  *fakeCaps
	clock *clock
	home  *Home
}

func newHomeEnv(t *testing.T) *homeEnv {
	t.Helper()
	return newHomeEnvWith(t, testSettings(t))
}

func newHomeEnvWith(t *testing.T, set *Settings) *homeEnv {
	t.Helper()
	e := &homeEnv{
		t: t, ctx: context.Background(), set: set, store: NewMemStore(), files: newFakeFiles(),
		caps: &fakeCaps{versions: map[int64]int{testKey: 1}}, clock: newClock(),
	}
	var err error
	if e.home, err = NewHome(e.store, e.set, e.files, e.caps); err != nil {
		t.Fatal(err)
	}
	e.home.now = e.clock.now
	e.healthy(true)
	return e
}

// healthy writes the health of the delivery as its check would.
func (e *homeEnv) healthy(ok bool) {
	e.t.Helper()
	e.healthyDc(testDc, ok)
}

// healthyDc writes the health of delivery dc as its check would.
func (e *homeEnv) healthyDc(dc int32, ok bool) {
	e.t.Helper()
	ms := e.clock.now().UnixMilli()
	h := &DeliveryHealth{DcId: dc, HealthyUntil: ms + 3*time.Minute.Milliseconds(), UpdatedAt: ms}
	if !ok {
		h.HealthyUntil, h.UnhealthyReason = 0, "status"
	}
	if err := e.store.PutDeliveryHealth(e.ctx, h); err != nil {
		e.t.Fatal(err)
	}
	// The health is cached for healthCacheTTL.
	e.clock.add(healthCacheTTL)
}

// prepareAll prepares every queued file for the origin: its key and the
// hashes of its plaintext.
func (e *homeEnv) prepareAll() {
	e.t.Helper()
	for {
		c, err := e.store.Claim(e.ctx, TargetHomeOrigin, time.Minute, e.clock.now().UnixMilli())
		if err != nil {
			e.t.Fatal(err)
		}
		if c == nil {
			return
		}
		if _, err = e.store.SetKey(e.ctx, c.File.RowId, mustSecrets(e.t)); err != nil {
			e.t.Fatal(err)
		}
		h := NewHasher()
		_, _ = h.Write(e.files.files[c.File.Key])
		if ok, err := e.store.MarkReady(e.ctx, c, h.Sum(), e.clock.now().UnixMilli()); !ok || err != nil {
			e.t.Fatalf("MarkReady: %v, %v", ok, err)
		}
		e.home.Forget(c.File.Key)
	}
}

func (e *homeEnv) replica(id int64) *Replica {
	e.t.Helper()
	f, err := e.store.Get(e.ctx, FileKey{Kind: KindDocument, Id: id})
	if err != nil || f == nil {
		e.t.Fatalf("row of %d: %v, %v", id, f, err)
	}
	r, err := e.store.Replica(e.ctx, f.RowId, TargetHomeOrigin)
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

// TestRedirectQueuesNewFile: a large file without a row is served from home
// and queued; once it is prepared it is redirected with a PLH1 token whose
// URL is signed for the delivery.
func TestRedirectQueuesNewFile(t *testing.T) {
	e := newHomeEnv(t)
	plain := randomBytes(t, 3<<20+5)
	e.files.add(FileKey{Kind: KindDocument, Id: 10}, plain)

	res, result := e.home.Redirect(e.ctx, testKey, docLoc(10), 0)
	if res != nil || result != ResultNotReady {
		t.Fatalf("first request: %v, %q", res, result)
	}
	if r := e.replica(10); r == nil || r.State != StateQueued {
		t.Fatalf("preparation %+v, want queued", r)
	}

	e.prepareAll()
	res, result = e.home.Redirect(e.ctx, testKey, docLoc(10), 1<<20)
	if result != ResultRedirect || res.GetPredicateName() != mtproto.Predicate_upload_fileCdnRedirect {
		t.Fatalf("after the preparation: %v, %q", res, result)
	}
	if res.GetDcId() != testDc || len(res.GetEncryptionKey()) != KeySize || len(res.GetEncryptionIv()) != IVSize {
		t.Fatalf("redirect: dc %d, key %d, iv %d bytes", res.GetDcId(), len(res.GetEncryptionKey()), len(res.GetEncryptionIv()))
	}
	id, exp, url, err := ParseRedirectToken(res.GetFileToken())
	if err != nil {
		t.Fatalf("file_token: %v", err)
	}
	f, _ := e.store.Get(e.ctx, FileKey{Kind: KindDocument, Id: 10})
	if string(id) != string(f.Secrets.Token) {
		t.Fatal("the token does not carry the id of the file")
	}
	wantExp := e.clock.now().Unix() + int64(DefaultURLTTL/time.Second)
	if exp != wantExp || !strings.HasPrefix(url, "https://cdn.example.com"+ObjectPath(id)+"?md5=") ||
		!strings.HasSuffix(url, "&expires="+strconv.FormatInt(wantExp, 10)) {
		t.Fatalf("URL until %d, want %d", exp, wantExp)
	}

	// The hashes of the redirect cover the plaintext from the offset asked.
	hashes := res.GetFileHashes()
	if len(hashes) != int(HashParts(int64(len(plain))))-8 || hashes[0].GetOffset_INT64() != 1<<20 {
		t.Fatalf("%d hashes from %d", len(hashes), hashes[0].GetOffset_INT64())
	}
	for _, h := range hashes {
		off, n := h.GetOffset_INT64(), int64(h.GetLimit())
		if sum := sha256.Sum256(plain[off : off+n]); string(sum[:]) != string(h.GetHash()) {
			t.Fatalf("the hash at %d is not of the plaintext", off)
		}
	}
}

// TestRedirectSmall: a file below the threshold is never queued.
func TestRedirectSmall(t *testing.T) {
	e := newHomeEnv(t)
	e.files.add(FileKey{Kind: KindDocument, Id: 11}, randomBytes(t, 900<<10))
	for range 2 {
		if res, result := e.home.Redirect(e.ctx, testKey, docLoc(11), 0); res != nil || result != ResultSmall {
			t.Fatalf("%v, %q", res, result)
		}
	}
	if r := e.replica(11); r != nil {
		t.Fatalf("a small file was queued: %+v", r)
	}
}

// TestRedirectUnhealthy: a prepared file is not redirected to a delivery
// that is out, or whose last check is too old; it is served from home.
func TestRedirectUnhealthy(t *testing.T) {
	e := newHomeEnv(t)
	e.files.add(FileKey{Kind: KindDocument, Id: 12}, randomBytes(t, 2<<20))
	e.home.Redirect(e.ctx, testKey, docLoc(12), 0)
	e.prepareAll()
	e.healthy(false)
	if res, result := e.home.Redirect(e.ctx, testKey, docLoc(12), 0); res != nil || result != ResultUnhealthy {
		t.Fatalf("%v, %q", res, result)
	}
	e.healthy(true)
	e.clock.add(3 * time.Minute)
	if res, result := e.home.Redirect(e.ctx, testKey, docLoc(12), 0); res != nil || result != ResultUnhealthy {
		t.Fatalf("stale check: %v, %q", res, result)
	}
}

// TestRedirectStoreError: a failure of MySQL serves the file from home.
func TestRedirectStoreError(t *testing.T) {
	e := newHomeEnv(t)
	e.files.add(FileKey{Kind: KindDocument, Id: 13}, randomBytes(t, 2<<20))
	e.store.SetErr(errors.New("mysql is down"))
	if res, result := e.home.Redirect(e.ctx, testKey, docLoc(13), 0); res != nil || result != ResultError {
		t.Fatalf("%v, %q", res, result)
	}
	e.store.SetErr(nil)
	e.files.sizeErr = errors.New("media is down")
	if res, result := e.home.Redirect(e.ctx, testKey, docLoc(13), 0); res != nil || result != ResultError {
		t.Fatalf("media down: %v, %q", res, result)
	}
}

// TestRedirectOtherLocations: avatars, document thumbnails and the files
// of secret chats never go through the CDN, and nothing is asked about
// them.
func TestRedirectOtherLocations(t *testing.T) {
	e := newHomeEnv(t)
	e.files.sizeErr = errors.New("must not be asked")
	e.store.SetErr(errors.New("must not be asked"))
	for _, loc := range []*mtproto.InputFileLocation{
		mtproto.MakeTLInputPeerPhotoFileLocation(&mtproto.InputFileLocation{PhotoId: 5, Peer: mtproto.MakeTLInputPeerSelf(nil).To_InputPeer()}).To_InputFileLocation(),
		mtproto.MakeTLInputDocumentFileLocation(&mtproto.InputFileLocation{Id: 5, ThumbSize: "m"}).To_InputFileLocation(),
		mtproto.MakeTLInputEncryptedFileLocation(&mtproto.InputFileLocation{Id: 5, AccessHash: 7}).To_InputFileLocation(),
	} {
		if res, result := e.home.Redirect(e.ctx, testKey, loc, 0); res != nil || result != "" {
			t.Fatalf("%s: %v, %q", loc.GetPredicateName(), res, result)
		}
	}
	if e.caps.reads != 0 {
		t.Fatalf("pluma_cdn read %d times", e.caps.reads)
	}
}

// TestNewHomeRoute: the delivery of the route must be on.
func TestNewHomeRoute(t *testing.T) {
	set := testSettings(t)
	set.Routes = []Route{{Deliveries: []int32{202}}}
	if _, err := NewHome(NewMemStore(), set, newFakeFiles(), &fakeCaps{}); err == nil {
		t.Fatal("a Home on a delivery that is off")
	}
	set.Routes = nil
	if _, err := NewHome(NewMemStore(), set, newFakeFiles(), &fakeCaps{}); err == nil {
		t.Fatal("a Home without a route")
	}
}

func isError(err, want error) bool {
	return status.Convert(err).Message() == status.Convert(want).Message() && status.Code(err) == status.Code(want)
}

// TestReupload: always FILE_TOKEN_INVALID; past 30 calls a minute a user
// waits, others do not.
func TestReupload(t *testing.T) {
	e := newHomeEnv(t)
	for i := range reuploadsPerMinute {
		if err := e.home.Reupload(1); !isError(err, mtproto.ErrFileTokenInvalid) {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	err := e.home.Reupload(1)
	if status.Code(err) != 420 || status.Convert(err).Message() != "FLOOD_WAIT_60" {
		t.Fatalf("call over the limit: %v", err)
	}
	if err = e.home.Reupload(2); !isError(err, mtproto.ErrFileTokenInvalid) {
		t.Fatalf("another user: %v", err)
	}
	e.clock.add(time.Minute)
	if err = e.home.Reupload(1); !isError(err, mtproto.ErrFileTokenInvalid) {
		t.Fatalf("a minute later: %v", err)
	}
}

// TestFileHashes: upload.getCdnFileHashes takes the PLH1 token of a
// redirect, nothing else.
func TestFileHashes(t *testing.T) {
	e := newHomeEnv(t)
	e.files.add(FileKey{Kind: KindDocument, Id: 30}, randomBytes(t, 9<<20))
	e.home.Redirect(e.ctx, testKey, docLoc(30), 0)
	e.prepareAll()
	res, result := e.home.Redirect(e.ctx, testKey, docLoc(30), 0)
	if result != ResultRedirect {
		t.Fatalf("redirect: %q", result)
	}
	token := res.GetFileToken()
	hashes, err := e.home.FileHashes(e.ctx, token, 1<<20+4096)
	if err != nil {
		t.Fatal(err)
	}
	if len(hashes) != MaxHashesPerAnswer || hashes[0].GetOffset_INT64() != 1<<20 {
		t.Fatalf("%d hashes from %d", len(hashes), hashes[0].GetOffset_INT64())
	}
	id, _, _, _ := ParseRedirectToken(token)
	unknown, err := EncodeRedirectToken(randomBytes(t, TokenSize), 1, "https://cdn.example.com/o/x")
	if err != nil {
		t.Fatal(err)
	}
	for name, tok := range map[string][]byte{
		"the bare id": id,
		"unknown id":  unknown,
		"short token": token[:len(RedirectTokenMagic)+TokenSize],
		"other magic": append([]byte("PLH2"), token[len(RedirectTokenMagic):]...),
		"empty":       nil,
	} {
		if _, err = e.home.FileHashes(e.ctx, tok, 0); !isError(err, mtproto.ErrFileTokenInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err = e.home.FileHashes(e.ctx, token, -1); !isError(err, mtproto.ErrFileTokenInvalid) {
		t.Errorf("negative offset: %v", err)
	}
}

// TestEmptyCdnConfig: help.getCdnConfig names no key.
func TestEmptyCdnConfig(t *testing.T) {
	if c := EmptyCdnConfig(); c.GetPredicateName() != mtproto.Predicate_cdnConfig || len(c.GetPublicKeys()) != 0 {
		t.Fatalf("config %v", c)
	}
}

// TestStateCacheBytes: the cache keeps within its bytes, the least recently
// used going first.
func TestStateCacheBytes(t *testing.T) {
	c := newStateCache(3*(stateOverhead+100), time.Minute)
	now := time.Unix(1000, 0)
	st := func() *fileState { return &fileState{file: &File{Hashes: make([]byte, 100)}} }
	for i := range 3 {
		c.put(FileKey{Id: int64(i)}, st(), now)
	}
	c.get(FileKey{Id: 0}, now) // 1 is now the least recent
	c.put(FileKey{Id: 3}, st(), now)
	if c.get(FileKey{Id: 1}, now) != nil || c.get(FileKey{Id: 0}, now) == nil || c.get(FileKey{Id: 3}, now) == nil {
		t.Fatal("the least recently used entry was not the one dropped")
	}
	if c.get(FileKey{Id: 0}, now.Add(time.Minute)) != nil {
		t.Fatal("an entry outlived its ttl")
	}
	if c.bytes > c.maxBytes {
		t.Fatalf("%d bytes of %d", c.bytes, c.maxBytes)
	}
}

// redirectable returns an env whose document id of size bytes is prepared
// for the origin and redirected to testKey.
func redirectable(t *testing.T, id int64, size int) *homeEnv {
	t.Helper()
	e := newHomeEnv(t)
	e.files.add(FileKey{Kind: KindDocument, Id: id}, randomBytes(t, size))
	e.home.Redirect(e.ctx, testKey, docLoc(id), 0)
	e.prepareAll()
	if _, result := e.home.Redirect(e.ctx, testKey, docLoc(id), 0); result != ResultRedirect {
		t.Fatalf("prepared file: %q", result)
	}
	return e
}

// TestRedirectRequiresPlumaCdnParam: cdn_supported alone, which every
// Android and tdesktop sends, gets no redirect: only a key whose client sent
// pluma_cdn does, and an unbound key never does.
func TestRedirectRequiresPlumaCdnParam(t *testing.T) {
	e := redirectable(t, 40, 2<<20)
	for name, key := range map[string]int64{"no pluma_cdn": otherKey, "no key": 0} {
		if res, result := e.home.Redirect(e.ctx, key, docLoc(40), 0); res != nil || result != ResultNoCaps {
			t.Fatalf("%s: %s, %q", name, res.GetPredicateName(), result)
		}
	}
	e.caps.set(otherKey, 1)
	if _, result := e.home.Redirect(e.ctx, otherKey, docLoc(40), 0); result != ResultRedirect {
		t.Fatalf("after pluma_cdn: %q", result)
	}
}

// TestRedirectCapsError: when the params of the key cannot be read the file
// is served from home.
func TestRedirectCapsError(t *testing.T) {
	e := redirectable(t, 41, 2<<20)
	e.caps.err = errors.New("authsession is down")
	if res, result := e.home.Redirect(e.ctx, testKey, docLoc(41), 0); res != nil || result != ResultError {
		t.Fatalf("%v, %q", res, result)
	}
}

// TestRedirectFallbackAvoids: a key that asks for a file it was just
// redirected to without cdn_supported fell back from the delivery, and gets
// no redirect to it for AvoidTTL; other keys still do. A request after the
// window, or a second one, is no fallback.
func TestRedirectFallbackAvoids(t *testing.T) {
	e := redirectable(t, 42, 2<<20)
	if !e.home.ServedHome(testKey, docLoc(42)) {
		t.Fatal("the request without cdn_supported is no fallback")
	}
	if e.home.ServedHome(testKey, docLoc(42)) {
		t.Fatal("one redirect counted as two fallbacks")
	}
	if res, result := e.home.Redirect(e.ctx, testKey, docLoc(42), 0); res != nil || result != ResultAvoided {
		t.Fatalf("after the fallback: %v, %q", res, result)
	}
	e.caps.set(otherKey, 1)
	if _, result := e.home.Redirect(e.ctx, otherKey, docLoc(42), 0); result != ResultRedirect {
		t.Fatalf("another key: %q", result)
	}

	e.clock.add(e.set.AvoidTTL)
	e.healthy(true)
	if _, result := e.home.Redirect(e.ctx, testKey, docLoc(42), 0); result != ResultRedirect {
		t.Fatalf("after AvoidTTL: %q", result)
	}
	e.clock.add(fallbackWindow)
	if e.home.ServedHome(testKey, docLoc(42)) {
		t.Fatal("a request long after the redirect is a fallback")
	}
	if e.home.ServedHome(otherKey, docLoc(43)) || e.home.ServedHome(testKey, mtproto.MakeTLInputPeerPhotoFileLocation(&mtproto.InputFileLocation{PhotoId: 5}).To_InputFileLocation()) {
		t.Fatal("a file never redirected is a fallback")
	}
}

// TestRedirectBudget: a delivery whose month reached its budget, by the
// origin's bytes and the estimate of its egress, takes no redirect until
// the next month (UTC).
func TestRedirectBudget(t *testing.T) {
	e := newHomeEnv(t)
	d, _ := e.set.Delivery(testDc)
	month := UsageMonth(e.clock.now())
	if err := e.store.AddUsage(e.ctx, testDc, month, d.MonthlyBytes-2, 1); err != nil {
		t.Fatal(err)
	}
	e.files.add(FileKey{Kind: KindDocument, Id: 44}, randomBytes(t, 2<<20))
	e.home.Redirect(e.ctx, testKey, docLoc(44), 0)
	e.prepareAll()
	if _, result := e.home.Redirect(e.ctx, testKey, docLoc(44), 0); result != ResultRedirect {
		t.Fatalf("below the budget: %q", result)
	}
	if err := e.store.AddUsage(e.ctx, testDc, month, 0, 1); err != nil {
		t.Fatal(err)
	}
	e.clock.add(budgetCacheTTL)
	e.healthy(true)
	if res, result := e.home.Redirect(e.ctx, testKey, docLoc(44), 0); res != nil || result != ResultBudget {
		t.Fatalf("at the budget: %v, %q", res, result)
	}
	// 2026-12-01 00:00 UTC.
	e.clock.add(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC).Sub(e.clock.now()))
	e.healthy(true)
	if _, result := e.home.Redirect(e.ctx, testKey, docLoc(44), 0); result != ResultRedirect {
		t.Fatalf("the next month: %q", result)
	}
}

// TestRedirectSecondDelivery: with the preferred delivery out, a prepared
// file goes to the next one of the route.
func TestRedirectSecondDelivery(t *testing.T) {
	c := testConfig(t)
	second := c.Deliveries[0]
	second.Id, second.Name, second.BaseURL = 202, "second", "https://cdn2.example.com"
	c.Deliveries = append(c.Deliveries, second)
	c.Routes = "*:201,202"
	set, err := c.Parse()
	if err != nil {
		t.Fatal(err)
	}
	e := newHomeEnvWith(t, set)
	e.files.add(FileKey{Kind: KindDocument, Id: 45}, randomBytes(t, 2<<20))
	e.home.Redirect(e.ctx, testKey, docLoc(45), 0)
	e.prepareAll()
	e.healthy(false)
	e.healthyDc(202, true)
	res, result := e.home.Redirect(e.ctx, testKey, docLoc(45), 0)
	if result != ResultRedirect || res.GetDcId() != 202 {
		t.Fatalf("%q to %d, want a redirect to 202", result, res.GetDcId())
	}
	if _, _, url, err := ParseRedirectToken(res.GetFileToken()); err != nil || !strings.HasPrefix(url, "https://cdn2.example.com/o/") {
		t.Fatalf("URL of the second delivery: %v", err)
	}
	e.healthyDc(202, false)
	if res, result = e.home.Redirect(e.ctx, testKey, docLoc(45), 0); res != nil || result != ResultUnhealthy {
		t.Fatalf("both out: %v, %q", res, result)
	}
}
