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

package session

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"

	"github.com/teamgram/teamgram-server/app/service/cdn/internal/store"
	"github.com/teamgram/teamgram-server/pkg/cdn"
	"github.com/teamgram/teamgram-server/pkg/mtserver/handshake"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newClock() *clock { return &clock{t: time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)} }

func randBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

// memFiles is a Files over a map.
type memFiles map[string][]byte

func (m memFiles) ReadAt(token []byte, offset int64, limit int) ([]byte, int64, bool, error) {
	b, ok := m[string(token)]
	if !ok {
		return nil, 0, false, store.ErrNotFound
	}
	if offset >= int64(len(b)) {
		return nil, int64(len(b)), false, nil
	}
	end := min(offset+int64(limit), int64(len(b)))
	return b[offset:end], int64(len(b)), false, nil
}

func TestCheckFileRange(t *testing.T) {
	for _, tc := range []struct {
		offset int64
		limit  int32
		want   error
	}{
		{0, 131072, nil},
		{1 << 20, 1 << 20, nil},
		{(1 << 20) - 4096, 4096, nil},
		{3 << 20, 524288, nil},
		{0, 0, mtproto.ErrLimitInvalid},
		{0, 2 << 20, mtproto.ErrLimitInvalid},
		{-4096, 4096, mtproto.ErrOffsetInvalid},
		{1024, 4096, mtproto.ErrOffsetInvalid},    // precise is not for the CDN
		{0, 1024, mtproto.ErrLimitInvalid},        // nor 1 KB limits
		{0, 12288, mtproto.ErrLimitInvalid},       // 1 MB is not divisible by it
		{786432, 524288, mtproto.ErrLimitInvalid}, // over the 1 MB boundary
	} {
		if err := checkFileRange(tc.offset, tc.limit); !errors.Is(err, tc.want) {
			t.Errorf("checkFileRange(%d, %d) = %v, want %v", tc.offset, tc.limit, err, tc.want)
		}
	}
}

func newServer(t *testing.T, files Files, c *clock) (*Server, *RequestTokens) {
	t.Helper()
	tokens := &RequestTokens{}
	s, err := New(NewKeyStore(10, c.Now), files, tokens, c.Now)
	if err != nil {
		t.Fatal(err)
	}
	return s, tokens
}

func encode(t *testing.T, obj mtproto.TLObject) []byte {
	t.Helper()
	x := mtproto.NewEncodeBuf(256)
	if err := obj.Encode(x, 233); err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), x.GetBuf()...)
}

func getCdnFile(t *testing.T, token []byte, offset int64, limit int32) []byte {
	t.Helper()
	return encode(t, &mtproto.TLUploadGetCdnFile{Constructor: mtproto.CRC32_upload_getCdnFile_395f69da, FileToken: token, Offset_INT64: offset, Limit: limit})
}

// wrapped puts query in invokeWithLayer(initConnection(...)) as the clients
// send their first request on a connection.
func wrapped(t *testing.T, query []byte) []byte {
	t.Helper()
	init := encode(t, &mtproto.TLInitConnection{
		Constructor: mtproto.CRC32_initConnection_c1cd5ea9, ApiId: 1, DeviceModel: "n/a", SystemVersion: "n/a",
		AppVersion: "1", SystemLangCode: "en", LangCode: "en", Query: query,
	})
	return encode(t, &mtproto.TLInvokeWithLayer{Constructor: mtproto.CRC32_invokeWithLayer, Layer: 233, Query: init})
}

func rpcError(res mtproto.TLObject) string {
	if e, ok := res.(*mtproto.TLRpcError); ok {
		return e.GetErrorMessage()
	}
	return ""
}

