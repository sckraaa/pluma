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
	"github.com/teamgram/proto/mtproto"
	pb "github.com/teamgram/teamgram-server/app/service/sfu/sfu"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// PhoneLeaveGroupCallPresentation
// phone.leaveGroupCallPresentation#1c50d144 call:InputGroupCall = Updates;
//
// Stops the shared screen of the account.
func (c *GroupCallsCore) PhoneLeaveGroupCallPresentation(in *mtproto.TLPhoneLeaveGroupCallPresentation) (*mtproto.Updates, error) {
	call, err := c.loadLive(in.GetCall())
	if err != nil {
		return nil, err
	}
	after, row, changed, err := c.svcCtx.Dao.Store.SetPresentation(c.ctx, call.Id, c.MD.UserId, 0, nil)
	if err != nil {
		return nil, storeError(err)
	}
	c.leaveScreen(call, c.MD.UserId)
	a, err := c.loadPeer(call.PeerType, call.PeerId, c.MD.UserId)
	if err != nil {
		return nil, err
	}
	changes := []groupcalls.Change{{Participant: row}}
	if changed {
		c.deliver(a, after, fanOut{changes: changes})
	}
	return c.answer(a, after, changes, nil, false), nil
}

// leaveScreen ends the screen's connection of userId on the SFU; one that
// is gone already is no error.
func (c *GroupCallsCore) leaveScreen(call *groupcalls.Call, userId int64) {
	if call.SfuNode == "" || !c.svcCtx.SFU.Has(call.SfuNode) {
		return
	}
	err := c.svcCtx.SFU.Leave(c.ctx, call.SfuNode, call.Id, userId, pb.ConnectionKind_CONNECTION_KIND_PRESENTATION)
	if err != nil && status.Code(err) != codes.NotFound {
		metricSFUErrors.Inc("leave")
		c.Logger.Errorf("groupcalls: leave of the screen of %d in call %d: %v", userId, call.Id, err)
	}
}
