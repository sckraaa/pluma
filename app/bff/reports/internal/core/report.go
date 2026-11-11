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
	"slices"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/moderation"
)

// historyEvidence is how many of the last messages of a private chat a
// messages.reportSpam keeps: that method names no message, and a moderator
// would have nothing to judge by.
const historyEvidence = 5

// fromIdOf is who sent a message of the reporter's view of the chat.
func fromIdOf(m *mtproto.Message, reporter int64) int64 {
	if from := m.GetFromId(); from != nil && from.GetUserId() != 0 {
		return from.GetUserId()
	}
	if !m.GetOut() && m.GetPeerId().GetUserId() != 0 {
		return m.GetPeerId().GetUserId()
	}
	if m.GetOut() {
		return reporter
	}
	return 0
}

func snapshotMessage(m *mtproto.Message, reporter int64) moderation.SnapshotMessage {
	return moderation.SnapshotMessage{
		Id:     m.GetId(),
		Date:   m.GetDate(),
		FromId: fromIdOf(m, reporter),
		Text:   m.GetMessage(),
		Media:  moderation.MediaOf(m.GetMedia()),
	}
}

// reportedMessages reads the reported messages from the reporter's own view
// of the chat, which also proves the reporter could see them. Ids beyond
// the report's limit are dropped.
func (c *ReportsCore) reportedMessages(peer *mtproto.PeerUtil, ids []int32) ([]moderation.SnapshotMessage, error) {
	if len(ids) > moderation.MaxReportMessages {
		ids = ids[:moderation.MaxReportMessages]
	}
	switch peer.PeerType {
	case mtproto.PEER_CHANNEL:
		list, err := c.svcCtx.Dao.ChannelGetMessages(c.ctx, &channel.TLChannelGetMessages{
			SelfId:    c.MD.UserId,
			ChannelId: peer.PeerId,
			Id:        ids,
		})
		if err != nil {
			return nil, err
		}
		out := make([]moderation.SnapshotMessage, 0, len(list.GetDatas()))
		for _, m := range list.GetDatas() {
			if m.GetPredicateName() == mtproto.Predicate_message {
				out = append(out, snapshotMessage(m, c.MD.UserId))
			}
		}
		return out, nil
	default:
		boxes, err := c.svcCtx.Dao.MessageGetUserMessageList(c.ctx, &message.TLMessageGetUserMessageList{
			UserId: c.MD.UserId,
			IdList: ids,
		})
		if err != nil {
			return nil, err
		}
		out := make([]moderation.SnapshotMessage, 0, len(boxes.GetDatas()))
		for _, b := range boxes.GetDatas() {
			if m := b.GetMessage(); m.GetPredicateName() == mtproto.Predicate_message && inChat(m, peer) {
				out = append(out, snapshotMessage(m, c.MD.UserId))
			}
		}
		return out, nil
	}
}

// inChat tells whether a message of the reporter's box is of the reported
// chat. The peer of the message is the reporter's view of it; the peer
// columns of a box row are the sender's (msg dao.ReceiveIncoming).
func inChat(m *mtproto.Message, peer *mtproto.PeerUtil) bool {
	to := m.GetPeerId()
	switch peer.PeerType {
	case mtproto.PEER_SELF:
		return to.GetUserId() == peer.SelfId
	case mtproto.PEER_USER:
		return to.GetUserId() == peer.PeerId
	case mtproto.PEER_CHAT:
		return to.GetChatId() == peer.PeerId
	}
	return false
}

// historyOf reads the last messages of a private chat, the evidence of a
// spam report.
func (c *ReportsCore) historyOf(peer *mtproto.PeerUtil) ([]moderation.SnapshotMessage, error) {
	boxes, err := c.svcCtx.Dao.MessageGetHistoryMessages(c.ctx, &message.TLMessageGetHistoryMessages{
		UserId:   c.MD.UserId,
		PeerType: peer.PeerType,
		PeerId:   peer.PeerId,
		Limit:    historyEvidence,
	})
	if err != nil {
		return nil, err
	}
	out := make([]moderation.SnapshotMessage, 0, len(boxes.GetDatas()))
	for _, b := range boxes.GetDatas() {
		if m := b.GetMessage(); m.GetPredicateName() == mtproto.Predicate_message {
			out = append(out, snapshotMessage(m, c.MD.UserId))
		}
	}
	return out, nil
}

