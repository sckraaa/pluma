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
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/zeromicro/go-zero/core/logx"
	"golang.org/x/time/rate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// FileReader reads a file the way upload.getFile does: dfs.downloadFile.
type FileReader interface {
	// ReadPart returns the bytes of the file of loc from offset, a multiple
	// of limit, at most limit (PushChunkSize). dfs answers a part it
	// failed to read with no bytes, not an error
	// (app/service/dfs/internal/core/dfs.downloadFile_handler.go): the
	// pusher checks every length.
	ReadPart(ctx context.Context, loc *mtproto.InputFileLocation, offset int64, limit int32) ([]byte, error)
}

// Location is the location dfs reads the file of key by. dfs finds a
// document and a secret chat file by id and a photo by id and size; the
// access hash only names the storage type of its answer, which the pusher
// does not use (dfs.downloadFile_handler.go).
func Location(key FileKey) *mtproto.InputFileLocation {
	switch key.Kind {
	case KindDocument:
		return mtproto.MakeTLInputDocumentFileLocation(&mtproto.InputFileLocation{Id: key.Id, FileReference: []byte{}}).To_InputFileLocation()
	case KindPhoto:
		return mtproto.MakeTLInputPhotoFileLocation(&mtproto.InputFileLocation{Id: key.Id, FileReference: []byte{}, ThumbSize: key.Thumb}).To_InputFileLocation()
	case KindEncrypted:
		return mtproto.MakeTLInputEncryptedFileLocation(&mtproto.InputFileLocation{Id: key.Id}).To_InputFileLocation()
	}
	return nil
}

// Limits of the pusher (task 102).
const (
	// pushLease is how long a claim holds a copy; it is renewed while the
	// push goes on, past half of it.
	pushLease = 10 * time.Minute
	// pushIdle is how long a pusher waits when the queue is empty or the
	// node is out, unless woken.
	pushIdle = 5 * time.Second
	// pushSlack is added to the time a push should take at the rate for
	// the bound of the whole push.
	pushSlack = 2 * time.Minute
)

// Reasons of failed pushes (cdn_replicas.last_error, pluma_cdn_push_total).
const (
	pushOK          = "ok"
	pushShortRead   = "short_read"
	pushReadError   = "read_error"
	pushNodeFull    = "node_full"
	pushNodeError   = "node_error"
	pushMismatch    = "node_mismatch"
	pushTooLarge    = "too_large"
	pushBadSize     = "bad_size"
	pushLeaseLost   = "lease_lost"
	pushStoreError  = "store_error"
	pushInterrupted = "interrupted"
)

// Pusher pushes the queued copies of files to the node (ADR 0065 §6): it
// reads each file from dfs a megabyte at a time, hashes the plaintext,
// encrypts it and sends the cipher at a bounded rate; the copy is ready
// only when the node stored the same size and SHA-256.
type Pusher struct {
	store   Store
	node    NodeAPI
	reader  FileReader
	set     *Settings
	healthy func() bool
	cap     func() int64 // the node's cache, 0 if not known
	onReady func(FileKey)
	limiter *rate.Limiter
	now     func() time.Time
	idle    time.Duration
	wake    chan struct{}
}

// PusherDeps are what a Pusher needs besides the store and the node.
type PusherDeps struct {
	Reader FileReader
	// Healthy reports whether the node takes pushes now (the prober's
	// last word).
	Healthy func() bool
	// CacheCap is the node's cache in bytes, 0 if not known: a larger
	// file is never pushed.
	CacheCap func() int64
	// OnReady is called when a copy of the file of a key became ready.
	OnReady func(FileKey)
}

// NewPusher returns the pusher of the node of set.
func NewPusher(store Store, node NodeAPI, set *Settings, deps PusherDeps) *Pusher {
	return &Pusher{
		store:   store,
		node:    node,
		reader:  deps.Reader,
		set:     set,
		healthy: deps.Healthy,
		cap:     deps.CacheCap,
		onReady: deps.OnReady,
		limiter: rate.NewLimiter(rate.Limit(set.PushBytesPerSecond), PushChunkSize),
		now:     time.Now,
		idle:    pushIdle,
		wake:    make(chan struct{}, 1),
	}
}

// Wake makes an idle pusher look at the queue now: a copy was queued.
func (p *Pusher) Wake() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// Run pushes until ctx is done, PushConcurrency files at a time.
func (p *Pusher) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for range p.set.PushConcurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.work(ctx)
		}()
	}
	wg.Wait()
}

func (p *Pusher) work(ctx context.Context) {
	for ctx.Err() == nil {
		if !p.healthy() || !p.pushNext(ctx) {
			select {
			case <-ctx.Done():
			case <-p.wake:
			case <-time.After(p.idle):
			}
		}
	}
}

