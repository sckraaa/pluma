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
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"github.com/teamgram/teamgram-server/app/service/sfu/internal/room"
	"github.com/teamgram/teamgram-server/app/service/sfu/internal/transport"
	pb "github.com/teamgram/teamgram-server/app/service/sfu/sfu"
	"github.com/teamgram/teamgram-server/pkg/groupcallpeer"
	"github.com/teamgram/teamgram-server/pkg/tgcallsjson"
)

// testCA is a throwaway certificate authority.
type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newCA(t *testing.T, name string) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &testCA{cert: cert, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

// issue makes a certificate for a server (with IP SAN) or a client.
func (ca *testCA) issue(t *testing.T, server bool) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "sfu-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	if server {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		tmpl.IPAddresses = []net.IP{net.IPv4(127, 0, 0, 1)}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

type fixture struct {
	addr     string
	serverCA *testCA
	clientCA *testCA
}

func startServer(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	serverCA, clientCA := newCA(t, "server-ca"), newCA(t, "client-ca")
	certPEM, keyPEM := serverCA.issue(t, true)
	tlsConfig, err := ServerTLS(TLSFiles{
		CertFile:     writeFile(t, dir, "server.pem", certPEM),
		KeyFile:      writeFile(t, dir, "server-key.pem", keyPEM),
		ClientCAFile: writeFile(t, dir, "client-ca.pem", clientCA.pem),
	})
	if err != nil {
		t.Fatal(err)
	}

	mux, err := transport.NewMux(transport.MuxConfig{UDP: "127.0.0.1:0", IncludeLoopback: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mux.Close() })
	cert, err := transport.NewCertificate()
	if err != nil {
		t.Fatal(err)
	}
	bus := NewBus()
	rooms, err := room.NewManager(room.Config{
		Addresses: []tgcallsjson.Address{{IP: netip.MustParseAddr("127.0.0.1"), Port: uint16(mux.UDPAddr().Port)}},
		Plaintext: true,
		Transport: transport.EndpointConfig{StartTimeout: 300 * time.Millisecond},
	}, mux, cert, bus)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rooms.Close)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewGRPCServer(tlsConfig, NewService(rooms, bus))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return &fixture{addr: lis.Addr().String(), serverCA: serverCA, clientCA: clientCA}
}

