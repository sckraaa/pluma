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

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/interface/session/internal/sess"

	"github.com/zeromicro/go-zero/core/metric"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Push rejections answer sync with codes that tell it what to do with the
// device (ADR 0012, p. 7): NotFound — the key is unknown, drop the push;
// FailedPrecondition — this device cannot take the push now, retry once and
// then mark it for resync. Neither code counts as a failure for go-zero's
// breaker, so one device never closes the node for the others; Unavailable
// is left to the transport.
const (
	pushRejectUnknownKey      = "unknown_key"
	pushRejectAuthUnavailable = "auth_unavailable"
	pushRejectChannelFull     = "channel_full"
)

var pushRejected = metric.NewCounterVec(&metric.CounterVecOpts{
	Namespace: "session",
	Subsystem: "push",
	Name:      "rejected_total",
	Help:      "Pushes from sync that session rejected, by reason.",
	Labels:    []string{"reason"},
})

func rejectPush(reason string) error {
	pushRejected.Inc(reason)
	if reason == pushRejectUnknownKey {
		return status.Error(codes.NotFound, reason)
	}
	return status.Error(codes.FailedPrecondition, reason)
}

// pushFetchError maps an error of looking up the device's key.
func pushFetchError(err error) error {
	if isUnknownKey(err) {
		return rejectPush(pushRejectUnknownKey)
	}
	return rejectPush(pushRejectAuthUnavailable)
}

// pushArriveError maps an error of handing the push to the device.
func pushArriveError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sess.ErrDataChannelFull) {
		return rejectPush(pushRejectChannelFull)
	}
	return rejectPush(pushRejectAuthUnavailable)
}

// isUnknownKey reports whether authsession answered that it has no such key
// (or the push named no key). The answer crosses gRPC as a status, which
// errors.Is compares by code and message.
func isUnknownKey(err error) bool {
	return errors.Is(err, mtproto.ErrAuthKeyUnregistered) || errors.Is(err, mtproto.ErrInputRequestInvalid)
}
