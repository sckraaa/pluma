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

package control

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	pb "github.com/teamgram/teamgram-server/app/service/cdn/cdn"
	"github.com/teamgram/teamgram-server/app/service/cdn/internal/nodekey"
	"github.com/teamgram/teamgram-server/app/service/cdn/internal/session"
	"github.com/teamgram/teamgram-server/app/service/cdn/internal/store"
	"github.com/teamgram/teamgram-server/pkg/cdn"
)

// pki is a throwaway pair of CAs and certificates, as control-certs.sh
// issues them: the node's, signed by one CA, and the home's, by another.
type pki struct {
	dir                   string
	serverCA, clientCA    *x509.Certificate
	serverCAKey, clientCK *ecdsa.PrivateKey
}

func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func newCA(t *testing.T, name string) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	k := newKey(t)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return c, k
}

// leaf writes a certificate signed by ca to dir/name.pem and
// dir/name-key.pem.
func leaf(t *testing.T, dir, name string, ca *x509.Certificate, caKey *ecdsa.PrivateKey, usage x509.ExtKeyUsage) {
	t.Helper()
	k := newKey(t)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		DNSNames:     []string{"cdn"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &k.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	writePEM(t, filepath.Join(dir, name+".pem"), "CERTIFICATE", der)
	writePEM(t, filepath.Join(dir, name+"-key.pem"), "EC PRIVATE KEY", kb)
}

func writePEM(t *testing.T, path, typ string, der []byte) {
	t.Helper()
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newPKI(t *testing.T) *pki {
	t.Helper()
	p := &pki{dir: t.TempDir()}
	p.serverCA, p.serverCAKey = newCA(t, "nodes")
	p.clientCA, p.clientCK = newCA(t, "home")
	writePEM(t, filepath.Join(p.dir, "server-ca.pem"), "CERTIFICATE", p.serverCA.Raw)
	writePEM(t, filepath.Join(p.dir, "client-ca.pem"), "CERTIFICATE", p.clientCA.Raw)
	leaf(t, p.dir, "server", p.serverCA, p.serverCAKey, x509.ExtKeyUsageServerAuth)
	leaf(t, p.dir, "client", p.clientCA, p.clientCK, x509.ExtKeyUsageClientAuth)
	// A client certificate of the wrong CA: the nodes'.
	leaf(t, p.dir, "intruder", p.serverCA, p.serverCAKey, x509.ExtKeyUsageClientAuth)
	return p
}

type fixture struct {
	dir    string
	files  *store.Store
	tokens *session.RequestTokens
	key    *nodekey.Key
	addr   string
	pki    *pki
}

func start(t *testing.T) *fixture {
	t.Helper()
	p := newPKI(t)
	dir := t.TempDir()
	key, err := nodekey.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	files, err := store.Open(store.Config{Dir: dir, CacheBytes: 1 << 20, IdleTTL: time.Hour, DiskReserve: 1000,
		DiskFree: func(string) (int64, error) { return 1 << 30, nil }})
	if err != nil {
		t.Fatal(err)
	}
	egress, err := store.OpenEgress(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	egress.Add(777)
	tlsConfig, err := ServerTLS(TLSFiles{
		CertFile:     filepath.Join(p.dir, "server.pem"),
		KeyFile:      filepath.Join(p.dir, "server-key.pem"),
		ClientCAFile: filepath.Join(p.dir, "client-ca.pem"),
	})
	if err != nil {
		t.Fatal(err)
	}
	tokens := &session.RequestTokens{}
	srv := NewGRPCServer(tlsConfig, NewService(201, key, []byte("0123456789abcdef"), files, egress, tokens))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(srv.Stop)
	return &fixture{dir: dir, files: files, tokens: tokens, key: key, addr: ln.Addr().String(), pki: p}
}

// dial connects as the home, with the client certificate name.
func (f *fixture) dial(t *testing.T, name string) pb.CDNClient {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(f.pki.serverCA)
	cfg := &tls.Config{RootCAs: pool, ServerName: "cdn", MinVersion: tls.VersionTLS13}
	if name != "" {
		cert, err := tls.LoadX509KeyPair(filepath.Join(f.pki.dir, name+".pem"), filepath.Join(f.pki.dir, name+"-key.pem"))
		if err != nil {
			t.Fatal(err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	conn, err := grpc.NewClient(f.addr, grpc.WithTransportCredentials(credentials.NewTLS(cfg)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return pb.NewCDNClient(conn)
}

func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return c
}

func randBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

// put sends a Put of size bytes made of chunks.
func put(t *testing.T, c pb.CDNClient, token []byte, size int64, chunks ...[]byte) (*pb.PutResponse, error) {
	t.Helper()
	stream, err := c.Put(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&pb.PutRequest{Part: &pb.PutRequest_Header{Header: &pb.PutHeader{FileToken: token, Size: size}}}); err != nil {
		return nil, err
	}
	for _, ch := range chunks {
		if err := stream.Send(&pb.PutRequest{Part: &pb.PutRequest_Chunk{Chunk: ch}}); err != nil {
			if errors.Is(err, io.EOF) {
				break // the node answered early: CloseAndRecv has why
			}
			return nil, err
		}
	}
	return stream.CloseAndRecv()
}

func TestStatusAndKeys(t *testing.T) {
	f := start(t)
	c := f.dial(t, "client")
	st, err := c.Status(ctx(t), &pb.StatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if st.GetDcId() != 201 || st.GetFingerprint() != int64(f.key.Fingerprint) || st.GetPublicKeyPem() != string(f.key.PublicPEM) ||
		string(st.GetBootId()) != "0123456789abcdef" || st.GetCacheCap() != 1<<20 || st.GetEgressBytes() != 777 ||
		st.GetEgressMonth() != time.Now().UTC().Format("2006-01") || st.GetDiskLow() || st.GetRequestTokenKeysSet() {
		t.Fatalf("status: %+v", st)
	}

	if _, err := c.SetRequestTokenKeys(ctx(t), &pb.SetRequestTokenKeysRequest{Current: randBytes(t, 16)}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a short key: %v", err)
	}
	if _, err := c.SetRequestTokenKeys(ctx(t), &pb.SetRequestTokenKeysRequest{Current: randBytes(t, cdn.RtKeySize), Previous: randBytes(t, cdn.RtKeySize)}); err != nil {
		t.Fatal(err)
	}
	if st, err = c.Status(ctx(t), &pb.StatusRequest{}); err != nil || !st.GetRequestTokenKeysSet() {
		t.Fatalf("after SetRequestTokenKeys: %v %+v", err, st)
	}
	if _, ok := f.tokens.Sign(randBytes(t, cdn.TokenSize), time.Now()); !ok {
		t.Fatal("the node does not sign after SetRequestTokenKeys")
	}
}

func TestPutDeleteInventory(t *testing.T) {
	f := start(t)
	c := f.dial(t, "client")
	tok := randBytes(t, cdn.TokenSize)
	body := randBytes(t, 300000)
	res, err := put(t, c, tok, int64(len(body)), body[:100000], body[100000:200000], body[200000:])
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	if res.GetSize() != int64(len(body)) || !bytes.Equal(res.GetSha256(), sum[:]) {
		t.Fatalf("put answer: %d bytes", res.GetSize())
	}
	got, _, _, err := f.files.ReadAt(tok, 131072, 131072)
	if err != nil || !bytes.Equal(got, body[131072:262144]) {
		t.Fatalf("stored bytes: %v", err)
	}

	inv, err := c.Inventory(ctx(t), &pb.InventoryRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var items []*pb.InventoryItem
	for {
		it, err := inv.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, it)
	}
	if len(items) != 1 || !bytes.Equal(items[0].GetFileToken(), tok) || items[0].GetSize() != int64(len(body)) {
		t.Fatalf("inventory: %v", items)
	}

	d, err := c.Delete(ctx(t), &pb.DeleteRequest{FileToken: tok})
	if err != nil || !d.GetDeleted() {
		t.Fatalf("delete: %v %v", d, err)
	}
	if d, err = c.Delete(ctx(t), &pb.DeleteRequest{FileToken: tok}); err != nil || d.GetDeleted() {
		t.Fatalf("delete again: %v %v", d, err)
	}
	if _, _, _, err := f.files.ReadAt(tok, 0, 4096); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a deleted file: %v", err)
	}
}

// TestPutRefused: a broken or impossible Put stores nothing.
func TestPutRefused(t *testing.T) {
	f := start(t)
	c := f.dial(t, "client")
	tok := randBytes(t, cdn.TokenSize)

	if _, err := put(t, c, tok, 1000, randBytes(t, 999)); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("fewer bytes than the size: %v", err)
	}
	if _, err := put(t, c, tok, 1000, randBytes(t, 600), randBytes(t, 600)); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("more bytes than the size: %v", err)
	}
	if _, err := put(t, c, []byte("short"), 10, randBytes(t, 10)); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a short token: %v", err)
	}
	if _, err := put(t, c, tok, 2<<20); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("a file over the cache: %v", err)
	}
	stream, err := c.Put(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&pb.PutRequest{Part: &pb.PutRequest_Chunk{Chunk: []byte("x")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.CloseAndRecv(); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("no header: %v", err)
	}

	// A stream cut by the home.
	cctx, cancel := context.WithCancel(context.Background())
	stream, err = c.Put(cctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&pb.PutRequest{Part: &pb.PutRequest_Header{Header: &pb.PutHeader{FileToken: tok, Size: 1000}}}); err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&pb.PutRequest{Part: &pb.PutRequest_Chunk{Chunk: randBytes(t, 500)}}); err != nil {
		t.Fatal(err)
	}
	waitTmp := func(want int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			left, err := os.ReadDir(filepath.Join(f.dir, "tmp"))
			if err != nil {
				t.Fatal(err)
			}
			if len(left) == want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%d unfinished files, want %d", len(left), want)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitTmp(1) // the put is under way
	cancel()
	waitTmp(0) // and the node dropped what it got
	if used, n := f.files.Usage(); used != 0 || n != 0 {
		t.Fatalf("after refused puts: %d bytes, %d files", used, n)
	}
	if _, _, _, err := f.files.ReadAt(tok, 0, 4096); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a refused put is served: %v", err)
	}
}

// TestMutualTLS: only the home's certificate is let in.
func TestMutualTLS(t *testing.T) {
	f := start(t)
	for _, name := range []string{"", "intruder"} {
		if _, err := f.dial(t, name).Status(ctx(t), &pb.StatusRequest{}); err == nil {
			t.Errorf("client certificate %q let in", name)
		}
	}
}