func TestInvokeGetCdnFile(t *testing.T) {
	c := newClock()
	tok := randBytes(t, cdn.TokenSize)
	body := randBytes(t, 3<<20+5)
	s, tokens := newServer(t, memFiles{string(tok): body}, c)

	res := s.invoke(wrapped(t, getCdnFile(t, tok, 1<<20, 1<<20)))
	f, ok := res.(*mtproto.TLUploadCdnFile)
	if !ok || !bytes.Equal(f.GetBytes(), body[1<<20:2<<20]) {
		t.Fatalf("getCdnFile: %T", res)
	}
	// The last part is short; past the end is empty.
	res = s.invoke(getCdnFile(t, tok, 3<<20, 4096))
	if f, ok := res.(*mtproto.TLUploadCdnFile); !ok || !bytes.Equal(f.GetBytes(), body[3<<20:]) {
		t.Fatalf("last part: %T", res)
	}
	res = s.invoke(getCdnFile(t, tok, 4<<20, 4096))
	if f, ok := res.(*mtproto.TLUploadCdnFile); !ok || len(f.GetBytes()) != 0 {
		t.Fatalf("past the end: %T", res)
	}
	// Other wrappers of the clients.
	after := encode(t, &mtproto.TLInvokeAfterMsg{Constructor: mtproto.CRC32_invokeAfterMsg, MsgId: 4, Query: getCdnFile(t, tok, 0, 4096)})
	res = s.invoke(encode(t, &mtproto.TLInvokeWithoutUpdates{Constructor: mtproto.CRC32_invokeWithoutUpdates, Query: after}))
	if _, ok := res.(*mtproto.TLUploadCdnFile); !ok {
		t.Fatalf("invokeWithoutUpdates(invokeAfterMsg(...)): %T", res)
	}

	// A range against the rules.
	if res = s.invoke(getCdnFile(t, tok, 786432, 524288)); rpcError(res) != "LIMIT_INVALID" {
		t.Fatalf("range over 1 MB: %v", res)
	}

	// A file the node does not have, before the home sent a key: back to
	// the home at once.
	missing := randBytes(t, cdn.TokenSize)
	if res = s.invoke(getCdnFile(t, missing, 0, 4096)); rpcError(res) != "FILE_TOKEN_INVALID" {
		t.Fatalf("missing file without a key: %v", res)
	}
	// With the key: a request_token the home accepts for that file only.
	key := randBytes(t, cdn.RtKeySize)
	tokens.Set(key)
	res = s.invoke(getCdnFile(t, missing, 0, 4096))
	r, ok := res.(*mtproto.TLUploadCdnFileReuploadNeeded)
	if !ok {
		t.Fatalf("missing file: %T %v", res, res)
	}
	if !cdn.VerifyRequestToken([][]byte{key}, missing, r.GetRequestToken(), c.Now()) {
		t.Fatal("the request_token does not verify at the home")
	}
	if cdn.VerifyRequestToken([][]byte{key}, tok, r.GetRequestToken(), c.Now()) {
		t.Fatal("the request_token verifies for another file")
	}
	// Not a token of the home.
	if res = s.invoke(getCdnFile(t, []byte("short"), 0, 4096)); rpcError(res) != "FILE_TOKEN_INVALID" {
		t.Fatalf("short token: %v", res)
	}
}

