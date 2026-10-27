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
	"math"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	inboxpb "github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// The largest event a basic group writes: the whole list of a full chat
// (maxChatMembers), each member with the largest id and a rank of 16
// characters of four bytes each, as one inbox event to one member. A
// change writes one to each other member in one transaction; userbox
// splits their INSERTs at 4 MiB of payloads and 500 rows.
func TestLargestChatEvent(t *testing.T) {
	ps := make([]*mtproto.ImmutableChatParticipant, maxChatMembers)
	for i := range ps {
		ps[i] = mtproto.MakeTLImmutableChatParticipant(&mtproto.ImmutableChatParticipant{
			ChatId:          math.MaxInt64,
			UserId:          math.MaxInt64 - int64(i),
			ParticipantType: mtproto.ChatMemberAdmin,
			State:           mtproto.ChatMemberStateNormal,
			InviterUserId:   math.MaxInt64,
			InvitedAt:       math.MaxInt32,
			Rank:            strings.Repeat("𝄞", 16),
		}).To_ImmutableChatParticipant()
	}
	ct := &chatTx{LockedChat: &dao.LockedChat{MutableChat: mtproto.MakeTLMutableChat(&mtproto.MutableChat{
		Chat:             mtproto.MakeTLImmutableChat(&mtproto.ImmutableChat{Id: math.MaxInt64, Version: math.MaxInt32}).To_ImmutableChat(),
		ChatParticipants: ps,
	}).To_MutableChat()}}
	u := ct.participantsUpdate()
	if n := len(u.GetParticipants_CHATPARTICIPANTS().GetParticipants()); n != maxChatMembers {
		t.Fatalf("%d members in the list", n)
	}
	data, err := jsonx.Marshal(&inboxpb.TLInboxSeqUpdatesToInbox{UserId: math.MaxInt64, FromId: math.MaxInt64, Updates: []*mtproto.Update{u}})
	if err != nil {
		t.Fatal(err)
	}
	// Far below the limit of one event. The 199 events of one change, some
	// 8 MB, go in two or three INSERTs, each below max_allowed_packet
	// (userbox.Check); TestFanOutOf200 writes them.
	if len(data) > userbox.MaxEventBytes/16 {
		t.Errorf("an event of %d bytes, limit %d", len(data), userbox.MaxEventBytes)
	}
	t.Logf("the largest event of a basic group: %d bytes, %d for the members of one change", len(data), len(data)*(maxChatMembers-1))
}
