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
	"time"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/moderation"
)

// reportStories files a report of the stories ids of the peer into the
// moderation queue (ADR 0040): the stories the reporter may open, as they
// are at this moment (caption and media), accusing the user who posted the
// first of them. A report whose stories are all gone or not for the
// reporter is refused: there is nothing to moderate. The limit of reports
// per user is the one of the other reports.
func (c *StoriesCore) reportStories(peer *mtproto.InputPeer, ids []int32, reason, comment string) error {
	v, err := c.view(peer)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return mtproto.ErrStoryIdEmpty
	}
	if len(ids) > moderation.MaxReportMessages {
		ids = ids[:moderation.MaxReportMessages]
	}
	list, err := c.svcCtx.Dao.Store.Get(c.ctx, v.peer, ids)
	if err != nil {
		return err
	}
	if list, err = c.shown(v, list); err != nil {
		return err
	}
	if len(list) == 0 {
		return errStoryIdInvalid
	}
	target := list[0].SenderId
	if target == c.MD.UserId {
		target = 0
	}
	snapshot := &moderation.Snapshot{}
	users := []int64{}
	for _, st := range list {
		snapshot.Messages = append(snapshot.Messages, moderation.SnapshotMessage{
			Id:     st.Id,
			Date:   st.Date,
			FromId: st.SenderId,
			Text:   st.Caption,
			Media:  moderation.MediaOf(st.Media),
		})
		if !containsId(users, st.SenderId) {
			users = append(users, st.SenderId)
		}
	}
	if v.peer.IsUser() && !containsId(users, v.peer.Id) {
		users = append(users, v.peer.Id)
	}
	found, err := c.svcCtx.Dao.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: users})
	if err != nil {
		return err
	}
	for _, id := range users {
		u, ok := found.GetImmutableUser(id)
		if !ok {
			continue
		}
		snapshot.Users = append(snapshot.Users, moderation.SnapshotUser{
			Id:        u.Id(),
			FirstName: u.FirstName(),
			LastName:  u.LastName(),
			Username:  u.Username(),
			About:     u.About(),
			Bot:       u.IsBot(),
		})
	}
	if v.channel != nil {
		snapshot.PeerTitle = v.channel.GetChannel().GetTitle()
	}

	since := time.Now().Add(-time.Hour).Unix()
	n, err := c.svcCtx.Dao.Reports.ReportsSince(c.ctx, c.MD.UserId, since)
	if err != nil {
		return err
	}
	if n >= moderation.ReportsPerHour {
		c.Logger.Errorf("stories: user %d over the hourly limit of reports", c.MD.UserId)
		return mtproto.NewErrFloodWaitX(3600)
	}
	return c.svcCtx.Dao.Reports.AddReport(c.ctx, &moderation.Report{
		ReporterUserId: c.MD.UserId,
		Kind:           moderation.KindStory,
		PeerType:       v.peer.Type,
		PeerId:         v.peer.Id,
		TargetUserId:   target,
		Reason:         reason,
		Comment:        comment,
	}, idsOf(list), snapshot)
}
