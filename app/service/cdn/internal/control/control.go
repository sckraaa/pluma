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

// Package control serves the control API of a CDN node (cdn.proto) over
// mutual TLS: the home checks the node, hands it the request_token key and
// pushes, deletes and lists the cipher of files.
package control

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	pb "github.com/teamgram/teamgram-server/app/service/cdn/cdn"
	"github.com/teamgram/teamgram-server/app/service/cdn/internal/metrics"
	"github.com/teamgram/teamgram-server/app/service/cdn/internal/nodekey"
	"github.com/teamgram/teamgram-server/app/service/cdn/internal/session"
	"github.com/teamgram/teamgram-server/app/service/cdn/internal/store"
	"github.com/teamgram/teamgram-server/pkg/cdn"
)

// maxChunk bounds one chunk of Put.
const maxChunk = 1 << 20

// TLSFiles names the PEM files of the control API: the node's certificate
// and key, and the CA that signs the home's certificate.
type TLSFiles struct {
	CertFile     string
	KeyFile      string
	ClientCAFile string
}

// ServerTLS builds the server side of mutual TLS: a client without a
// certificate of the CA is refused during the handshake.
func ServerTLS(files TLSFiles) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(files.CertFile, files.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("control: server certificate: %w", err)
	}
	caPEM, err := os.ReadFile(files.ClientCAFile)
	if err != nil {
		return nil, fmt.Errorf("control: client CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("control: no certificate in the client CA file")
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}, nil
}

// NewGRPCServer makes a gRPC server with the TLS config and the service.
func NewGRPCServer(tlsConfig *tls.Config, svc *Service) *grpc.Server {
	s := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsConfig)), grpc.MaxRecvMsgSize(maxChunk+4096))
	pb.RegisterCDNServer(s, svc)
	return s
}

// Service implements the control API.
type Service struct {
	pb.UnimplementedCDNServer

	dcID   int32
	key    *nodekey.Key
	bootID []byte
	files  *store.Store
	egress *store.Egress
	tokens *session.RequestTokens
}

// NewService returns the control API of a node.
func NewService(dcID int32, key *nodekey.Key, bootID []byte, files *store.Store, egress *store.Egress, tokens *session.RequestTokens) *Service {
	return &Service{dcID: dcID, key: key, bootID: bootID, files: files, egress: egress, tokens: tokens}
}

// Status reports the node's identity, cache, disk and traffic.
func (s *Service) Status(context.Context, *pb.StatusRequest) (*pb.StatusResponse, error) {
	used, n := s.files.Usage()
	free, low, err := s.files.Disk()
	if err != nil {
		// Reported as low: the home takes the node out rather than push to
		// a disk it cannot check.
		logx.Errorf("cdn control: free space: %v", err)
	}
	month, sent := s.egress.Month()
	return &pb.StatusResponse{
		DcId:                s.dcID,
		PublicKeyPem:        string(s.key.PublicPEM),
		Fingerprint:         int64(s.key.Fingerprint),
		BootId:              s.bootID,
		CacheUsed:           used,
		CacheCap:            s.files.Cap(),
		EgressMonth:         month,
		EgressBytes:         sent,
		CacheFiles:          int64(n),
		DiskFree:            free,
		DiskLow:             low,
		RequestTokenKeysSet: s.tokens.Ready(),
	}, nil
}

// SetRequestTokenKeys takes the request_token key.
func (s *Service) SetRequestTokenKeys(_ context.Context, req *pb.SetRequestTokenKeysRequest) (*pb.SetRequestTokenKeysResponse, error) {
	if len(req.GetCurrent()) != cdn.RtKeySize || (len(req.GetPrevious()) != 0 && len(req.GetPrevious()) != cdn.RtKeySize) {
		return nil, status.Errorf(codes.InvalidArgument, "the keys are %d bytes", cdn.RtKeySize)
	}
	s.tokens.Set(req.GetCurrent())
	return &pb.SetRequestTokenKeysResponse{}, nil
}

// Put stores the cipher of a file.
func (s *Service) Put(stream grpc.ClientStreamingServer[pb.PutRequest, pb.PutResponse]) error {
	res, err := s.put(stream)
	switch {
	case err == nil:
		metrics.Puts.WithLabelValues("ok").Inc()
		metrics.PutBytes.Add(float64(res.GetSize()))
		return stream.SendAndClose(res)
	case status.Code(err) == codes.InvalidArgument:
		metrics.Puts.WithLabelValues("invalid").Inc()
	case status.Code(err) == codes.ResourceExhausted:
		metrics.Puts.WithLabelValues("exhausted").Inc()
	default:
		metrics.Puts.WithLabelValues("error").Inc()
	}
	return err
}

func (s *Service) put(stream grpc.ClientStreamingServer[pb.PutRequest, pb.PutResponse]) (*pb.PutResponse, error) {
	first, err := stream.Recv()
	if err != nil {
		return nil, err
	}
	h := first.GetHeader()
	if h == nil {
		return nil, status.Error(codes.InvalidArgument, "the first message is not the header")
	}
	w, err := s.files.Put(h.GetFileToken(), h.GetSize())
	switch {
	case errors.Is(err, store.ErrInvalid):
		return nil, status.Errorf(codes.InvalidArgument, "the file_token is %d bytes and the size not negative", cdn.TokenSize)
	case errors.Is(err, store.ErrTooLarge):
		return nil, status.Errorf(codes.ResourceExhausted, "%d bytes are more than the cache (%d)", h.GetSize(), s.files.Cap())
	case errors.Is(err, store.ErrDiskLow):
		return nil, status.Error(codes.ResourceExhausted, "the disk is below its reserve")
	case err != nil:
		logx.Errorf("cdn control: put: %v", err)
		return nil, status.Error(codes.Internal, "the node cannot store the file")
	}
	committed := false
	defer func() {
		if !committed {
			w.Abort()
		}
	}()
	for {
		m, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		chunk := m.GetChunk()
		if m.GetHeader() != nil || len(chunk) > maxChunk {
			return nil, status.Error(codes.InvalidArgument, "a second header or a chunk over 1 MiB")
		}
		if _, err := w.Write(chunk); err != nil {
			if errors.Is(err, store.ErrSize) {
				return nil, status.Errorf(codes.InvalidArgument, "more bytes than the size %d", h.GetSize())
			}
			logx.Errorf("cdn control: put: %v", err)
			return nil, status.Error(codes.Internal, "the node cannot store the file")
		}
	}
	committed = true
	sum, err := w.Commit()
	if errors.Is(err, store.ErrSize) {
		return nil, status.Errorf(codes.InvalidArgument, "fewer bytes than the size %d", h.GetSize())
	}
	if err != nil {
		logx.Errorf("cdn control: put: %v", err)
		return nil, status.Error(codes.Internal, "the node cannot store the file")
	}
	return &pb.PutResponse{Size: h.GetSize(), Sha256: sum}, nil
}

// Delete removes a file.
func (s *Service) Delete(_ context.Context, req *pb.DeleteRequest) (*pb.DeleteResponse, error) {
	return &pb.DeleteResponse{Deleted: s.files.Delete(req.GetFileToken())}, nil
}

// Inventory lists the files.
func (s *Service) Inventory(_ *pb.InventoryRequest, stream grpc.ServerStreamingServer[pb.InventoryItem]) error {
	for _, it := range s.files.List() {
		if err := stream.Send(&pb.InventoryItem{FileToken: it.Token, Size: it.Size}); err != nil {
			return err
		}
	}
	return nil
}
