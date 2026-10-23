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

package core

import (
	"errors"
	"fmt"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/interface/session/internal/sess"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestPushFetchError(t *testing.T) {
	// What the authsession client returns: the status rebuilt from the wire.
	overWire := status.FromProto(status.Convert(mtproto.ErrAuthKeyUnregistered).Proto()).Err()

	tests := []struct {
		name string
		err  error
		code codes.Code
		msg  string
	}{
		{"unregistered key", mtproto.ErrAuthKeyUnregistered, codes.NotFound, pushRejectUnknownKey},
		{"unregistered key over gRPC", overWire, codes.NotFound, pushRejectUnknownKey},
		{"wrapped unregistered key", fmt.Errorf("fetch: %w", mtproto.ErrAuthKeyUnregistered), codes.NotFound, pushRejectUnknownKey},
		{"no key in the push", mtproto.ErrInputRequestInvalid, codes.NotFound, pushRejectUnknownKey},
		{"authsession down", status.Error(codes.Unavailable, "connection refused"), codes.FailedPrecondition, pushRejectAuthUnavailable},
		{"authsession timeout", status.Error(codes.DeadlineExceeded, "deadline"), codes.FailedPrecondition, pushRejectAuthUnavailable},
		{"other error", errors.New("boom"), codes.FailedPrecondition, pushRejectAuthUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := status.Convert(pushFetchError(tt.err))
			if st.Code() != tt.code || st.Message() != tt.msg {
				t.Fatalf("got %v %q, want %v %q", st.Code(), st.Message(), tt.code, tt.msg)
			}
		})
	}
}

func TestPushArriveError(t *testing.T) {
	if err := pushArriveError(nil); err != nil {
		t.Fatalf("nil: got %v", err)
	}
	st := status.Convert(pushArriveError(fmt.Errorf("arrive: %w", sess.ErrDataChannelFull)))
	if st.Code() != codes.FailedPrecondition || st.Message() != pushRejectChannelFull {
		t.Fatalf("channel full: got %v %q", st.Code(), st.Message())
	}
}