// fillSnapshot adds the people of the report and the title of the chat.
func (c *ReportsCore) fillSnapshot(s *moderation.Snapshot, peer *mtproto.PeerUtil, targetId int64) error {
	ids := []int64{}
	if targetId != 0 {
		ids = append(ids, targetId)
	}
	if peer.PeerType == mtproto.PEER_USER {
		ids = append(ids, peer.PeerId)
	}
	for _, m := range s.Messages {
		if m.FromId != 0 {
			ids = append(ids, m.FromId)
		}
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) > 0 {
		users, err := c.svcCtx.Dao.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: ids})
		if err != nil {
			return err
		}
		for _, id := range ids {
			u, _ := users.GetImmutableUser(id)
			if u == nil {
				continue
			}
			s.Users = append(s.Users, moderation.SnapshotUser{
				Id:        u.Id(),
				FirstName: u.FirstName(),
				LastName:  u.LastName(),
				Username:  u.Username(),
				About:     u.About(),
				Bot:       u.IsBot(),
			})
		}
	}
	var err error
	switch peer.PeerType {
	case mtproto.PEER_CHAT:
		s.PeerTitle, err = c.svcCtx.Dao.Reports.ChatTitle(c.ctx, peer.PeerId)
	case mtproto.PEER_CHANNEL:
		s.PeerTitle, err = c.svcCtx.Dao.Reports.ChannelTitle(c.ctx, peer.PeerId)
	}
	return err
}

// targetOf is whom a report accuses: the sender of the first reported
// message who is not the reporter, or the reported user.
func targetOf(s *moderation.Snapshot, peer *mtproto.PeerUtil, reporter int64) int64 {
	for _, m := range s.Messages {
		if m.FromId != 0 && m.FromId != reporter {
			return m.FromId
		}
	}
	if peer.PeerType == mtproto.PEER_USER && peer.PeerId != reporter {
		return peer.PeerId
	}
	return 0
}

// file stores a report with the limit of reports per user (ADR 0040).
func (c *ReportsCore) file(r *moderation.Report, ids []int32, s *moderation.Snapshot) error {
	since := time.Now().Add(-time.Hour).Unix()
	n, err := c.svcCtx.Dao.Reports.ReportsSince(c.ctx, c.MD.UserId, since)
	if err != nil {
		return err
	}
	if n >= moderation.ReportsPerHour {
		c.Logger.Errorf("reports: user %d over the hourly limit", c.MD.UserId)
		return mtproto.NewErrFloodWaitX(3600)
	}
	r.ReporterUserId = c.MD.UserId
	return c.svcCtx.Dao.Reports.AddReport(c.ctx, r, ids, s)
}

// peerOf reads the InputPeer of a report, which must be a chat.
func peerOf(self int64, in *mtproto.InputPeer) (*mtproto.PeerUtil, error) {
	if in == nil {
		return nil, mtproto.ErrPeerIdInvalid
	}
	peer := mtproto.FromInputPeer2(self, in)
	switch peer.PeerType {
	case mtproto.PEER_USER, mtproto.PEER_CHAT, mtproto.PEER_CHANNEL, mtproto.PEER_SELF:
		return peer, nil
	}
	return nil, mtproto.ErrPeerIdInvalid
}

// reportMessages files a report of messages of a chat; no ids is a report
// of the whole chat, as Android's report of a chat sends. A report whose
// messages are all gone is refused, there being nothing to moderate.
func (c *ReportsCore) reportMessages(kind int8, peer *mtproto.PeerUtil, ids []int32, reason, comment string) error {
	s := &moderation.Snapshot{}
	if len(ids) != 0 {
		messages, err := c.reportedMessages(peer, ids)
		if err != nil {
			return err
		}
		if len(messages) == 0 {
			return mtproto.ErrMessageIdInvalid
		}
		s.Messages = messages
	}
	target := targetOf(s, peer, c.MD.UserId)
	if err := c.fillSnapshot(s, peer, target); err != nil {
		return err
	}
	return c.file(&moderation.Report{
		Kind:         kind,
		PeerType:     peer.PeerType,
		PeerId:       peer.PeerId,
		TargetUserId: target,
		Reason:       reason,
		Comment:      comment,
	}, ids, s)
}
