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

// Package control serves the SFU's gRPC control API (sfu.proto) over
// mutual TLS, and the stream of its events.
package control

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"github.com/teamgram/teamgram-server/app/service/sfu/internal/metrics"
	"github.com/teamgram/teamgram-server/app/service/sfu/internal/room"
	pb "github.com/teamgram/teamgram-server/app/service/sfu/sfu"
)

// eventBuffer is how many events wait for a slow subscriber before new
// ones are dropped for it.
const eventBuffer = 1024

// TLSFiles names the PEM files of the control API: the server's
// certificate and key, and the CA that signs the clients' certificates.
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
	s := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsConfig)))
	pb.RegisterSFUServer(s, svc)
	return s
}

// Service implements the control API on a room manager.
type Service struct {
	pb.UnimplementedSFUServer
	rooms *room.Manager
	bus   *Bus
}

// NewService serves rooms; bus is where the manager publishes events.
func NewService(rooms *room.Manager, bus *Bus) *Service {
	return &Service{rooms: rooms, bus: bus}
}

func toKind(k pb.ConnectionKind) (room.Kind, error) {
	switch k {
	case pb.ConnectionKind_CONNECTION_KIND_MAIN:
		return room.KindMain, nil
	case pb.ConnectionKind_CONNECTION_KIND_PRESENTATION:
		return room.KindPresentation, nil
	}
	return 0, status.Errorf(codes.InvalidArgument, "unknown connection kind %d", int32(k))
}

func fromKind(k room.Kind) pb.ConnectionKind {
	if k == room.KindPresentation {
		return pb.ConnectionKind_CONNECTION_KIND_PRESENTATION
	}
	return pb.ConnectionKind_CONNECTION_KIND_MAIN
}

// toStatus maps the manager's errors to gRPC codes. The messages carry no
// client data.
func toStatus(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, room.ErrRoomNotFound):
		return status.Error(codes.NotFound, "no such room")
	case errors.Is(err, room.ErrNotParticipant):
		return status.Error(codes.NotFound, "no such connection")
	case errors.Is(err, room.ErrSSRCInUse):
		return status.Error(codes.AlreadyExists, "source used by another participant")
	case errors.Is(err, room.ErrBadParams):
		return status.Error(codes.InvalidArgument, "bad join params")
	case errors.Is(err, room.ErrRoomFull):
		return status.Error(codes.ResourceExhausted, "room is full")
	case errors.Is(err, room.ErrE2EMismatch):
		return status.Error(codes.FailedPrecondition, "the node does not take this kind of call")
	}
	return status.Error(codes.Internal, "internal error")
}

func (s *Service) CreateRoom(_ context.Context, req *pb.CreateRoomRequest) (*pb.CreateRoomResponse, error) {
	created, err := s.rooms.CreateRoom(req.GetCallId(), req.GetE2E())
	if err != nil {
		return nil, toStatus(err)
	}
	return &pb.CreateRoomResponse{Created: created}, nil
}

func (s *Service) CloseRoom(_ context.Context, req *pb.CloseRoomRequest) (*pb.CloseRoomResponse, error) {
	return &pb.CloseRoomResponse{Closed: s.rooms.CloseRoom(req.GetCallId())}, nil
}

func (s *Service) Join(_ context.Context, req *pb.JoinRequest) (*pb.JoinResponse, error) {
	kind, err := toKind(req.GetKind())
	if err != nil {
		return nil, err
	}
	res, err := s.rooms.Join(req.GetCallId(), req.GetParticipant(), kind, req.GetParams(), req.GetMuted())
	if err != nil {
		return nil, toStatus(err)
	}
	return &pb.JoinResponse{Params: res.Params, Endpoint: res.Endpoint, Sources: res.Sources}, nil
}

func (s *Service) Leave(_ context.Context, req *pb.LeaveRequest) (*pb.LeaveResponse, error) {
	kind, err := toKind(req.GetKind())
	if err != nil {
		return nil, err
	}
	if err := s.rooms.Leave(req.GetCallId(), req.GetParticipant(), kind); err != nil {
		return nil, toStatus(err)
	}
	return &pb.LeaveResponse{}, nil
}

func (s *Service) SetMuted(_ context.Context, req *pb.SetMutedRequest) (*pb.SetMutedResponse, error) {
	if err := s.rooms.SetMuted(req.GetCallId(), req.GetParticipant(), req.GetMuted()); err != nil {
		return nil, toStatus(err)
	}
	return &pb.SetMutedResponse{}, nil
}

func (s *Service) Sources(_ context.Context, req *pb.SourcesRequest) (*pb.SourcesResponse, error) {
	list, err := s.rooms.Sources(req.GetCallId())
	if err != nil {
		return nil, toStatus(err)
	}
	out := &pb.SourcesResponse{}
	for _, c := range list {
		out.Connections = append(out.Connections, &pb.ConnectionSources{
			Participant: c.Participant,
			Kind:        fromKind(c.Kind),
			Connected:   c.Connected,
			Sources:     c.Sources,
			Muted:       c.Muted,
		})
	}
	return out, nil
}

func (s *Service) Events(_ *pb.EventsRequest, stream grpc.ServerStreamingServer[pb.Event]) error {
	ch, cancel := s.bus.Subscribe()
	defer cancel()
	ctx := stream.Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-ch:
			if err := stream.Send(ev); err != nil {
				return err
			}
		}
	}
}

// Bus hands the manager's events to every subscriber of the stream.
type Bus struct {
	mu   sync.Mutex
	subs map[chan *pb.Event]struct{}
}

// NewBus makes an empty bus.
func NewBus() *Bus { return &Bus{subs: map[chan *pb.Event]struct{}{}} }

// Subscribe returns a channel of events and the function that ends the
// subscription.
func (b *Bus) Subscribe() (<-chan *pb.Event, func()) {
	ch := make(chan *pb.Event, eventBuffer)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
	}
}

// Publish implements room.Sink. It never blocks: a subscriber that falls
// behind loses events, and the metric shows it.
func (b *Bus) Publish(ev room.Event) {
	msg := toProto(ev)
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- msg:
		default:
			metrics.EventsDropped.Inc()
		}
	}
}

func toProto(ev room.Event) *pb.Event {
	out := &pb.Event{
		CallId:      ev.CallID,
		Participant: ev.Participant,
		Kind:        fromKind(ev.Kind),
		TimeUnixMs:  ev.Time.UnixMilli(),
		Network:     ev.Network,
		Role:        ev.Role,
		ConnectMs:   uint32(ev.ConnectTime.Milliseconds()),
		Reason:      ev.Reason,
		IdealHeight: int32(ev.IdealHeight),
	}
	switch ev.Type {
	case room.EventConnected:
		out.Type = pb.Event_TYPE_CONNECTED
	case room.EventDisconnected:
		out.Type = pb.Event_TYPE_DISCONNECTED
	case room.EventSpeaking:
		out.Type = pb.Event_TYPE_SPEAKING
		for p, level := range ev.Levels {
			out.Levels = append(out.Levels, &pb.SpeakingLevel{Participant: p, Level: uint32(level)})
		}
	case room.EventVideoLayers:
		out.Type = pb.Event_TYPE_VIDEO_LAYERS
	}
	return out
}
