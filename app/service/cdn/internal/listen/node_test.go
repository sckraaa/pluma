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

package listen

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"

	"github.com/teamgram/teamgram-server/app/service/cdn/internal/nodekey"
	"github.com/teamgram/teamgram-server/app/service/cdn/internal/session"
	"github.com/teamgram/teamgram-server/app/service/cdn/internal/store"
	"github.com/teamgram/teamgram-server/pkg/cdn"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"github.com/teamgram/teamgram-server/pkg/mtserver/codec"
	"github.com/teamgram/teamgram-server/pkg/mtserver/handshake"
	"github.com/teamgram/teamgram-server/pkg/serverkey"
)

// cdnDC is the DC number the tests' clients name in the key exchange and
// the obfuscated header, as the clients do for a CDN DC.
const cdnDC = 201

// obfTransport is the client side of a connection made with a "dd" secret:
// the obfuscated header with keys SHA256(key + secret) and the padded
// intermediate transport, written after tdesktop's TcpConnection
// (connection_tcp.cpp:160-248) with the standard library's AES-CTR, so that
// it shares no code with the node under test.
type obfTransport struct {
	conn net.Conn
	r    *bufio.Reader
	wmu  sync.Mutex
	send cipher.Stream
	recv cipher.Stream
}

func dialObf(t *testing.T, addr string, secret []byte, dc int16) *obfTransport {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, 64)
	for {
		if _, err := rand.Read(nonce); err != nil {
			t.Fatal(err)
		}
		// As the clients do, a header that reads as another transport is
		// drawn again (Android Connection.cpp:524-527).
		first := binary.LittleEndian.Uint32(nonce)
		if nonce[0] != 0xef && binary.LittleEndian.Uint32(nonce[4:]) != 0 &&
			first != 0x44414548 && first != 0x54534f50 && first != 0x20544547 && first != 0x4954504f &&
			first != 0xeeeeeeee && first != 0xdddddddd && first != 0x02010316 {
			break
		}
	}
	prepareKey := func(k []byte) []byte {
		if secret == nil {
			return append([]byte(nil), k...)
		}
		h := sha256.Sum256(append(append([]byte(nil), k...), secret...))
		return h[:]
	}
	stream := func(key, iv []byte) cipher.Stream {
		b, err := aes.NewCipher(key)
		if err != nil {
			t.Fatal(err)
		}
		return cipher.NewCTR(b, iv)
	}
	binary.LittleEndian.PutUint32(nonce[56:], 0xdddddddd)
	binary.LittleEndian.PutUint16(nonce[60:], uint16(dc))
	reversed := make([]byte, 48)
	for i := range reversed {
		reversed[i] = nonce[55-i]
	}
	o := &obfTransport{
		conn: conn,
		r:    bufio.NewReaderSize(conn, 1<<20),
		send: stream(prepareKey(nonce[8:40]), nonce[40:56]),
		recv: stream(prepareKey(reversed[:32]), reversed[32:48]),
	}
	encrypted := make([]byte, 64)
	o.send.XORKeyStream(encrypted, nonce)
	header := append(append([]byte(nil), nonce[:56]...), encrypted[56:]...)
	if _, err := conn.Write(header); err != nil {
		t.Fatal(err)
	}
	return o
}

func (o *obfTransport) WritePacket(p []byte) error {
	var pad [1]byte
	_, _ = rand.Read(pad[:])
	n := int(pad[0] % 16)
	b := make([]byte, 4+len(p)+n)
	binary.LittleEndian.PutUint32(b, uint32(len(p)+n))
	copy(b[4:], p)
	_, _ = rand.Read(b[4+len(p):])
	o.wmu.Lock()
	defer o.wmu.Unlock()
	o.send.XORKeyStream(b, b)
	_, err := o.conn.Write(b)
	return err
}

