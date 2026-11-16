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
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"golang.org/x/time/rate"
)

// Target is where a prepared file must be (task 107, "Подготовка"). The
// home origin only needs the hashes of the plaintext, since it encrypts
// on the fly; a store a CDN serves from by itself (later) also needs the
// cipher uploaded.
type Target interface {
	// ID is cdn_replicas.target_id: TargetHomeOrigin, or 2 and above.
	ID() int32
	// NeedsCiphertext reports whether Put is called with the cipher of
	// each file.
	NeedsCiphertext() bool
	// Put stores the cipher of a file, size bytes read from r, under name
	// (ObjectPath of its id without the leading slash). It returns once
	// the object is stored whole, or an error; r fails if the reading of
	// the file does, and Put must not store what it got then.
	Put(ctx context.Context, name string, r io.Reader, size int64) error
}

// HomeOrigin is the Target of the home origin: preparing a file computes
// its hashes; the cipher is made by the origin at each request.
type HomeOrigin struct{}

var _ Target = HomeOrigin{}

func (HomeOrigin) ID() int32             { return TargetHomeOrigin }
func (HomeOrigin) NeedsCiphertext() bool { return false }

// Put is never called: the home origin keeps no copy.
func (HomeOrigin) Put(context.Context, string, io.Reader, int64) error {
	return errors.New("cdn: the home origin keeps no copy of a file")
}

// targetOf returns the Target of delivery d.
func targetOf(d *Delivery) (Target, error) {
	if d.Source == SourceHomeOrigin && d.Target() == TargetHomeOrigin {
		return HomeOrigin{}, nil
	}
	return nil, fmt.Errorf("cdn: delivery %d: no target for source %q", d.ID, d.Source)
}

// Limits of the preparation (task 107).
const (
	// prepareLease is how long a claim holds a preparation; it is renewed
	// past half of it while the preparation goes on.
	prepareLease = 10 * time.Minute
	// prepareIdle is how long a preparer waits when the queue is empty,
	// unless woken.
	prepareIdle = 5 * time.Second
	// prepareSlack is added to the time a preparation should take at the
	// rate for the bound of the whole of it.
	prepareSlack = 2 * time.Minute
	// markTimeout bounds the record of a failed preparation.
	markTimeout = 10 * time.Second
)

// Results of preparations (cdn_replicas.last_error, pluma_cdn_prepare_total).
const (
	prepareOK          = "ok"
	prepareShortRead   = "short_read"
	prepareReadError   = "read_error"
	preparePutError    = "put_error"
	prepareBadSize     = "bad_size"
	prepareTimeout     = "timeout"
	prepareLeaseLost   = "lease_lost"
	prepareStoreError  = "store_error"
	prepareInterrupted = "interrupted"
)

// Preparer prepares the queued files of a target one at a time (the queue
// of cdn_replicas, by lease, as in task 102): it makes the key and IV of a
// file before reading it, reads it from dfs a ReadChunkSize part at a time
// at PrepareRateMbit, hashes the plaintext by HashPartSize and, for a
// target that needs it, uploads the cipher. The preparation is ready once
// the hashes are written. Several processes may run preparers of one
// target: each preparation is claimed by one of them.
type Preparer struct {
	store   Store
	target  Target
	reader  FileReader
	rate    int64 // bytes a second
	onReady func(FileKey)
	limiter *rate.Limiter
	now     func() time.Time
	idle    time.Duration
	wake    chan struct{}
}

// NewPreparer returns the preparer of target with the limits of set;
// onReady (may be nil) is called with the key of each file made ready.
func NewPreparer(store Store, target Target, reader FileReader, set *Settings, onReady func(FileKey)) *Preparer {
	return &Preparer{
		store:   store,
		target:  target,
		reader:  reader,
		rate:    set.PrepareBytesPerSecond,
		onReady: onReady,
		limiter: rate.NewLimiter(rate.Limit(set.PrepareBytesPerSecond), ReadChunkSize),
		now:     time.Now,
		idle:    prepareIdle,
		wake:    make(chan struct{}, 1),
	}
}

// Wake makes an idle preparer look at the queue now: a file was queued.
func (p *Preparer) Wake() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// Run prepares files until ctx is done.
func (p *Preparer) Run(ctx context.Context) {
	for ctx.Err() == nil {
		if !p.Next(ctx) {
			select {
			case <-ctx.Done():
			case <-p.wake:
			case <-time.After(p.idle):
			}
		}
	}
}

// Next claims the next preparation and does it; false if there was none.
func (p *Preparer) Next(ctx context.Context) bool {
	c, err := p.store.Claim(ctx, p.target.ID(), prepareLease, p.now().UnixMilli())
	if err != nil {
		if ctx.Err() == nil {
			logx.WithContext(ctx).Errorf("cdn: claim a preparation for target %d: %v", p.target.ID(), err)
		}
		return false
	}
	if c == nil {
		return false
	}
	result, err := p.prepare(ctx, c)
	metricPrepares.Inc(result)
	if err != nil && result != prepareInterrupted {
		logx.WithContext(ctx).Errorf("cdn: preparation of file row %d (%d bytes) for target %d, attempt %d: %s: %v",
			c.File.RowId, c.File.Size, c.TargetId, c.Attempts, result, err)
	}
	return true
}

// prepare does the preparation of c and records its outcome; it returns
// the result and the error behind a failure.
func (p *Preparer) prepare(ctx context.Context, c *Claim) (string, error) {
	result, retry, err := p.run(ctx, c)
	switch {
	case result == prepareOK || result == prepareLeaseLost:
		return result, err
	case ctx.Err() != nil:
		// Stopping: the lease runs out and the preparation is claimed
		// again.
		return prepareInterrupted, err
	}
	next := int64(0)
	if retry {
		next, _ = RetryAt(c.Attempts, p.now().UnixMilli())
	}
	// A context of its own: the outcome is recorded even if ctx is about
	// to end.
	mctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), markTimeout)
	defer cancel()
	if _, merr := p.store.MarkFailed(mctx, c, result, next, p.now().UnixMilli()); merr != nil {
		err = errors.Join(err, fmt.Errorf("record the failure: %w", merr))
	}
	return result, err
}