// pushNext claims the next copy and pushes it; false if there was none.
func (p *Pusher) pushNext(ctx context.Context) bool {
	c, err := p.store.Claim(ctx, p.set.DcId, pushLease, p.now().UnixMilli())
	if err != nil {
		if ctx.Err() == nil {
			logx.WithContext(ctx).Errorf("cdn: claim a push: %v", err)
		}
		return false
	}
	if c == nil {
		return false
	}
	result, err := p.push(ctx, c)
	metricPushes.Inc(result)
	if err != nil && result != pushInterrupted {
		logx.WithContext(ctx).Errorf("cdn: push of file row %d (%d bytes), attempt %d: %s: %v", c.File.RowId, c.File.Size, c.Attempts, result, err)
	}
	return true
}

// push pushes the copy of c and records the outcome; it returns the result
// and the error behind a failure.
func (p *Pusher) push(ctx context.Context, c *Claim) (string, error) {
	result, retry, err := p.send(ctx, c)
	switch {
	case result == pushOK || result == pushLeaseLost:
		return result, err
	case ctx.Err() != nil:
		// Stopping: the lease runs out and the copy is claimed again.
		return pushInterrupted, err
	}
	next := int64(0)
	if retry {
		next, _ = RetryAt(c.Attempts, p.now().UnixMilli())
	}
	// A short context of its own: the outcome is recorded even if ctx is
	// about to end.
	mctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), controlTimeout)
	defer cancel()
	if _, merr := p.store.MarkFailed(mctx, c, result, next, p.now().UnixMilli()); merr != nil {
		err = errors.Join(err, fmt.Errorf("record the failure: %w", merr))
	}
	return result, err
}

// send does the push of c: the result, whether a failure is tried again,
// and the error behind it.
func (p *Pusher) send(ctx context.Context, c *Claim) (string, bool, error) {
	size := c.File.Size
	switch {
	case !c.File.Eligible || size <= 0 || size > MaxCipherSize:
		return pushBadSize, false, fmt.Errorf("size %d, eligible %v", size, c.File.Eligible)
	case p.cap != nil && p.cap() > 0 && size > p.cap():
		return pushTooLarge, false, fmt.Errorf("larger than the node's cache %d", p.cap())
	}
	f := &c.File
	if !f.HasKey() {
		sec, err := NewSecrets()
		if err != nil {
			return pushStoreError, true, err
		}
		// The secrets set first win: one key and token whatever races.
		if f, err = p.store.SetKey(ctx, c.File.RowId, sec); err != nil {
			return pushStoreError, true, err
		}
		if f == nil || !f.HasKey() {
			return pushStoreError, false, errors.New("the file row is gone")
		}
	}
	enc, err := NewEncrypter(f.Secrets.Key, f.Secrets.IV)
	if err != nil {
		return pushStoreError, false, err
	}

	// The whole push is bounded: at the rate it takes size/rate.
	bound := time.Duration(float64(size)/float64(p.set.PushBytesPerSecond)*float64(time.Second)) + pushSlack
	pctx, cancel := context.WithTimeout(ctx, bound)
	defer cancel() // also ends the upload if it is not closed
	stream, err := p.node.Put(pctx, f.Secrets.Token, size)
	if err != nil {
		return nodeResult(err), true, err
	}

	loc := Location(f.Key)
	hasher := NewHasher()
	cipherSum := sha256.New()
	for off := int64(0); off < size; off += PushChunkSize {
		want := min(int64(PushChunkSize), size-off)
		b, err := p.reader.ReadPart(pctx, loc, off, PushChunkSize)
		if err != nil {
			return pushReadError, true, err
		}
		if int64(len(b)) != want {
			return pushShortRead, true, fmt.Errorf("%d bytes at %d, want %d", len(b), off, want)
		}
		_, _ = hasher.Write(b) // never fails
		enc.XORKeyStream(b, b)
		cipherSum.Write(b)
		if err = p.limiter.WaitN(pctx, len(b)); err != nil {
			return pushNodeError, true, err
		}
		if err = stream.Send(b); err != nil {
			return nodeResult(err), true, err
		}
		metricPushBytes.Add(float64(len(b)))
		if now := p.now().UnixMilli(); c.LeaseUntil-now < pushLease.Milliseconds()/2 {
			ok, err := p.store.ExtendLease(pctx, c, pushLease, now)
			if err != nil {
				return pushStoreError, true, err
			}
			if !ok {
				return pushLeaseLost, false, errors.New("another pusher holds the copy")
			}
		}
	}
	gotSize, gotSum, err := stream.Close()
	if err != nil {
		return nodeResult(err), true, err
	}
	if gotSize != size || !bytes.Equal(gotSum, cipherSum.Sum(nil)) {
		return pushMismatch, true, fmt.Errorf("the node stored %d bytes with another SHA-256", gotSize)
	}
	ok, err := p.store.MarkReady(ctx, c, hasher.Sum(), p.now().UnixMilli())
	if err != nil {
		return pushStoreError, true, err
	}
	if !ok {
		return pushLeaseLost, false, errors.New("another pusher holds the copy")
	}
	if p.onReady != nil {
		p.onReady(f.Key)
	}
	return pushOK, false, nil
}

// nodeResult names a failure of the node's Put.
func nodeResult(err error) string {
	if status.Code(err) == codes.ResourceExhausted {
		return pushNodeFull
	}
	return pushNodeError
}
