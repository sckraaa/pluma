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
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/grpc/status"
)

func docLoc(id int64) *mtproto.InputFileLocation {
	return mtproto.MakeTLInputDocumentFileLocation(&mtproto.InputFileLocation{Id: id, AccessHash: 7, FileReference: []byte{1}}).To_InputFileLocation()
}

// homeEnv is a Home over a MemStore with files and a node in memory.
type homeEnv struct {
	t     *testing.T
	ctx   context.Context
	set   *Settings
	store *MemStore
	files *fakeFiles
	node  *fakeNode
	clock *clock
	home  *Home
	push  *Pusher
}

func newHomeEnv(t *testing.T) *homeEnv {
	t.Helper()
	e := &homeEnv{t: t, ctx: context.Background(), set: testSettings(t), store: NewMemStore(), files: newFakeFiles(), node: newFakeNode(testDc), clock: newClock()}
	e.home = NewHome(e.store, e.set, e.files)
	e.home.now = e.clock.now
	e.home.poll = 10 * time.Millisecond
	e.push = NewPusher(e.store, e.node, e.set, PusherDeps{Reader: e.files, Healthy: func() bool { return true }, OnReady: e.home.Forget})
	e.push.now = e.clock.now
	e.healthy(true)
	return e
}

// healthy writes the row of the node as the prober would.
func (e *homeEnv) healthy(ok bool) {
	e.t.Helper()
	ms := e.clock.now().UnixMilli()
	if _, err := e.store.RotateRtKey(e.ctx, testDc, mustRtKey(e.t), rtKeyEvery, ms); err != nil {
		e.t.Fatal(err)
	}
	n, err := e.store.Node(e.ctx, testDc)
	if err != nil {
		e.t.Fatal(err)
	}
	n.PublicKey, n.Fingerprint, n.UpdatedAt = "pem", 1, ms
	n.HealthyUntil, n.UnhealthyReason = ms+healthyFor.Milliseconds(), ""
	if !ok {
		n.HealthyUntil, n.UnhealthyReason = 0, unhealthyUnreachable
	}
	if err = e.store.PutNode(e.ctx, n); err != nil {
		e.t.Fatal(err)
	}
	// The row is cached for nodeCacheTTL.
	e.clock.add(nodeCacheTTL)
}

// pushAll pushes every queued copy.
func (e *homeEnv) pushAll() {
	e.t.Helper()
	for e.push.pushNext(e.ctx) {
	}
}

