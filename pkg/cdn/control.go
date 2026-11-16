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
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/teamgram/teamgram-server/app/service/cdn/cdn"
)

// NodeAPI is the control API of a CDN node (app/service/cdn/cdn/cdn.proto) as
// the home uses it; NodeClient implements it, the tests fake it.
type NodeAPI interface {
	// Status is the node's identity, cache, disk and traffic.
	Status(ctx context.Context) (*pb.StatusResponse, error)
	// SetRequestTokenKeys hands the node the request_token keys.
	SetRequestTokenKeys(ctx context.Context, current, previous []byte) error
	// Put opens the upload of the cipher of a file of size bytes.
	Put(ctx context.Context, token []byte, size int64) (PutStream, error)
	// Delete removes a file; false if the node did not have it.
	Delete(ctx context.Context, token []byte) (bool, error)
	// Inventory calls fn with every file the node holds; an error of fn
	// stops it.
	Inventory(ctx context.Context, fn func(token []byte, size int64) error) error
}

// PutStream is an upload opened by Node.Put.
type PutStream interface {
	// Send sends the next bytes of the cipher, at most PushChunkSize.
	Send(chunk []byte) error
	// Close ends the upload and returns what the node stored: its size and
	// the SHA-256 of its bytes.
	Close() (size int64, sum []byte, err error)
}

// PushChunkSize is the size of the parts a file is read from dfs and sent
// to the node in: the largest part of upload.getFile and of a chunk of Put.
const PushChunkSize = 1 << 20

// Timeouts of the control API; Put is bounded by its caller.
const (
	statusTimeout    = 3 * time.Second
	controlTimeout   = 5 * time.Second
	inventoryTimeout = 2 * time.Minute
)

// NodeClient is the control API of one node over mutual TLS (ADR 0065 §5,
// as the SFU's, ADR 0059).
type NodeClient struct {
	conn *grpc.ClientConn
	api  pb.CDNClient
}

var _ NodeAPI = (*NodeClient)(nil)

// DialNode returns the client of the node of s. The connection is made on
// the first call and again after a failure.
func DialNode(s *Settings) (*NodeClient, error) {
	tc, err := clientTLS(s.CertFile, s.KeyFile, s.CAFile)
	if err != nil {
		return nil, err
	}
	tc.ServerName = s.ServerName
	// passthrough: the name of the endpoint is resolved on every attempt
	// to connect, as for the SFU (app/bff/groupcalls/internal/sfuclient).
	conn, err := grpc.NewClient("passthrough:///"+s.NodeControl, grpc.WithTransportCredentials(credentials.NewTLS(tc)))
	if err != nil {
		return nil, fmt.Errorf("cdn: control API: %w", err)
	}
	return &NodeClient{conn: conn, api: pb.NewCDNClient(conn)}, nil
}

// clientTLS is the client side of mutual TLS: bff's certificate, and the CA
// the node's certificate must chain to.
func clientTLS(certFile, keyFile, caFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("cdn: client certificate: %w", err)
	}
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("cdn: CA of the node: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("cdn: no certificate in the CA file of the node")
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}, RootCAs: pool, MinVersion: tls.VersionTLS13}, nil
}

// Close closes the connection.
func (c *NodeClient) Close() error { return c.conn.Close() }

func (c *NodeClient) Status(ctx context.Context) (*pb.StatusResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	return c.api.Status(ctx, &pb.StatusRequest{})
}

func (c *NodeClient) SetRequestTokenKeys(ctx context.Context, current, previous []byte) error {
	ctx, cancel := context.WithTimeout(ctx, controlTimeout)
	defer cancel()
	_, err := c.api.SetRequestTokenKeys(ctx, &pb.SetRequestTokenKeysRequest{Current: current, Previous: previous})
	return err
}

func (c *NodeClient) Put(ctx context.Context, token []byte, size int64) (PutStream, error) {
	stream, err := c.api.Put(ctx)
	if err != nil {
		return nil, err
	}
	if err = stream.Send(&pb.PutRequest{Part: &pb.PutRequest_Header{Header: &pb.PutHeader{FileToken: token, Size: size}}}); err != nil {
		return nil, putErr(stream, err)
	}
	return &putStream{stream: stream}, nil
}

type putStream struct {
	stream grpc.ClientStreamingClient[pb.PutRequest, pb.PutResponse]
}

func (p *putStream) Send(chunk []byte) error {
	if err := p.stream.Send(&pb.PutRequest{Part: &pb.PutRequest_Chunk{Chunk: chunk}}); err != nil {
		return putErr(p.stream, err)
	}
	return nil
}

func (p *putStream) Close() (int64, []byte, error) {
	res, err := p.stream.CloseAndRecv()
	if err != nil {
		return 0, nil, err
	}
	return res.GetSize(), res.GetSha256(), nil
}

// putErr returns the status the node ended the stream with when Send saw
// only io.EOF (grpc ClientStream.SendMsg).
func putErr(stream grpc.ClientStreamingClient[pb.PutRequest, pb.PutResponse], err error) error {
	if !errors.Is(err, io.EOF) {
		return err
	}
	if _, rerr := stream.CloseAndRecv(); rerr != nil {
		return rerr
	}
	return errors.New("cdn: the node ended the upload early")
}

func (c *NodeClient) Delete(ctx context.Context, token []byte) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, controlTimeout)
	defer cancel()
	res, err := c.api.Delete(ctx, &pb.DeleteRequest{FileToken: token})
	if err != nil {
		return false, err
	}
	return res.GetDeleted(), nil
}

func (c *NodeClient) Inventory(ctx context.Context, fn func(token []byte, size int64) error) error {
	ctx, cancel := context.WithTimeout(ctx, inventoryTimeout)
	defer cancel()
	stream, err := c.api.Inventory(ctx, &pb.InventoryRequest{})
	if err != nil {
		return err
	}
	for {
		it, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err = fn(it.GetFileToken(), it.GetSize()); err != nil {
			return err
		}
	}
}

// Runtime is the CDN of a bff process: the Home the methods use, and the
// pusher and the prober running beside them.
type Runtime struct {
	Home   *Home
	node   *NodeClient
	cancel context.CancelFunc
	done   chan struct{}
}

// Start dials the node of set and starts the pusher and the prober over
// store. Several processes may run them at once: the pushes are claimed by
// lease, and the checks write the same row.
func Start(store Store, set *Settings, sizer Sizer, reader FileReader) (*Runtime, error) {
	node, err := DialNode(set)
	if err != nil {
		return nil, err
	}
	home := NewHome(store, set, sizer)
	prober := NewProber(store, node, set)
	pusher := NewPusher(store, node, set, PusherDeps{
		Reader:   reader,
		Healthy:  prober.Healthy,
		CacheCap: prober.CacheCap,
		OnReady:  home.Forget,
	})
	home.wake = pusher.Wake

	ctx, cancel := context.WithCancel(context.Background())
	r := &Runtime{Home: home, node: node, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(r.done)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); prober.Run(ctx) }()
		go func() { defer wg.Done(); pusher.Run(ctx) }()
		wg.Wait()
	}()
	return r, nil
}

// Stop stops the pusher and the prober, waits for them and closes the
// connection to the node. A push cut short is claimed again when its lease
// runs out.
func (r *Runtime) Stop() {
	r.cancel()
	<-r.done
	_ = r.node.Close()
}
