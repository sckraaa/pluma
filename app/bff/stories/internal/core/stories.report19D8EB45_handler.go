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

// StoriesReport19D8EB45
// stories.report#19d8eb45 peer:InputPeer id:Vector<int> option:bytes message:string = ReportResult;
//
// A report of stories walks the tree of messages.report (reporttree: a
// reason, then a comment) and ends in the moderation queue with the
// stories as they are at that moment.
func (c *StoriesCore) StoriesReport19D8EB45(in *mtproto.TLStoriesReport19D8EB45) (*mtproto.ReportResult, error) {
	reason, final, err := reporttree.StepOf(in.Option)
	if err != nil {
		return nil, err
	}
	if !final {
		if reason == "" {
			return reporttree.ChooseOption(c.MD.LangCode), nil
		}
		return reporttree.AddComment(reason), nil
	}
	if !reporttree.CommentOptional(reason) && in.Message == "" {
		return reporttree.AddComment(reason), nil
	}
	if err = c.reportStories(in.Peer, in.Id, reason, in.Message); err != nil {
		c.Logger.Errorf("stories.report#19d8eb45 - user %d: %v", c.MD.UserId, err)
		return nil, err
	}
	return mtproto.MakeTLReportResultReported(nil).To_ReportResult(), nil
}
