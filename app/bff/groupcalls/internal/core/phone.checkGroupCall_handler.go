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
	"slices"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"
)

// PhoneCheckGroupCall
// phone.checkGroupCall#b59cf977 call:InputGroupCall sources:Vector<int> = Vector<int>;
//
// The sources of the account's connections that the SFU holds, of those
// asked. tdesktop and iOS ask every 4 s while they connect and join again
// when their source is missing (tdesktop calls_group_call.cpp:55,
// 3746-3776); Android asks during the whole call (VoIPService.java:
// 2853-2935). So the answer is the SFU's, not the table's: after an SFU
// restart the room is gone, and the client comes back by itself (ADR 0059
// §6, §10).
func (c *GroupCallsCore) PhoneCheckGroupCall(in *mtproto.TLPhoneCheckGroupCall) (*mtproto.Vector_Int, error) {
	call, err := c.loadLive(in.GetCall())
	if err != nil {
		return nil, err
	}
	p, err := c.svcCtx.Dao.Store.ParticipantOf(c.ctx, call.Id, c.MD.UserId)
	if errors.Is(err, groupcalls.ErrNotFound) || (err == nil && !p.InCall) {
		return nil, mtproto.ErrGroupcallJoinMissing
	}
	if err != nil {
		return nil, err
	}
	sources, err := held(c.ctx, c.svcCtx.SFU, call, c.MD.UserId)
	if err != nil {
		// The SFU does not answer: none is held, and the client joins
		// again, which moves the room to a node that answers, if any.
		metricSFUErrors.Inc("sources")
		c.Logger.Errorf("groupcalls: sources of call %d: %v", call.Id, err)
		sources = nil
	}
	out := make([]int32, 0, len(in.GetSources()))
	for _, s := range in.GetSources() {
		if slices.Contains(sources, uint32(s)) {
			out = append(out, s)
		}
	}
	return &mtproto.Vector_Int{Datas: out}, nil
}
