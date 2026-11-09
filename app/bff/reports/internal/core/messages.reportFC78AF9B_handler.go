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
	"github.com/teamgram/teamgram-server/pkg/moderation"
)

// MessagesReportFC78AF9B
// messages.report#fc78af9b peer:InputPeer id:Vector<int> option:bytes message:string = ReportResult;
//
// The walk through the tree (tree.go) ends in a report with a snapshot of
// the messages at that moment, in the moderation queue.
func (c *ReportsCore) MessagesReportFC78AF9B(in *mtproto.TLMessagesReportFC78AF9B) (*mtproto.ReportResult, error) {
	peer, err := peerOf(c.MD.UserId, in.Peer)
	if err != nil {
		return nil, err
	}
	reason, final, err := stepOf(in.Option)
	if err != nil {
		return nil, err
	}
	if !final {
		if reason == "" {
			return chooseOption(c.MD.LangCode), nil
		}
		return addComment(reason), nil
	}
	if !commentOptional(reason) && in.Message == "" {
		return addComment(reason), nil
	}
	if err = c.reportMessages(moderation.KindMessage, peer, in.Id, reason, in.Message); err != nil {
		c.Logger.Errorf("messages.report#fc78af9b - user %d: %v", c.MD.UserId, err)
		return nil, err
	}
	return mtproto.MakeTLReportResultReported(nil).To_ReportResult(), nil
}