func (o *obfTransport) ReadPacket() ([]byte, error) {
	for {
		var hdr [4]byte
		if _, err := io.ReadFull(o.r, hdr[:]); err != nil {
			return nil, err
		}
		o.recv.XORKeyStream(hdr[:], hdr[:])
		n := binary.LittleEndian.Uint32(hdr[:])
		if n&0x80000000 != 0 {
			continue // a quick ACK
		}
		if n > 16<<20 {
			return nil, errors.New("bad length")
		}
		p := make([]byte, n)
		if _, err := io.ReadFull(o.r, p); err != nil {
			return nil, err
		}
		o.recv.XORKeyStream(p, p)
		if n == 4 {
			return nil, &mtclient.TransportError{Code: int32(binary.LittleEndian.Uint32(p))}
		}
		if n >= 24 && binary.LittleEndian.Uint64(p) != 0 {
			// The padding after the encrypted data.
			p = p[:24+(n-24)/16*16]
		}
		return p, nil
	}
}

func (o *obfTransport) Close() error                  { return o.conn.Close() }
func (o *obfTransport) SetDeadline(d time.Time) error { return o.conn.SetDeadline(d) }

// node is a CDN node on a loopback port.
type node struct {
	addr   string
	secret []byte // the 16 bytes after 0xdd
	pub    *rsa.PublicKey
	files  *store.Store
	tokens *session.RequestTokens
	srv    *Server
}

func startNode(t *testing.T, maxConns int) *node {
	t.Helper()
	dir := t.TempDir()
	key, err := nodekey.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	files, err := store.Open(store.Config{Dir: dir, CacheBytes: 64 << 20, IdleTTL: time.Hour,
		DiskFree: func(string) (int64, error) { return 1 << 40, nil }})
	if err != nil {
		t.Fatal(err)
	}
	hs, err := handshake.New([]*serverkey.Key{key.Key})
	if err != nil {
		t.Fatal(err)
	}
	tokens := &session.RequestTokens{}
	sess, err := session.New(session.NewKeyStore(100, nil), files, tokens, nil)
	if err != nil {
		t.Fatal(err)
	}
	var secret codec.Secret
	if _, err := rand.Read(secret[:]); err != nil {
		t.Fatal(err)
	}
	srv, err := New(Config{Secret: secret, MaxConnsPerIP: maxConns, KeyExchangesPerIPMinute: 300, MaxInFlight: 4,
		IdleTimeout: time.Minute, HandshakeWorkers: 2}, hs, sess)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	t.Cleanup(func() {
		_ = srv.Close()
		if err := <-done; err != nil {
			t.Errorf("Serve: %v", err)
		}
	})
	return &node{addr: ln.Addr().String(), secret: secret[:], pub: &key.Private.PublicKey, files: files, tokens: tokens, srv: srv}
}

// putFile stores b on the node under a new token.
func (n *node) putFile(t *testing.T, b []byte) []byte {
	t.Helper()
	tok := make([]byte, cdn.TokenSize)
	if _, err := rand.Read(tok); err != nil {
		t.Fatal(err)
	}
	w, err := n.files.Put(tok, int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(b); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Commit(); err != nil {
		t.Fatal(err)
	}
	return tok
}

// client connects with the node's secret and makes a key as opts says.
func (n *node) client(t *testing.T, opts mtclient.KeyOptions) *mtclient.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tr := dialObf(t, n.addr, n.secret, cdnDC)
	opts.DC = cdnDC
	key, err := mtclient.CreateAuthKey(ctx, tr, n.pub, opts)
	if err != nil {
		_ = tr.Close()
		t.Fatalf("key exchange: %v", err)
	}
	c := mtclient.New(tr, key, mtclient.Config{DeviceModel: "n/a", SystemVersion: "n/a"})
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func getCdnFileReq(token []byte, offset int64, limit int32) *mtproto.TLUploadGetCdnFile {
	return &mtproto.TLUploadGetCdnFile{Constructor: mtproto.CRC32_upload_getCdnFile_395f69da, FileToken: token, Offset_INT64: offset, Limit: limit}
}

func encodeReq(t *testing.T, obj mtproto.TLObject) []byte {
	t.Helper()
	x := mtproto.NewEncodeBuf(128)
	if err := obj.Encode(x, mtclient.DefaultLayer); err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), x.GetBuf()...)
}

