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

package sfuclient

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/app/bff/groupcalls/internal/config"
	pb "github.com/teamgram/teamgram-server/app/service/sfu/sfu"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// pki is a throwaway pair of CAs, the nodes' and bff's, as
// teamgramd/deploy/sfu/control-certs.sh issues them.
type pki struct {
	dir                string
	serverCA, clientCA *x509.Certificate
	serverKey          *ecdsa.PrivateKey
	clientKey          *ecdsa.PrivateKey
}

func newKey(t *testing.T) *ecdsa.PrivateKey {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func issue(t *testing.T, tmpl, parent *x509.Certificate, pub, signer any) *x509.Certificate {
	tmpl.SerialNumber = big.NewInt(time.Now().UnixNano())
	tmpl.NotBefore, tmpl.NotAfter = time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	if parent == nil {
		parent = tmpl
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, pub, signer)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func writePEM(t *testing.T, path, kind string, der []byte) {
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newPKI(t *testing.T) *pki {
	p := &pki{dir: t.TempDir(), serverKey: newKey(t), clientKey: newKey(t)}
	caTmpl := func(cn string) *x509.Certificate {
		return &x509.Certificate{Subject: pkix.Name{CommonName: cn}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	}
	sk, ck := newKey(t), newKey(t)
	p.serverCA = issue(t, caTmpl("nodes"), nil, &sk.PublicKey, sk)
	p.clientCA = issue(t, caTmpl("signalling"), nil, &ck.PublicKey, ck)
	server := issue(t, &x509.Certificate{
		Subject: pkix.Name{CommonName: "sfu"}, DNSNames: []string{"sfu"}, IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, p.serverCA, &p.serverKey.PublicKey, sk)
	client := issue(t, &x509.Certificate{
		Subject: pkix.Name{CommonName: "bff"}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}, p.clientCA, &p.clientKey.PublicKey, ck)
	// A certificate of the nodes' CA that claims to be bff's.
	stray := issue(t, &x509.Certificate{
		Subject: pkix.Name{CommonName: "bff"}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}, p.serverCA, &p.clientKey.PublicKey, sk)
	writePEM(t, filepath.Join(p.dir, "server.pem"), "CERTIFICATE", server.Raw)
	writePEM(t, filepath.Join(p.dir, "client.pem"), "CERTIFICATE", client.Raw)
	writePEM(t, filepath.Join(p.dir, "stray.pem"), "CERTIFICATE", stray.Raw)
	writePEM(t, filepath.Join(p.dir, "server-ca.pem"), "CERTIFICATE", p.serverCA.Raw)
	for name, k := range map[string]*ecdsa.PrivateKey{"server-key.pem": p.serverKey, "client-key.pem": p.clientKey} {
		der, err := x509.MarshalECPrivateKey(k)
		if err != nil {
			t.Fatal(err)
		}
		writePEM(t, filepath.Join(p.dir, name), "EC PRIVATE KEY", der)
	}
	return p
}

// node is a control API over mutual TLS that trusts bff's CA only.
type testNode struct {
	pb.UnimplementedSFUServer
	events chan *pb.Event
}

func (n *testNode) CreateRoom(context.Context, *pb.CreateRoomRequest) (*pb.CreateRoomResponse, error) {
	return &pb.CreateRoomResponse{Created: true}, nil
}

func (n *testNode) Join(_ context.Context, r *pb.JoinRequest) (*pb.JoinResponse, error) {
	return &pb.JoinResponse{Endpoint: "e1", Params: []byte("{}"), Sources: []uint32{uint32(r.GetParticipant())}}, nil
}

func (n *testNode) Sources(_ context.Context, r *pb.SourcesRequest) (*pb.SourcesResponse, error) {
	return &pb.SourcesResponse{Connections: []*pb.ConnectionSources{{Participant: 7, Sources: []uint32{70}}}}, nil
}

func (n *testNode) Events(_ *pb.EventsRequest, s pb.SFU_EventsServer) error {
	for {
		select {
		case <-s.Context().Done():
			return nil
		case ev := <-n.events:
			if err := s.Send(ev); err != nil {
				return err
			}
		}
	}
}

func startNode(t *testing.T, p *pki) (string, *testNode) {
	cert, err := tls.LoadX509KeyPair(filepath.Join(p.dir, "server.pem"), filepath.Join(p.dir, "server-key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(p.clientCA)
	srv := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13,
	})))
	n := &testNode{events: make(chan *pb.Event, 1)}
	pb.RegisterSFUServer(srv, n)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis.Addr().String(), n
}

func groupCalls(p *pki, cert string, nodes ...config.Node) config.GroupCalls {
	return config.GroupCalls{
		Nodes:          nodes,
		CertFile:       filepath.Join(p.dir, cert),
		KeyFile:        filepath.Join(p.dir, "client-key.pem"),
		CAFile:         filepath.Join(p.dir, "server-ca.pem"),
		RequestTimeout: 5 * time.Second,
	}
}

// A node without an endpoint is left out: the configuration of a server
// without group calls names one from an empty variable.
func TestNewWithoutNodes(t *testing.T) {
	c, err := New(config.GroupCalls{Nodes: []config.Node{{Name: "main"}}})
	if err != nil || c.Enabled() {
		t.Fatalf("enabled %v: %v", c.Enabled(), err)
	}
	if _, err = c.Pick(""); !errors.Is(err, ErrNoNode) {
		t.Fatalf("pick: %v", err)
	}
	if _, err = New(config.GroupCalls{Nodes: []config.Node{{Name: "main", Endpoint: "sfu:20500"}}}); err == nil {
		t.Fatal("a node without certificates")
	}
	p := newPKI(t)
	if _, err = New(groupCalls(p, "client.pem", config.Node{Endpoint: "sfu:20500"})); err == nil {
		t.Fatal("a node without a name")
	}
	if _, err = New(groupCalls(p, "client.pem", config.Node{Name: "a", Endpoint: "sfu:1"}, config.Node{Name: "a", Endpoint: "sfu:2"})); err == nil {
		t.Fatal("a name twice")
	}
}

// The control API over mutual TLS: requests and the stream of events go
// through with bff's certificate; one of the nodes' CA is refused.
func TestMutualTLS(t *testing.T) {
	p := newPKI(t)
	addr, n := startNode(t, p)
	c, err := New(groupCalls(p, "client.pem", config.Node{Name: "main", Endpoint: addr, ServerName: "sfu"}))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx := context.Background()
	if err = c.CreateRoom(ctx, "main", 1, false); err != nil {
		t.Fatal(err)
	}
	resp, err := c.Join(ctx, "main", &pb.JoinRequest{CallId: 1, Participant: 7})
	if err != nil || resp.GetEndpoint() != "e1" {
		t.Fatalf("join: %v %v", resp, err)
	}
	list, err := c.Sources(ctx, "main", 1)
	if err != nil || len(list) != 1 || list[0].GetSources()[0] != 70 {
		t.Fatalf("sources: %v %v", list, err)
	}
	if _, err = c.Join(ctx, "other", &pb.JoinRequest{}); !errors.Is(err, ErrUnknownNode) {
		t.Fatalf("unknown node: %v", err)
	}

	got := make(chan *pb.Event, 1)
	wctx, cancel := context.WithCancel(ctx)
	wg := c.Watch(wctx, func(node string, ev *pb.Event) {
		if node == "main" {
			got <- ev
		}
	})
	n.events <- &pb.Event{Type: pb.Event_TYPE_DISCONNECTED, CallId: 1, Participant: 7}
	select {
	case ev := <-got:
		if ev.GetParticipant() != 7 {
			t.Fatalf("event %v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no event")
	}
	cancel()
	wg.Wait()

	stray, err := New(groupCalls(p, "stray.pem", config.Node{Name: "main", Endpoint: addr, ServerName: "sfu"}))
	if err != nil {
		t.Fatal(err)
	}
	defer stray.Close()
	if err = stray.CreateRoom(ctx, "main", 1, false); err == nil {
		t.Fatal("a certificate of the nodes' CA passed for bff's")
	}
}

// Pick takes the nodes in their order, past skip.
func TestPick(t *testing.T) {
	p := newPKI(t)
	c, err := New(groupCalls(p, "client.pem", config.Node{Name: "a", Endpoint: "127.0.0.1:1"}, config.Node{Name: "b", Endpoint: "127.0.0.1:2"}))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if n, err := c.Pick(""); err != nil || n != "a" {
		t.Fatalf("first: %q %v", n, err)
	}
	if n, err := c.Pick("a"); err != nil || n != "b" {
		t.Fatalf("past a: %q %v", n, err)
	}
	if !c.Has("b") || c.Has("c") {
		t.Fatal("Has")
	}
}
