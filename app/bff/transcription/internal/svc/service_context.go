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

package svc

import (
	"context"
	"fmt"
	"sync"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/transcription/internal/config"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	message_client "github.com/teamgram/teamgram-server/app/service/biz/message/client"
	messagepb "github.com/teamgram/teamgram-server/app/service/biz/message/message"
	dfs_client "github.com/teamgram/teamgram-server/app/service/dfs/client"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/transcribe"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// Messages reads a message of a user's box.
type Messages interface {
	MessageGetUserMessage(ctx context.Context, in *messagepb.TLMessageGetUserMessage) (*mtproto.MessageBox, error)
}

// Channels reads a channel and its messages as a user sees them.
type Channels interface {
	ChannelGetMutableChannel(ctx context.Context, in *channelpb.TLChannelGetMutableChannel) (*channelpb.MutableChannel, error)
	ChannelGetMessages(ctx context.Context, in *channelpb.TLChannelGetMessages) (*channelpb.Vector_Message, error)
}

// Files reads the bytes of a document.
type Files interface {
	DfsDownloadFile(ctx context.Context, in *dfs.TLDfsDownloadFile) (*mtproto.Upload_File, error)
}

type ServiceContext struct {
	// Config has the defaults of Transcribe in.
	Config   config.Config
	Store    *transcribe.Store
	Messages Messages
	Channels Channels
	Files    Files
	// Engine is nil without a configured engine: transcription is off.
	Engine  transcribe.Engine
	Decoder transcribe.Decoder
	// TempDir holds the copy of a record while it is transcribed; the
	// default of the system if empty.
	TempDir string

	// Queued wakes the worker: a job was queued by this process. Finished
	// wakes the handlers that wait for a text and the sweep of the updates
	// owed. Another instance of bff finds the same by polling the tables.
	Queued   Signal
	Finished Signal
}

func NewServiceContext(c config.Config) *ServiceContext {
	c.Transcribe = c.Transcribe.WithDefaults()
	if err := c.Transcribe.Validate(); err != nil {
		panic(err)
	}
	if err := outbox.Register(outbox.Topics{Push: []string{c.SyncTopic}}); err != nil {
		panic(fmt.Errorf("transcription: %w", err))
	}
	db, err := userbox.Open(c.Mysql.DSN)
	if err != nil {
		panic(fmt.Errorf("transcription: %w", err))
	}
	s := &ServiceContext{
		Config:   c,
		Store:    transcribe.NewStore(db),
		Messages: message_client.NewMessageClient(rpcx.GetCachedRpcClient(c.MessageClient)),
		Channels: channel_client.NewChannelClient(rpcx.GetCachedRpcClient(c.ChannelClient)),
		Files:    dfs_client.NewDfsClient(rpcx.GetCachedRpcClient(c.DfsClient)),
	}
	if c.Transcribe.Enabled() {
		s.Engine = transcribe.NewHTTPEngine(c.Transcribe)
	}
	return s
}

// Signal wakes whoever waits for it, any number of times.
type Signal struct {
	mu sync.Mutex
	ch chan struct{}
}

// Wait returns a channel closed by the next Broadcast.
func (s *Signal) Wait() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ch == nil {
		s.ch = make(chan struct{})
	}
	return s.ch
}

// Broadcast wakes everyone who waits now.
func (s *Signal) Broadcast() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ch != nil {
		close(s.ch)
		s.ch = nil
	}
}