// run does the preparation of c: the result, whether a failure is tried
// again, and the error behind it.
func (p *Preparer) run(ctx context.Context, c *Claim) (string, bool, error) {
	size := c.File.Size
	if !c.File.Eligible || size <= 0 || size > MaxCipherSize {
		return prepareBadSize, false, fmt.Errorf("size %d, eligible %v", size, c.File.Eligible)
	}
	loc := Location(c.File.Key)
	if loc == nil {
		return prepareBadSize, false, fmt.Errorf("no location for kind %d", c.File.Key.Kind)
	}
	f := &c.File
	if !f.HasKey() {
		// The key and IV are made before the file is read (task 102): the
		// secrets set first win, so a file has one key and id whatever
		// races.
		sec, err := NewSecrets()
		if err != nil {
			return prepareStoreError, true, err
		}
		if f, err = p.store.SetKey(ctx, c.File.RowId, sec); err != nil {
			return prepareStoreError, true, err
		}
		if f == nil || !f.HasKey() {
			return prepareStoreError, false, errors.New("the file row is gone")
		}
	}

	// The whole preparation is bounded: at the rate it takes size/rate.
	bound := time.Duration(float64(size)/float64(p.rate)*float64(time.Second)) + prepareSlack
	pctx, cancel := context.WithTimeout(ctx, bound)
	defer cancel()

	var up *upload
	if p.target.NeedsCiphertext() {
		var err error
		if up, err = p.startUpload(pctx, f, size); err != nil {
			return prepareStoreError, false, err
		}
	}
	// fail ends an upload in progress with the failure, so that the target
	// stores nothing.
	fail := func(result string, retry bool, err error) (string, bool, error) {
		if up != nil {
			up.abort(err)
		}
		return result, retry, err
	}

	hasher := NewHasher()
	for off := int64(0); off < size; off += ReadChunkSize {
		want := min(int64(ReadChunkSize), size-off)
		// WaitN fails at once when the wait would pass the bound.
		if err := p.limiter.WaitN(pctx, int(want)); err != nil {
			return fail(prepareTimeout, true, err)
		}
		b, err := p.reader.ReadPart(pctx, loc, off, ReadChunkSize)
		if err != nil {
			return fail(prepareReadError, true, err)
		}
		// dfs answers a part it failed to read with no bytes (reader.go).
		if int64(len(b)) != want {
			return fail(prepareShortRead, true, fmt.Errorf("%d bytes at %d, want %d", len(b), off, want))
		}
		_, _ = hasher.Write(b) // never fails
		metricPrepareBytes.Add(float64(len(b)))
		if up != nil {
			if err = DecryptAt(f.Secrets.Key, f.Secrets.IV, off, b); err != nil { // encryption is the same operation
				return fail(prepareStoreError, false, err)
			}
			if err = up.write(b); err != nil {
				return fail(preparePutError, true, err)
			}
		}
		if now := p.now().UnixMilli(); c.LeaseUntil-now < prepareLease.Milliseconds()/2 {
			ok, err := p.store.ExtendLease(pctx, c, prepareLease, now)
			if err != nil {
				return fail(prepareStoreError, true, err)
			}
			if !ok {
				return fail(prepareLeaseLost, false, errors.New("another preparer holds the preparation"))
			}
		}
	}
	if up != nil {
		if err := up.finish(); err != nil {
			return preparePutError, true, err
		}
	}
	ok, err := p.store.MarkReady(ctx, c, hasher.Sum(), p.now().UnixMilli())
	if err != nil {
		return prepareStoreError, true, err
	}
	if !ok {
		return prepareLeaseLost, false, errors.New("another preparer holds the preparation")
	}
	if p.onReady != nil {
		p.onReady(f.Key)
	}
	return prepareOK, false, nil
}

// upload is the cipher of a file going to Target.Put through a pipe.
type upload struct {
	w    *io.PipeWriter
	done chan error
	// ended and err are the outcome of the Put once received from done.
	ended bool
	err   error
}

// startUpload starts the Put of the cipher of f.
func (p *Preparer) startUpload(ctx context.Context, f *File, size int64) (*upload, error) {
	if !f.Secrets.Valid() {
		return nil, errSecrets
	}
	r, w := io.Pipe()
	up := &upload{w: w, done: make(chan error, 1)}
	name := ObjectPath(f.Secrets.Token)[1:]
	go func() {
		err := p.target.Put(ctx, name, r, size)
		// A Put that returned early fails the writes that follow.
		_ = r.CloseWithError(errors.Join(err, io.ErrClosedPipe))
		up.done <- err
	}()
	return up, nil
}

// write sends the next bytes of the cipher; an error is the Put's when it
// stopped reading.
func (u *upload) write(b []byte) error {
	if _, err := u.w.Write(b); err != nil {
		if perr := u.wait(); perr != nil {
			return perr
		}
		return err
	}
	return nil
}

// finish ends the cipher and waits for the Put.
func (u *upload) finish() error {
	_ = u.w.Close()
	return u.wait()
}

// abort ends the cipher with err and waits for the Put.
func (u *upload) abort(err error) {
	_ = u.w.CloseWithError(err)
	_ = u.wait()
}

// wait returns the outcome of the Put once it returned.
func (u *upload) wait() error {
	if !u.ended {
		u.err, u.ended = <-u.done, true
	}
	return u.err
}
