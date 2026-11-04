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

//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// Methods of channels not served yet answer ERR_ENTERPRISE_IS_BLOCKED
// (400) through the session's routing, never an internal error, which the
// clients' transports retry without end: those of RPCChannels from the bff
// module channels, those the schema puts in other services (anti-spam here)
// from the proxy.
func TestChannelsNotServed(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	alice := login(ctx, t, testPhones(t, 1)[0], "Alice")
	channel := &mtproto.InputChannel{
		Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel),
		ChannelId:   1,
		AccessHash:  1,
	}
	for name, req := range map[string]mtproto.TLObject{
		"channels.getChannels": &mtproto.TLChannelsGetChannels{
			Constructor: alice.c.Constructor(mtproto.Predicate_channels_getChannels),
			Id:          []*mtproto.InputChannel{channel},
		},
		"channels.getFullChannel": &mtproto.TLChannelsGetFullChannel{
			Constructor: alice.c.Constructor(mtproto.Predicate_channels_getFullChannel),
			Channel:     channel,
		},
		"channels.leaveChannel": &mtproto.TLChannelsLeaveChannel{
			Constructor: alice.c.Constructor(mtproto.Predicate_channels_leaveChannel),
			Channel:     channel,
		},
		"channels.toggleAntiSpam": &mtproto.TLChannelsToggleAntiSpam{
			Constructor: alice.c.Constructor(mtproto.Predicate_channels_toggleAntiSpam),
			Channel:     channel,
			Enabled:     mtproto.BoolTrue,
		},
	} {
		if _, err := alice.c.Invoke(ctx, req); !mtclient.IsRPCError(err, "ERR_ENTERPRISE_IS_BLOCKED") {
			t.Errorf("%s: %v, want ERR_ENTERPRISE_IS_BLOCKED", name, err)
		}
	}
}
