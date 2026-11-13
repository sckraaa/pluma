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
	"hash/fnv"
	"strconv"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The service messages of a call in its chat (ADR 0059 §6):
// messageActionGroupCallScheduled when a call is scheduled,
// messageActionGroupCall when it starts and, with the duration, when it
// ends. They are sent as the creator of the call with random ids made of
// the call, so that the collector's retry of a send that was only lost on
// the way gets the first message back (msg's SentBefore), as for the
// messages of one-to-one calls.

// messageRandomId is the random_id of a service message of a call.
func messageRandomId(callId int64, bit int) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("groupcall:" + strconv.FormatInt(callId, 10) + ":" + strconv.Itoa(bit)))
	return int64(h.Sum64() >> 1)
}

// actionOf is the service message of a call for one posted bit.
func actionOf(call *groupcalls.Call, bit int) *mtproto.MessageAction {
	switch bit {
	case groupcalls.PostedScheduled:
		return mtproto.MakeTLMessageActionGroupCallScheduled(&mtproto.MessageAction{
			Call:               call.Input(),
			ScheduleDate_INT32: call.ScheduleDate,
		}).To_MessageAction()
	case groupcalls.PostedEnd:
		return mtproto.MakeTLMessageActionGroupCall(&mtproto.MessageAction{
			Call:     call.Input(),
			Duration: wrapperspb.Int32(call.Duration),
		}).To_MessageAction()
	}
	return mtproto.MakeTLMessageActionGroupCall(&mtproto.MessageAction{Call: call.Input()}).To_MessageAction()
}

// due are the service messages of a call that are not in the history, in
// their order.
func due(call *groupcalls.Call) []int {
	var out []int
	if call.ScheduleDate != 0 && call.Posted&groupcalls.PostedScheduled == 0 {
		out = append(out, groupcalls.PostedScheduled)
	}
	if call.StartDate != 0 && call.Posted&groupcalls.PostedStart == 0 {
		out = append(out, groupcalls.PostedStart)
	}
	if call.StartDate != 0 && call.Discarded && call.Posted&groupcalls.PostedEnd == 0 {
		out = append(out, groupcalls.PostedEnd)
	}
	return out
}

// post sends the service messages of a call that are due. A failed send is
// logged and left to the collector.
func (c *GroupCallsCore) post(call *groupcalls.Call) {
	for _, bit := range due(call) {
		if !c.sendService(call.CreatorId, 0, call.PeerType, call.PeerId, messageRandomId(call.Id, bit), actionOf(call, bit)) {
			return
		}
		if err := c.svcCtx.Dao.Store.MarkPosted(c.ctx, call.Id, int8(bit)); err != nil {
			c.Logger.Errorf("groupcalls: posting of call %d: %v", call.Id, err)
			return
		}
	}
}

// sendService sends a service message of fromId to a chat or a channel.
func (c *GroupCallsCore) sendService(fromId, authKeyId int64, peerType int32, peerId, randomId int64, action *mtproto.MessageAction) bool {
	_, err := c.svcCtx.Dao.MsgClient.MsgSendMessageV2(c.ctx, &msg.TLMsgSendMessageV2{
		UserId:    fromId,
		AuthKeyId: authKeyId,
		PeerType:  peerType,
		PeerId:    peerId,
		Message: []*msg.OutboxMessage{msg.MakeTLOutboxMessage(&msg.OutboxMessage{
			NoWebpage: true,
			RandomId:  randomId,
			Message: mtproto.MakeTLMessageService(&mtproto.Message{
				Out:    true,
				FromId: mtproto.MakePeerUser(fromId),
				PeerId: groupcalls.PeerOf(peerType, peerId),
				Date:   int32(time.Now().Unix()),
				Action: action,
			}).To_Message(),
		}).To_OutboxMessage()},
	})
	if err != nil {
		c.Logger.Errorf("groupcalls: a service message to %d/%d: %v", peerType, peerId, err)
		return false
	}
	return true
}