func TestInvokeOtherMethods(t *testing.T) {
	s, _ := newServer(t, memFiles{}, newClock())
	for name, q := range map[string][]byte{
		"help.getConfig":          wrapped(t, encode(t, &mtproto.TLHelpGetConfig{Constructor: mtproto.CRC32_help_getConfig})),
		"upload.getFile":          encode(t, &mtproto.TLUploadGetFile{Constructor: mtproto.CRC32_upload_getFile_be5335be, Location: mtproto.MakeTLInputDocumentFileLocation(&mtproto.InputFileLocation{}).To_InputFileLocation(), Limit: 4096}),
		"upload.reuploadCdnFile":  encode(t, &mtproto.TLUploadReuploadCdnFile{Constructor: mtproto.CRC32_upload_reuploadCdnFile}),
		"undecodable":             {0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
		"invokeWithLayer of junk": encode(t, &mtproto.TLInvokeWithLayer{Constructor: mtproto.CRC32_invokeWithLayer, Layer: 233, Query: []byte{1, 2, 3, 4}}),
	} {
		if res := s.invoke(q); rpcError(res) != "CDN_METHOD_INVALID" {
			t.Errorf("%s: %v", name, res)
		}
	}
}

func TestSalts(t *testing.T) {
	c := newClock()
	s, _ := newServer(t, memFiles{}, c)
	k := &AuthKey{ID: 42, FirstSalt: 777, CreatedAt: c.Now().Unix()}
	if !s.salts.valid(k, 777, c.Now()) {
		t.Fatal("the handshake's salt is refused at once")
	}
	cur := s.salts.current(k, c.Now())
	if !s.salts.valid(k, cur, c.Now()) || s.salts.valid(k, cur+1, c.Now()) {
		t.Fatal("the current salt is refused or another accepted")
	}
	other := &AuthKey{ID: 43}
	if s.salts.current(other, c.Now()) == cur {
		t.Fatal("two keys share a salt")
	}
	c.Add((firstSaltTTL + saltGrace + 1) * time.Second)
	if s.salts.valid(k, 777, c.Now()) {
		t.Fatal("the handshake's salt is accepted after its time")
	}
	// The salt of the previous period is accepted saltGrace into the next.
	start := (c.Now().Unix()/saltPeriod + 1) * saltPeriod
	c.Add(time.Duration(start-c.Now().Unix()) * time.Second)
	prev := s.salts.salt(k.ID, start/saltPeriod-1)
	if !s.salts.valid(k, prev, c.Now()) {
		t.Fatal("the previous salt is refused at the start of a period")
	}
	c.Add((saltGrace + 1) * time.Second)
	if s.salts.valid(k, prev, c.Now()) {
		t.Fatal("the previous salt is accepted after the grace")
	}
}

func TestReceiveWindow(t *testing.T) {
	st := newSessionState(1)
	base := int64(1) << 40
	for i := int64(0); i < receivedWindow; i++ {
		if old, seen, _ := st.receive(base + 4*i); old || seen {
			t.Fatalf("message %d: old %v seen %v", i, old, seen)
		}
	}
	if _, seen, running := st.receive(base + 8); !seen || running {
		t.Fatal("a repeated msg_id is not seen")
	}
	st.start(base + 8)
	if _, _, running := st.receive(base + 8); !running {
		t.Fatal("a repeated msg_id in progress is not running")
	}
	if got := st.stateInfo(base+8, time.Unix(base>>32, 0)); got != 4|8|32 {
		t.Fatalf("state of a request in progress: %d", got)
	}
	st.finish(base + 8)
	if got := st.stateInfo(base+8, time.Unix(base>>32, 0)); got != 2 {
		t.Fatalf("state of an answered request: %d", got)
	}
	if old, _, _ := st.receive(base - 4); !old {
		t.Fatal("a msg_id below a full window is not old")
	}
	if seq := st.seqNo(true); seq != 1 {
		t.Fatalf("first content seqno %d", seq)
	}
	if seq := st.seqNo(false); seq != 2 {
		t.Fatalf("seqno of a service message after one content message: %d", seq)
	}
}

func TestMsgIDs(t *testing.T) {
	var g msgIDs
	now := time.Now()
	last := int64(0)
	for i := 0; i < 1000; i++ {
		id := g.next(now, i%2 == 0)
		want := int64(1)
		if i%2 != 0 {
			want = 3
		}
		if id%4 != want || id <= last {
			t.Fatalf("msg_id %d after %d (answer %v)", id, last, i%2 == 0)
		}
		last = id
	}
	if last>>32 != now.Unix() && last>>32 != now.Unix()+1 {
		t.Fatalf("msg_id time %d, now %d", last>>32, now.Unix())
	}
}

// TestFirstMsgID: new_session_created names the smallest msg_id of a
// container, not the container's own (larger) one, or the clients would
// send the container's requests again.
func TestFirstMsgID(t *testing.T) {
	ping := encode(t, &mtproto.TLPing{Constructor: mtproto.CRC32_ping, PingId: 1})
	if got := firstMsgID(400, ping); got != 400 {
		t.Fatalf("a single message: %d", got)
	}
	x := mtproto.NewEncodeBuf(256)
	x.Int(int32(mtproto.CRC32_msg_container))
	x.Int(2)
	for _, id := range []int64{104, 100} {
		x.Long(id)
		x.Int(1)
		x.Int(int32(len(ping)))
		x.Bytes(ping)
	}
	if got := firstMsgID(108, x.GetBuf()); got != 100 {
		t.Fatalf("a container of 104 and 100 with id 108: %d", got)
	}
}

func TestKeyStore(t *testing.T) {
	c := newClock()
	ks := NewKeyStore(2, c.Now)
	save := func(id int64, expiresIn int32) error {
		return ks.Save(context.Background(), &handshake.AuthKey{AuthKeyId: id, AuthKey: make([]byte, 256)}, expiresIn)
	}
	if err := save(1, 0); err != nil {
		t.Fatal(err)
	}
	if err := save(1, 0); !errors.Is(err, handshake.ErrKeyIdTaken) {
		t.Fatalf("a taken id: %v", err)
	}
	if err := save(2, 60); err != nil {
		t.Fatal(err)
	}
	ks.Get(1) // 2 is now the least recently used
	if err := save(3, 0); err != nil {
		t.Fatal(err)
	}
	if ks.Get(2) != nil || ks.Get(1) == nil || ks.Get(3) == nil {
		t.Fatal("the least recently used key was not the one dropped")
	}
	if err := save(4, 60); err != nil {
		t.Fatal(err)
	}
	c.Add(61 * time.Second)
	if ks.Get(4) != nil {
		t.Fatal("an expired temporary key is served")
	}
	if ks.Len() != 1 {
		t.Fatalf("%d keys after the expired one went", ks.Len())
	}
}
