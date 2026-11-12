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
	"github.com/teamgram/teamgram-server/app/bff/reports/reporttree"
)

// StoriesReportC95BE06A
// stories.report#c95be06a user_id:InputUser id:Vector<int> reason:ReportReason message:string = Bool;
//
// The oldest form, of the stories of a user.
func (c *StoriesCore) StoriesReportC95BE06A(in *mtproto.TLStoriesReportC95BE06A) (*mtproto.Bool, error) {
	if in.UserId == nil {
		return nil, mtproto.ErrUserIdInvalid
	}
	peer := mtproto.FromInputUser(c.MD.UserId, in.UserId).ToInputPeer()
	if err := c.reportStories(peer, in.Id, reporttree.ReasonOf(in.Reason), in.Message); err != nil {
		c.Logger.Errorf("stories.report#c95be06a - user %d: %v", c.MD.UserId, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
