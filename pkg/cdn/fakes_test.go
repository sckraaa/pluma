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
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	pb "github.com/teamgram/teamgram-server/app/service/cdn/cdn"
	"github.com/teamgram/teamgram-server/pkg/mtserver/codec"
)

const testDc = 201

// testSettings are the settings of an enabled CDN for the tests.
func testSettings(t *testing.T) *Settings {
	t.Helper()
	var secret codec.Secret
	if _, err := rand.Read(secret[:]); err != nil {
		t.Fatal(err)
	}
	return &Settings{
		DcId:               testDc,
		DcAddress:          "127.0.0.1:1",
		DcSecret:           secret,
		MinFileSize:        DefaultMinFileSize,
		PushBytesPerSecond: 1 << 30,
		PushConcurrency:    1,
		MonthlyEgressLimit: 1 << 40,
		ReuploadWait:       2 * time.Second,
	}
}

// clock is a time the tests move by hand.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)} }

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// randomBytes returns n random bytes.
func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

// fakeFiles is a Sizer and a FileReader over files in memory, keyed as
// cdn_files keys them. A reader past the end, or of a file marked short,
// answers no bytes, as dfs does when it fails to read.
type fakeFiles struct {
	mu      sync.Mutex
	files   map[FileKey][]byte
	short   map[FileKey]bool
	sizeErr error
	reads   int
}

func newFakeFiles() *fakeFiles {
	return &fakeFiles{files: map[FileKey][]byte{}, short: map[FileKey]bool{}}
}

func (f *fakeFiles) add(key FileKey, b []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[key] = b
}

func (f *fakeFiles) FileSize(_ context.Context, loc *mtproto.InputFileLocation) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sizeErr != nil {
		return 0, f.sizeErr
	}
	key, _ := Key(loc)
	b, ok := f.files[key]
	if !ok {
		return 0, ErrNoFile
	}
	return int64(len(b)), nil
}

func (f *fakeFiles) ReadPart(_ context.Context, loc *mtproto.InputFileLocation, offset int64, limit int32) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	key, ok := Key(loc)
	if !ok {
		return nil, errors.New("not a CDN location")
	}
	b := f.files[key]
	if f.short[key] && offset > 0 || offset >= int64(len(b)) {
		return []byte{}, nil
	}
	return bytes.Clone(b[offset:min(int64(len(b)), offset+int64(limit))]), nil
}

// fakeNode is a CDN node in memory.
type fakeNode struct {
	mu        sync.Mutex
	status    *pb.StatusResponse
	statusErr error
	keys      [][]byte // SetRequestTokenKeys: current, previous
	keyCalls  int
	files     map[string][]byte
	putErr    error // Put fails with it
	corrupt   bool  // the node stores other bytes than it got
	puts      int
	deleted   [][]byte
}

func newFakeNode(dc int32) *fakeNode {
	return &fakeNode{
		status: &pb.StatusResponse{
			DcId:         dc,
			PublicKeyPem: "-----BEGIN RSA PUBLIC KEY-----\nMAoCAwEAAQIDAQAB\n-----END RSA PUBLIC KEY-----\n",
			Fingerprint:  0x1234567890,
			BootId:       []byte("boot-0123456789a"),
			CacheCap:     1 << 40,
			EgressMonth:  "2026-11",
		},
		files: map[string][]byte{},
	}
}

func (n *fakeNode) Status(context.Context) (*pb.StatusResponse, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.statusErr != nil {
		return nil, n.statusErr
	}
	st := proto.Clone(n.status).(*pb.StatusResponse)
	st.RequestTokenKeysSet = n.keys != nil
	st.CacheFiles = int64(len(n.files))
	return st, nil
}

func (n *fakeNode) SetRequestTokenKeys(_ context.Context, current, previous []byte) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.keys = [][]byte{bytes.Clone(current), bytes.Clone(previous)}
	n.keyCalls++
	return nil
}

// restart forgets the keys, as a node that started again.
func (n *fakeNode) restart() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.keys = nil
	n.status.BootId = []byte("boot-fedcba98765")
}

func (n *fakeNode) Put(_ context.Context, token []byte, size int64) (PutStream, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.puts++
	if n.putErr != nil {
		return nil, n.putErr
	}
	return &fakePut{node: n, token: bytes.Clone(token), size: size}, nil
}

func (n *fakeNode) Delete(_ context.Context, token []byte) (bool, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	_, ok := n.files[string(token)]
	delete(n.files, string(token))
	n.deleted = append(n.deleted, bytes.Clone(token))
	return ok, nil
}

func (n *fakeNode) Inventory(_ context.Context, fn func(token []byte, size int64) error) error {
	n.mu.Lock()
	files := map[string]int64{}
	for k, v := range n.files {
		files[k] = int64(len(v))
	}
	n.mu.Unlock()
	for k, size := range files {
		if err := fn([]byte(k), size); err != nil {
			return err
		}
	}
	return nil
}

func (n *fakeNode) file(token []byte) []byte {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.files[string(token)]
}

type fakePut struct {
	node  *fakeNode
	token []byte
	size  int64
	buf   []byte
}

func (p *fakePut) Send(chunk []byte) error {
	if len(chunk) > PushChunkSize {
		return status.Error(codes.InvalidArgument, "chunk over 1 MiB")
	}
	p.buf = append(p.buf, chunk...)
	if int64(len(p.buf)) > p.size {
		return status.Error(codes.InvalidArgument, "more bytes than the size")
	}
	return nil
}

func (p *fakePut) Close() (int64, []byte, error) {
	if int64(len(p.buf)) != p.size {
		return 0, nil, status.Error(codes.InvalidArgument, "fewer bytes than the size")
	}
	p.node.mu.Lock()
	defer p.node.mu.Unlock()
	stored := bytes.Clone(p.buf)
	if p.node.corrupt {
		stored[0] ^= 1
	}
	p.node.files[string(p.token)] = stored
	sum := sha256.Sum256(stored)
	return int64(len(stored)), sum[:], nil
}