// dial connects with a client certificate of ca (none if ca is nil).
func (f *fixture) dial(t *testing.T, ca *testCA) pb.SFUClient {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(f.serverCA.cert)
	cfg := &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}
	if ca != nil {
		certPEM, keyPEM := ca.issue(t, false)
		pair, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			t.Fatal(err)
		}
		cfg.Certificates = []tls.Certificate{pair}
	}
	conn, err := grpc.NewClient(f.addr, grpc.WithTransportCredentials(credentials.NewTLS(cfg)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return pb.NewSFUClient(conn)
}

func joinParams(t *testing.T) ([]byte, *groupcallpeer.Peer) {
	t.Helper()
	p, err := groupcallpeer.New(groupcallpeer.Config{VideoLayers: 3})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	params, err := p.JoinParams()
	if err != nil {
		t.Fatal(err)
	}
	return params, p
}

func code(err error) codes.Code { return status.Code(err) }

func TestControlAPI(t *testing.T) {
	f := startServer(t)
	c := f.dial(t, f.clientCA)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	events, err := c.Events(ctx, &pb.EventsRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if created, err := c.CreateRoom(ctx, &pb.CreateRoomRequest{CallId: 5}); err != nil || !created.GetCreated() {
		t.Fatalf("create: %v %v", created, err)
	}
	if again, err := c.CreateRoom(ctx, &pb.CreateRoomRequest{CallId: 5}); err != nil || again.GetCreated() {
		t.Fatalf("create again: %v %v", again, err)
	}
	if _, err := c.CreateRoom(ctx, &pb.CreateRoomRequest{CallId: 6, E2E: true}); code(err) != codes.FailedPrecondition {
		t.Errorf("e2e on a plaintext node: %v", err)
	}

	params, peer := joinParams(t)
	res, err := c.Join(ctx, &pb.JoinRequest{CallId: 5, Participant: 100, Params: params})
	if err != nil {
		t.Fatal(err)
	}
	answer, err := tgcallsjson.ParseJoinResponse(res.GetParams())
	if err != nil || answer.Video.Endpoint != res.GetEndpoint() || len(answer.Transport.Candidates) != 1 {
		t.Fatalf("answer %+v %v", answer, err)
	}
	if len(res.GetSources()) != 7 || res.GetSources()[0] != peer.AudioSSRC() {
		t.Errorf("sources %v", res.GetSources())
	}

	src, err := c.Sources(ctx, &pb.SourcesRequest{CallId: 5})
	if err != nil || len(src.GetConnections()) != 1 || src.GetConnections()[0].GetParticipant() != 100 || src.GetConnections()[0].GetConnected() {
		t.Fatalf("sources %v %v", src, err)
	}

	if _, err := c.Join(ctx, &pb.JoinRequest{CallId: 5, Participant: 101, Params: params}); code(err) != codes.AlreadyExists {
		t.Errorf("duplicate sources: %v", err)
	}
	if _, err := c.Join(ctx, &pb.JoinRequest{CallId: 9, Participant: 101, Params: params}); code(err) != codes.NotFound {
		t.Errorf("no room: %v", err)
	}
	if _, err := c.Join(ctx, &pb.JoinRequest{CallId: 5, Participant: 101, Params: []byte(`{}`)}); code(err) != codes.InvalidArgument {
		t.Errorf("bad params: %v", err)
	}
	if _, err := c.Join(ctx, &pb.JoinRequest{CallId: 5, Participant: 101, Kind: 7, Params: params}); code(err) != codes.InvalidArgument {
		t.Errorf("bad kind: %v", err)
	}
	if _, err := c.SetMuted(ctx, &pb.SetMutedRequest{CallId: 5, Participant: 100, Muted: true}); err != nil {
		t.Errorf("mute: %v", err)
	}

	// The client never connects: the SFU reports it gone.
	for {
		ev, err := events.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if ev.GetType() == pb.Event_TYPE_DISCONNECTED {
			if ev.GetCallId() != 5 || ev.GetParticipant() != 100 || ev.GetReason() != "no_check" {
				t.Errorf("event %v", ev)
			}
			break
		}
	}
	if _, err := c.Leave(ctx, &pb.LeaveRequest{CallId: 5, Participant: 100}); code(err) != codes.NotFound {
		t.Errorf("leave of a connection already gone: %v", err)
	}

	params2, _ := joinParams(t)
	if _, err := c.Join(ctx, &pb.JoinRequest{CallId: 5, Participant: 102, Kind: pb.ConnectionKind_CONNECTION_KIND_PRESENTATION, Params: params2}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Leave(ctx, &pb.LeaveRequest{CallId: 5, Participant: 102, Kind: pb.ConnectionKind_CONNECTION_KIND_PRESENTATION}); err != nil {
		t.Errorf("leave: %v", err)
	}
	if closed, err := c.CloseRoom(ctx, &pb.CloseRoomRequest{CallId: 5}); err != nil || !closed.GetClosed() {
		t.Errorf("close: %v %v", closed, err)
	}
	if _, err := c.Sources(ctx, &pb.SourcesRequest{CallId: 5}); code(err) != codes.NotFound {
		t.Errorf("sources of a closed room: %v", err)
	}
}

func TestControlRequiresClientCertificate(t *testing.T) {
	f := startServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for name, ca := range map[string]*testCA{"no certificate": nil, "foreign CA": newCA(t, "foreign")} {
		c := f.dial(t, ca)
		if _, err := c.CreateRoom(ctx, &pb.CreateRoomRequest{CallId: 1}); code(err) != codes.Unavailable {
			t.Errorf("%s: %v", name, err)
		}
	}
	// The server's own CA does not sign clients.
	c := f.dial(t, f.serverCA)
	if _, err := c.CreateRoom(ctx, &pb.CreateRoomRequest{CallId: 1}); err == nil {
		t.Error("a certificate of the server CA accepted as a client")
	}
}

func TestBusDropsForSlowSubscriber(t *testing.T) {
	b := NewBus()
	ch, cancel := b.Subscribe()
	defer cancel()
	for i := 0; i < eventBuffer+10; i++ {
		b.Publish(room.Event{Type: room.EventSpeaking, CallID: 1, Levels: map[int64]uint8{1: 3}})
	}
	if len(ch) != eventBuffer {
		t.Fatalf("%d queued", len(ch))
	}
	ev := <-ch
	if ev.GetType() != pb.Event_TYPE_SPEAKING || len(ev.GetLevels()) != 1 || ev.GetLevels()[0].GetLevel() != 3 {
		t.Errorf("event %v", ev)
	}
}
