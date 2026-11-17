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

package service

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/svc"
	"github.com/teamgram/teamgram-server/pkg/statsgraph"

	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc/status"
)

// With the statistics on, the statistics of stories and polls and the
// public forwards are served: a request they refuse gets the error of its
// method's page, not ERR_ENTERPRISE_IS_BLOCKED. Each of these is refused
// before anything is read.
func TestStoriesPollsForwardsServed(t *testing.T) {
	key := make([]byte, statsgraph.MinKeyBytes)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	signer, err := statsgraph.NewSigner(key)
	if err != nil {
		t.Fatal(err)
	}
	s := New(&svc.ServiceContext{Signer: signer, Flight: new(singleflight.Group)})
	ctx := context.Background()
	user := mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: 5, AccessHash: 1}).To_InputPeer()
	empty := mtproto.MakeTLInputChannelEmpty(nil).To_InputChannel()
	for name, c := range map[string]struct {
		call func() error
		want error
	}{
		"stats.getStoryStats": {func() error {
			_, err := s.StatsGetStoryStats(ctx, &mtproto.TLStatsGetStoryStats{Peer: user, Id: 1})
			return err
		}, mtproto.ErrPeerIdInvalid},
		"stats.getStoryPublicForwards": {func() error {
			_, err := s.StatsGetStoryPublicForwards(ctx, &mtproto.TLStatsGetStoryPublicForwards{Peer: user, Id: 1, Limit: 10})
			return err
		}, mtproto.ErrPeerIdInvalid},
		"stats.getMessagePublicForwards": {func() error {
			_, err := s.StatsGetMessagePublicForwards5F150144(ctx, &mtproto.TLStatsGetMessagePublicForwards5F150144{Channel: empty, MsgId: 1, Limit: 10})
			return err
		}, mtproto.ErrChannelInvalid},
		"stats.getPollStats": {func() error {
			_, err := s.StatsGetPollStats(ctx, &mtproto.TLStatsGetPollStats{Peer: mtproto.MakeTLInputPeerEmpty(nil).To_InputPeer(), MsgId: 1})
			return err
		}, mtproto.ErrPeerIdInvalid},
	} {
		got, want := status.Convert(c.call()), status.Convert(c.want)
		if got.Code() != want.Code() || got.Message() != want.Message() {
			t.Errorf("%s: %v, want %v", name, got.Message(), want.Message())
		}
	}
}