func mustRtKey(t *testing.T) []byte {
	t.Helper()
	k, err := NewRtKey()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func (e *homeEnv) replica(id int64) *Replica {
	e.t.Helper()
	f, err := e.store.Get(e.ctx, FileKey{Kind: KindDocument, Id: id})
	if err != nil || f == nil {
		e.t.Fatalf("row of %d: %v, %v", id, f, err)
	}
	r, err := e.store.Replica(e.ctx, f.RowId, testDc)
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

// TestRedirectQueuesNewFile: a large file without a row is served from home
// and queued; once its copy is ready it is redirected.
func TestRedirectQueuesNewFile(t *testing.T) {
	e := newHomeEnv(t)
	plain := randomBytes(t, 3<<20+5)
	e.files.add(FileKey{Kind: KindDocument, Id: 10}, plain)

	res, result := e.home.Redirect(e.ctx, docLoc(10), 0)
	if res != nil || result != ResultNotReady {
		t.Fatalf("first request: %v, %q", res, result)
	}
	if r := e.replica(10); r == nil || r.State != StateQueued {
		t.Fatalf("copy %+v, want queued", r)
	}

	e.pushAll()
	if r := e.replica(10); r == nil || r.State != StateReady {
		t.Fatalf("copy %+v after the push, want ready", r)
	}
	res, result = e.home.Redirect(e.ctx, docLoc(10), 1<<20)
	if result != ResultRedirect || res.GetPredicateName() != mtproto.Predicate_upload_fileCdnRedirect {
		t.Fatalf("after the push: %v, %q", res, result)
	}
	if res.GetDcId() != testDc || len(res.GetEncryptionKey()) != KeySize || len(res.GetEncryptionIv()) != IVSize || len(res.GetFileToken()) != TokenSize {
		t.Fatalf("redirect: dc %d, key %d, iv %d, token %d bytes", res.GetDcId(), len(res.GetEncryptionKey()), len(res.GetEncryptionIv()), len(res.GetFileToken()))
	}

	// What a client downloads from the node decrypts to the file, and the
	// hashes of the redirect cover the plaintext from the offset asked.
	cipherText := e.node.file(res.GetFileToken())
	if len(cipherText) != len(plain) {
		t.Fatalf("the node has %d bytes, want %d", len(cipherText), len(plain))
	}
	hashes := res.GetFileHashes()
	if len(hashes) != int(HashParts(int64(len(plain))))-8 || hashes[0].GetOffset_INT64() != 1<<20 {
		t.Fatalf("%d hashes from %d", len(hashes), hashes[0].GetOffset_INT64())
	}
	for _, h := range hashes {
		off, n := h.GetOffset_INT64(), int64(h.GetLimit())
		part := append([]byte(nil), cipherText[off:off+n]...)
		if err := DecryptAt(res.GetEncryptionKey(), res.GetEncryptionIv(), off, part); err != nil {
			t.Fatal(err)
		}
		if sum := sha256.Sum256(part); string(sum[:]) != string(h.GetHash()) {
			t.Fatalf("the hash at %d is not of the decrypted part", off)
		}
		if string(part) != string(plain[off:off+n]) {
			t.Fatalf("the part at %d does not decrypt to the file", off)
		}
	}
}

// TestRedirectSmall: a file below the threshold is never queued.
func TestRedirectSmall(t *testing.T) {
	e := newHomeEnv(t)
	e.files.add(FileKey{Kind: KindDocument, Id: 11}, randomBytes(t, 900<<10))
	for range 2 {
		if res, result := e.home.Redirect(e.ctx, docLoc(11), 0); res != nil || result != ResultSmall {
			t.Fatalf("%v, %q", res, result)
		}
	}
	if r := e.replica(11); r != nil {
		t.Fatalf("a small file was queued: %+v", r)
	}
}

// TestRedirectUnhealthy: a ready copy on a node that is out is not
// redirected to; the file is served from home.
func TestRedirectUnhealthy(t *testing.T) {
	e := newHomeEnv(t)
	e.files.add(FileKey{Kind: KindDocument, Id: 12}, randomBytes(t, 2<<20))
	e.home.Redirect(e.ctx, docLoc(12), 0)
	e.pushAll()
	e.healthy(false)
	if res, result := e.home.Redirect(e.ctx, docLoc(12), 0); res != nil || result != ResultUnhealthy {
		t.Fatalf("%v, %q", res, result)
	}
	// The row says healthy, but the check is over 30 s old.
	e.healthy(true)
	e.clock.add(healthyFor)
	if res, result := e.home.Redirect(e.ctx, docLoc(12), 0); res != nil || result != ResultUnhealthy {
		t.Fatalf("stale check: %v, %q", res, result)
	}
}

// TestRedirectStoreError: a failure of MySQL serves the file from home.
func TestRedirectStoreError(t *testing.T) {
	e := newHomeEnv(t)
	e.files.add(FileKey{Kind: KindDocument, Id: 13}, randomBytes(t, 2<<20))
	e.store.SetErr(errors.New("mysql is down"))
	if res, result := e.home.Redirect(e.ctx, docLoc(13), 0); res != nil || result != ResultError {
		t.Fatalf("%v, %q", res, result)
	}
	e.store.SetErr(nil)
	e.files.sizeErr = errors.New("media is down")
	if res, result := e.home.Redirect(e.ctx, docLoc(13), 0); res != nil || result != ResultError {
		t.Fatalf("media down: %v, %q", res, result)
	}
}

// TestRedirectOtherLocations: avatars and document thumbnails never go
// through the CDN, and nothing is asked about them.
func TestRedirectOtherLocations(t *testing.T) {
	e := newHomeEnv(t)
	e.files.sizeErr = errors.New("must not be asked")
	for _, loc := range []*mtproto.InputFileLocation{
		mtproto.MakeTLInputPeerPhotoFileLocation(&mtproto.InputFileLocation{PhotoId: 5, Peer: mtproto.MakeTLInputPeerSelf(nil).To_InputPeer()}).To_InputFileLocation(),
		mtproto.MakeTLInputDocumentFileLocation(&mtproto.InputFileLocation{Id: 5, ThumbSize: "m"}).To_InputFileLocation(),
	} {
		if res, result := e.home.Redirect(e.ctx, loc, 0); res != nil || result != "" {
			t.Fatalf("%s: %v, %q", loc.GetPredicateName(), res, result)
		}
	}
}

// readyFile makes a ready copy of a file of size bytes and returns its
// row.
func (e *homeEnv) readyFile(id int64, size int) *File {
	e.t.Helper()
	e.files.add(FileKey{Kind: KindDocument, Id: id}, randomBytes(e.t, size))
	e.home.Redirect(e.ctx, docLoc(id), 0)
	e.pushAll()
	f, err := e.store.Get(e.ctx, FileKey{Kind: KindDocument, Id: id})
	if err != nil || f == nil || !f.HasKey() {
		e.t.Fatalf("file %d: %+v, %v", id, f, err)
	}
	return f
}

func (e *homeEnv) requestToken(fileToken []byte) []byte {
	e.t.Helper()
	n, err := e.store.Node(e.ctx, testDc)
	if err != nil {
		e.t.Fatal(err)
	}
	return SignRequestToken(n.RtKey, fileToken, e.clock.now())
}

func isError(err, want error) bool {
	return status.Convert(err).Message() == status.Convert(want).Message() && status.Code(err) == status.Code(want)
}

// TestReuploadErrors: the answers that send the client back to
// upload.getFile.
func TestReuploadErrors(t *testing.T) {
	e := newHomeEnv(t)
	f := e.readyFile(20, 2<<20)
	token := f.Secrets.Token

	other := randomBytes(t, TokenSize)
	if _, err := e.home.Reupload(e.ctx, 1, other, e.requestToken(other)); !isError(err, mtproto.ErrFileTokenInvalid) {
		t.Fatalf("unknown file_token: %v", err)
	}
	bad := e.requestToken(token)
	bad[len(bad)-1] ^= 1
	if _, err := e.home.Reupload(e.ctx, 1, token, bad); !isError(err, ErrRequestTokenInvalid) {
		t.Fatalf("bad signature: %v", err)
	}
	// A request_token of another file does not pass for this one.
	if _, err := e.home.Reupload(e.ctx, 1, token, e.requestToken(other)); !isError(err, ErrRequestTokenInvalid) {
		t.Fatalf("token of another file: %v", err)
	}
	old := e.requestToken(token)
	e.clock.add(ReqTokenTTL + time.Second)
	if _, err := e.home.Reupload(e.ctx, 1, token, old); !isError(err, ErrRequestTokenInvalid) {
		t.Fatalf("expired token: %v", err)
	}
	// Nothing was changed by the refused calls.
	if r := e.replica(20); r.State != StateReady {
		t.Fatalf("copy %+v after refused calls", r)
	}

	signed := e.requestToken(token)
	e.store.SetErr(errors.New("mysql is down"))
	if _, err := e.home.Reupload(e.ctx, 1, token, signed); !isError(err, mtproto.ErrFileTokenInvalid) {
		t.Fatalf("mysql down: %v", err)
	}
}

// TestReuploadReady: the copy is pushed again while the call waits, and the
// hashes from offset 0 answer.
func TestReuploadReady(t *testing.T) {
	e := newHomeEnv(t)
	f := e.readyFile(21, 2<<20+100)
	if _, err := e.node.Delete(e.ctx, f.Secrets.Token); err != nil { // the node lost it
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		// The pusher picks the copy up while the call waits.
		for range 400 {
			r, err := e.store.Replica(e.ctx, f.RowId, testDc)
			if err == nil && r != nil && r.State == StateQueued && r.Priority == PriorityReupload {
				for e.push.pushNext(e.ctx) {
				}
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	hashes, err := e.home.Reupload(e.ctx, 1, f.Secrets.Token, e.requestToken(f.Secrets.Token))
	<-done
	if err != nil {
		t.Fatal(err)
	}
	if len(hashes) != 17 || hashes[0].GetOffset_INT64() != 0 || hashes[16].GetLimit() != 100 {
		t.Fatalf("%d hashes", len(hashes))
	}
	if e.node.file(f.Secrets.Token) == nil {
		t.Fatal("the node did not get the file again")
	}
}

// TestReuploadTimeout: a copy not ready within the wait answers
// FILE_TOKEN_INVALID, and stays queued first.
func TestReuploadTimeout(t *testing.T) {
	e := newHomeEnv(t)
	e.set.ReuploadWait = 50 * time.Millisecond
	f := e.readyFile(22, 2<<20)
	start := time.Now()
	if _, err := e.home.Reupload(e.ctx, 1, f.Secrets.Token, e.requestToken(f.Secrets.Token)); !isError(err, mtproto.ErrFileTokenInvalid) {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) < e.set.ReuploadWait {
		t.Fatal("answered before the wait")
	}
	if r := e.replica(22); r.State != StateQueued || r.Priority != PriorityReupload {
		t.Fatalf("copy %+v, want queued first", r)
	}
}

// TestReuploadUnhealthy: with the node out nothing is waited for.
func TestReuploadUnhealthy(t *testing.T) {
	e := newHomeEnv(t)
	f := e.readyFile(23, 2<<20)
	e.healthy(false)
	if _, err := e.home.Reupload(e.ctx, 1, f.Secrets.Token, e.requestToken(f.Secrets.Token)); !isError(err, mtproto.ErrFileTokenInvalid) {
		t.Fatalf("err = %v", err)
	}
	if r := e.replica(23); r.State != StateReady {
		t.Fatalf("copy %+v changed for a node that is out", r)
	}
}

// TestReuploadLimit: past 30 calls a minute a user waits, others do not.
func TestReuploadLimit(t *testing.T) {
	e := newHomeEnv(t)
	other := randomBytes(t, TokenSize)
	for i := range reuploadsPerMinute {
		if _, err := e.home.Reupload(e.ctx, 1, other, nil); !isError(err, mtproto.ErrFileTokenInvalid) {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	_, err := e.home.Reupload(e.ctx, 1, other, nil)
	if status.Code(err) != 420 || status.Convert(err).Message() != "FLOOD_WAIT_60" {
		t.Fatalf("call over the limit: %v", err)
	}
	if _, err = e.home.Reupload(e.ctx, 2, other, nil); !isError(err, mtproto.ErrFileTokenInvalid) {
		t.Fatalf("another user: %v", err)
	}
	e.clock.add(time.Minute)
	if _, err = e.home.Reupload(e.ctx, 1, other, nil); !isError(err, mtproto.ErrFileTokenInvalid) {
		t.Fatalf("a minute later: %v", err)
	}
}

// TestFileHashes: upload.getCdnFileHashes.
func TestFileHashes(t *testing.T) {
	e := newHomeEnv(t)
	f := e.readyFile(30, 9<<20)
	hashes, err := e.home.FileHashes(e.ctx, f.Secrets.Token, 1<<20+4096)
	if err != nil {
		t.Fatal(err)
	}
	if len(hashes) != MaxHashesPerAnswer || hashes[0].GetOffset_INT64() != 1<<20 {
		t.Fatalf("%d hashes from %d", len(hashes), hashes[0].GetOffset_INT64())
	}
	if _, err = e.home.FileHashes(e.ctx, randomBytes(t, TokenSize), 0); !isError(err, mtproto.ErrFileTokenInvalid) {
		t.Fatalf("unknown token: %v", err)
	}
	if _, err = e.home.FileHashes(e.ctx, []byte{1}, 0); !isError(err, mtproto.ErrFileTokenInvalid) {
		t.Fatalf("short token: %v", err)
	}
}

// TestCdnConfig: the keys of every node with one, healthy or not.
func TestCdnConfig(t *testing.T) {
	e := newHomeEnv(t)
	e.healthy(false)
	if err := e.store.PutNode(e.ctx, &Node{DcId: 202, UpdatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	cfg, err := e.home.CdnConfig(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	keys := cfg.GetPublicKeys()
	if len(keys) != 1 || keys[0].GetDcId() != testDc || keys[0].GetPublicKey() != "pem" {
		t.Fatalf("keys %v", keys)
	}
	if empty := CdnConfigOf(nil); empty.GetPredicateName() != mtproto.Predicate_cdnConfig || len(empty.GetPublicKeys()) != 0 {
		t.Fatalf("empty config %v", empty)
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
