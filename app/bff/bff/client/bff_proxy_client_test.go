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

package bff_proxy_client

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
)

// TestRewriteLegacyAndroidRequest: Android's old join constructors become
// the ones whose reply it reads, messages.ChatInviteJoinResult; other
// clients and other requests pass as they are.
func TestRewriteLegacyAndroidRequest(t *testing.T) {
	android := &metadata.RpcMetadata{Client: "android", Layer: 228}
	channel := mtproto.MakeTLInputChannel(&mtproto.InputChannel{ChannelId: 7, AccessHash: 9}).To_InputChannel()

	got := rewriteLegacyAndroidRequest(android, &mtproto.TLMessagesImportChatInvite6C50051C{Hash: "abc"})
	if r, ok := got.(*mtproto.TLMessagesImportChatInviteDE91436E); !ok || r.Hash != "abc" || r.Constructor != mtproto.CRC32_messages_importChatInviteDE91436E {
		t.Errorf("importChatInvite: %T %+v", got, got)
	}
	got = rewriteLegacyAndroidRequest(android, &mtproto.TLChannelsJoinChannel24B524C5{Channel: channel})
	if r, ok := got.(*mtproto.TLChannelsJoinChannel7F6A1E22); !ok || r.Channel.GetChannelId() != 7 || r.Constructor != mtproto.CRC32_channels_joinChannel7F6A1E22 {
		t.Errorf("joinChannel: %T %+v", got, got)
	}
	// The rewrites of task 017 stay.
	if _, ok := rewriteLegacyAndroidRequest(android, &mtproto.TLMessagesCreateChat34A818{Title: "t"}).(*mtproto.TLMessagesCreateChat92CEDDD4); !ok {
		t.Error("createChat is not rewritten")
	}
	// Each reply type is served by a registered route.
	for _, o := range []mtproto.TLObject{
		&mtproto.TLMessagesImportChatInviteDE91436E{Constructor: mtproto.CRC32_messages_importChatInviteDE91436E},
		&mtproto.TLChannelsJoinChannel7F6A1E22{Constructor: mtproto.CRC32_channels_joinChannel7F6A1E22},
	} {
		if mtproto.FindRPCContextTuple(o) == nil {
			t.Errorf("%T has no route", o)
		}
	}

	for _, md := range []*metadata.RpcMetadata{{Client: "ios", Layer: 228}, {Client: "android", Layer: 227}, nil} {
		in := &mtproto.TLMessagesImportChatInvite6C50051C{Hash: "abc"}
		if got := rewriteLegacyAndroidRequest(md, in); got != in {
			t.Errorf("%+v: rewritten to %T", md, got)
		}
	}
}
