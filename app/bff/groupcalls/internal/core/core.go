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

// Package core is the phone.* methods of group calls of chats and channels
// (task 093, ADR 0059) and of their end-to-end encryption (task 094, ADR
// 0060; chain.go). The state is pkg/groupcalls; the media are the SFU's
// (app/service/sfu), reached through sfuclient. What the clients expect of
// each step is cited at the step.
//
// The order of a step is the state, then the SFU, then the updates (ADR
// 0059 §10, ADR 0060 §2), a join included: its block and its row are
// written first, so that a join refused by the chain never touches the
// connection the participant has. A lost update only leaves a gap in the
// versions, which the clients close by reading the call again.
package core

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/groupcalls/internal/svc"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/status"
)

// Errors of core.telegram.org/api/group-calls that the mtproto package does
// not define, and this server's own.
var (
	// errVideoTooMuch: the call has its cameras; Android reloads the call
	// on it (VoIPService.java:2822, 3840).
	errVideoTooMuch = status.Error(mtproto.ErrBadRequest, "GROUPCALL_VIDEO_TOO_MUCH")
	// errParticipantsTooMuch: the call is full (Android
	// GroupCallActivity.java:1337, iOS GroupCalls.swift:707).
	errParticipantsTooMuch = status.Error(mtproto.ErrBadRequest, "GROUPCALL_PARTICIPANTS_TOO_MUCH")
	// errNotConfigured: no SFU node is configured (docs/ops/group-calls.md).
	errNotConfigured = status.Error(mtproto.ErrBadRequest, "GROUPCALLS_NOT_CONFIGURED")
)

type GroupCallsCore struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
	MD *metadata.RpcMetadata
}

func New(ctx context.Context, svcCtx *svc.ServiceContext) *GroupCallsCore {
	return &GroupCallsCore{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
		MD:     metadata.RpcMetadataFromIncoming(ctx),
	}
}

// background is a core of the service's own work (the collector, the
// events of the SFU): no account behind it.
func background(ctx context.Context, svcCtx *svc.ServiceContext) *GroupCallsCore {
	return &GroupCallsCore{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx), MD: &metadata.RpcMetadata{}}
}

// load returns the call in names, GROUPCALL_INVALID for anything else. A
// slug or an invite message names a conference (not served in stage 1).
func (c *GroupCallsCore) load(in *mtproto.InputGroupCall) (*groupcalls.Call, error) {
	if in.GetPredicateName() != mtproto.Predicate_inputGroupCall {
		return nil, mtproto.ErrGroupcallInvalid
	}
	call, err := c.svcCtx.Dao.Store.Get(c.ctx, in.GetId())
	if errors.Is(err, groupcalls.ErrNotFound) {
		return nil, mtproto.ErrGroupcallInvalid
	}
	if err != nil {
		return nil, err
	}
	if call.AccessHash != in.GetAccessHash() {
		return nil, mtproto.ErrGroupcallInvalid
	}
	return call, nil
}

// loadLive is load of a call that is not over.
func (c *GroupCallsCore) loadLive(in *mtproto.InputGroupCall) (*groupcalls.Call, error) {
	call, err := c.load(in)
	if err != nil {
		return nil, err
	}
	if call.Discarded {
		return nil, mtproto.ErrGroupcallAlreadyDiscarded
	}
	return call, nil
}

// positiveId is a random id or access_hash.
func positiveId() (int64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	return int64(binary.BigEndian.Uint64(b[:]) >> 1), nil
}

func now() int32 { return int32(time.Now().Unix()) }

// storeError maps the errors of the Store to those of the API.
func storeError(err error) error {
	switch {
	case errors.Is(err, groupcalls.ErrNotFound):
		return mtproto.ErrGroupcallInvalid
	case errors.Is(err, groupcalls.ErrDiscarded):
		return mtproto.ErrGroupcallAlreadyDiscarded
	case errors.Is(err, groupcalls.ErrScheduled):
		// tdesktop and Android wait for the start of a scheduled call and
		// join after it; a join before it is refused.
		return mtproto.ErrGroupcallInvalid
	case errors.Is(err, groupcalls.ErrNotScheduled):
		return mtproto.ErrGroupcallAlreadyStarted
	case errors.Is(err, groupcalls.ErrNotJoined):
		return mtproto.ErrGroupcallJoinMissing
	case errors.Is(err, groupcalls.ErrSourceInUse):
		return mtproto.ErrGroupcallSsrcDuplicateMuch
	case errors.Is(err, groupcalls.ErrFull):
		return errParticipantsTooMuch
	case errors.Is(err, groupcalls.ErrBadOffset):
		return mtproto.ErrGroupcallInvalid
	}
	return err
}