func invoke(t *testing.T, c *mtclient.Client, req mtproto.TLObject) (mtproto.TLObject, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return c.Invoke(ctx, req)
}

// download reads the whole file by parts of limit bytes, the first request
// wrapped in invokeWithLayer(initConnection(...)) as clients send it.
func download(t *testing.T, c *mtclient.Client, token []byte, limit int32) []byte {
	t.Helper()
	var out []byte
	for offset := int64(0); ; offset += int64(limit) {
		var req mtproto.TLObject = getCdnFileReq(token, offset, limit)
		if offset == 0 {
			req = c.WrapInit(encodeReq(t, req))
		}
		res, err := invoke(t, c, req)
		if err != nil {
			t.Fatalf("getCdnFile(%d, %d): %v", offset, limit, err)
		}
		f, ok := res.(*mtproto.TLUploadCdnFile)
		if !ok {
			t.Fatalf("getCdnFile(%d, %d): %T", offset, limit, res)
		}
		if len(f.GetBytes()) == 0 {
			return out
		}
		out = append(out, f.GetBytes()...)
	}
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

// TestDownload: a client with a permanent key (as Android and iOS make on a
// CDN DC) and one with a temporary unbound key (as tdesktop makes) download
// a file by 128 KB and by 1 MB.
func TestDownload(t *testing.T) {
	n := startNode(t, 32)
	body := randomBytes(t, 3<<20+5)
	tok := n.putFile(t, body)

	for name, opts := range map[string]mtclient.KeyOptions{
		"permanent": {},
		"temporary": {Temp: true, ExpiresIn: 86400},
	} {
		c := n.client(t, opts)
		if got := download(t, c, tok, 128<<10); !bytes.Equal(got, body) {
			t.Fatalf("%s key, by 128 KB: %d bytes of %d differ", name, len(got), len(body))
		}
		if got := download(t, c, tok, 1<<20); !bytes.Equal(got, body) {
			t.Fatalf("%s key, by 1 MB: %d bytes of %d differ", name, len(got), len(body))
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := c.Ping(ctx); err != nil {
			t.Fatalf("%s key: ping: %v", name, err)
		}
		cancel()
		if c.Duplicates() != 0 {
			t.Fatalf("%s key: %d messages repeated by the node", name, c.Duplicates())
		}
	}
}

// TestRestricted: the node serves upload.getCdnFile and nothing else, by
// the rules of its range.
func TestRestricted(t *testing.T) {
	n := startNode(t, 32)
	tok := n.putFile(t, randomBytes(t, 2<<20))
	c := n.client(t, mtclient.KeyOptions{})

	query := encodeReq(t, &mtproto.TLHelpGetConfig{Constructor: mtproto.CRC32_help_getConfig})
	if _, err := invoke(t, c, c.WrapInit(query)); !mtclient.IsRPCError(err, "CDN_METHOD_INVALID") {
		t.Fatalf("help.getConfig: %v", err)
	}
	if _, err := invoke(t, c, getCdnFileReq(tok, 786432, 524288)); !mtclient.IsRPCError(err, "LIMIT_INVALID") {
		t.Fatalf("a range over the 1 MB boundary: %v", err)
	}
	if _, err := invoke(t, c, getCdnFileReq(tok, 1024, 4096)); !mtclient.IsRPCError(err, "OFFSET_INVALID") {
		t.Fatalf("an offset of 1 KB: %v", err)
	}
	// The session goes on after errors.
	if _, err := invoke(t, c, getCdnFileReq(tok, 0, 4096)); err != nil {
		t.Fatalf("after the errors: %v", err)
	}
}

// TestReuploadNeeded: a file the node does not have is answered with a
// request_token the home verifies; before the home sent its key, with
// FILE_TOKEN_INVALID.
func TestReuploadNeeded(t *testing.T) {
	n := startNode(t, 32)
	c := n.client(t, mtclient.KeyOptions{Temp: true, ExpiresIn: 3600})
	missing := randomBytes(t, cdn.TokenSize)

	if _, err := invoke(t, c, getCdnFileReq(missing, 0, 131072)); !mtclient.IsRPCError(err, "FILE_TOKEN_INVALID") {
		t.Fatalf("without a request_token key: %v", err)
	}
	key := randomBytes(t, cdn.RtKeySize)
	n.tokens.Set(key)
	res, err := invoke(t, c, getCdnFileReq(missing, 0, 131072))
	if err != nil {
		t.Fatal(err)
	}
	r, ok := res.(*mtproto.TLUploadCdnFileReuploadNeeded)
	if !ok {
		t.Fatalf("a missing file: %T", res)
	}
	if !cdn.VerifyRequestToken([][]byte{key}, missing, r.GetRequestToken(), time.Now()) {
		t.Fatal("the request_token does not verify")
	}
}

// TestUnknownKey: a key the node does not hold (made elsewhere, or lost in
// a restart) gets transport error -404.
func TestUnknownKey(t *testing.T) {
	n := startNode(t, 32)
	key := &mtclient.AuthKey{Key: randomBytes(t, 256)}
	key.ID = mtclient.AuthKeyID(key.Key)
	c := mtclient.New(dialObf(t, n.addr, n.secret, cdnDC), key, mtclient.Config{})
	t.Cleanup(func() { _ = c.Close() })
	if _, err := invoke(t, c, getCdnFileReq(randomBytes(t, cdn.TokenSize), 0, 4096)); !mtclient.IsTransportError(err, -404) {
		t.Fatalf("an unknown key: %v", err)
	}
}

// TestBadSalt: a message with a wrong salt is answered bad_server_salt; the
// client sends it again with the new salt.
func TestBadSalt(t *testing.T) {
	n := startNode(t, 32)
	tok := n.putFile(t, randomBytes(t, 8192))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tr := dialObf(t, n.addr, n.secret, cdnDC)
	key, err := mtclient.CreateAuthKey(ctx, tr, n.pub, mtclient.KeyOptions{DC: cdnDC})
	if err != nil {
		t.Fatal(err)
	}
	key.ServerSalt ^= 0x5a5a
	c := mtclient.New(tr, key, mtclient.Config{})
	t.Cleanup(func() { _ = c.Close() })
	if _, err := invoke(t, c, getCdnFileReq(tok, 0, 4096)); err != nil {
		t.Fatalf("after bad_server_salt: %v", err)
	}
}

// TestSecretRequired: connections without the secret, or with another, are
// closed before any key exchange.
func TestSecretRequired(t *testing.T) {
	n := startNode(t, 32)

	// The intermediate transport: its first packet is shorter than an
	// obfuscated header, and nothing is ever answered.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	plain, err := mtclient.DialIntermediate(ctx, n.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	if _, err := mtclient.CreateAuthKey(ctx, plain, n.pub, mtclient.KeyOptions{DC: cdnDC}); err == nil {
		t.Fatal("a key exchange without the secret")
	}

	// An obfuscated header made with another secret or none: its transport
	// tag decrypts to garbage, and the node closes the connection at once.
	for name, secret := range map[string][]byte{"another secret": randomBytes(t, 16), "no secret": nil} {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		tr := dialObf(t, n.addr, secret, cdnDC)
		_, err := mtclient.CreateAuthKey(ctx, tr, n.pub, mtclient.KeyOptions{DC: cdnDC})
		if err == nil || ctx.Err() != nil {
			t.Errorf("%s: %v (the connection was not closed)", name, err)
		}
		_ = tr.Close()
		cancel()
	}
}

// TestConnLimit: an address holds at most MaxConnsPerIP connections.
func TestConnLimit(t *testing.T) {
	n := startNode(t, 2)
	n.client(t, mtclient.KeyOptions{})
	n.client(t, mtclient.KeyOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tr := dialObf(t, n.addr, n.secret, cdnDC)
	defer tr.Close()
	if _, err := mtclient.CreateAuthKey(ctx, tr, n.pub, mtclient.KeyOptions{DC: cdnDC}); err == nil {
		t.Fatal("a third connection from the address with a limit of 2")
	}
}
