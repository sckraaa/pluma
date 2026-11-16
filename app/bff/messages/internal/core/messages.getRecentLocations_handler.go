// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package core

import (
	"math"
	"time"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
)

const (
	// recentLocationsMax is the most messages of one answer: Android asks
	// for 100 (ui/LocationActivity.java:2606), iOS for the size of the hole
	// it fills (TelegramCore State/Holes.swift:819-827).
	recentLocationsMax = 100
	// recentLocationsPage is how many live locations one query reads.
	recentLocationsPage = 100
	// recentLocationsScanMax bounds the messages read past the last day.
	recentLocationsScanMax = 500
	// liveLocationIndefinite is the period of a live location shared until
	// it is stopped (Android ui/LocationActivity.java:1518, iOS
	// SyncCore_TelegramMediaMap.swift:3 liveLocationIndefinitePeriod).
	liveLocationIndefinite = 0x7fffffff
	// recentLocationsDay is how far back the scan reads without a bound:
	// the periods the clients offer end within a day; only an indefinite
	// location, or one extended again and again, is live for longer.
	recentLocationsDay = 86400
)

// MessagesGetRecentLocations
// messages.getRecentLocations#702a40e0 peer:InputPeer limit:int hash:long = messages.Messages;
//
// The live locations shared in a private chat or a group now, up to one
// per sender, his newest, newest first (core.telegram.org/method/
// messages.getRecentLocations: "returns up to 1 location message
// (messageMediaGeoLive) per chat participant"). A location is live while
// date + period is ahead, or for good with the indefinite period: Android
// keeps only those on the map (ui/LocationActivity.java:2483), iOS counts
// the same (PeerLiveLocationsContext.swift:22). A broadcast channel has
// none: Android does not ask for one (ui/LocationActivity.java:2597-2602)
// and the method is for a "group or private chat".
//
// hash is over the id and the edit_date of each message of the answer
// (core.telegram.org/api/offsets): a live location moves by edits, which
// keep the id. An equal non-zero hash answers messages.messagesNotModified.
func (c *MessagesCore) MessagesGetRecentLocations(in *mtproto.TLMessagesGetRecentLocations) (*mtproto.Messages_Messages, error) {
	if c.MD.IsBot {
		err := mtproto.ErrBotMethodInvalid
		c.Logger.Errorf("messages.getRecentLocations - error: %v", err)
		return nil, err
	}
	limit := in.Limit
	if limit <= 0 || limit > recentLocationsMax {
		limit = recentLocationsMax
	}
	now := int32(time.Now().Unix())

	var (
		r   *mtproto.Messages_Messages
		err error
	)
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.Peer)
	switch peer.PeerType {
	case mtproto.PEER_SELF, mtproto.PEER_USER, mtproto.PEER_CHAT:
		r, err = c.boxRecentLocations(peer, limit, now)
	case mtproto.PEER_CHANNEL:
		r, err = c.channelRecentLocations(peer, limit, now)
	default:
		err = mtproto.ErrPeerIdInvalid
	}
	if err != nil {
		c.Logger.Errorf("messages.getRecentLocations - error: %v", err)
		return nil, err
	}
	if in.Hash != 0 && in.Hash == recentLocationsHash(r.Messages) {
		return mtproto.MakeTLMessagesMessagesNotModified(&mtproto.Messages_Messages{
			Count: int32(len(r.Messages)),
		}).To_Messages_Messages(), nil
	}
	return r, nil
}

// boxRecentLocations reads the live locations of a private chat or a basic
// group from the caller's box.
func (c *MessagesCore) boxRecentLocations(peer *mtproto.PeerUtil, limit, now int32) (*mtproto.Messages_Messages, error) {
	peerType, peerId := peer.PeerType, peer.PeerId
	if peerType == mtproto.PEER_SELF {
		peerType, peerId = mtproto.PEER_USER, c.MD.UserId
	}
	messages, err := pickRecentLocations(limit, now, func(offset int32) ([]locationCandidate, error) {
		boxes, err := c.svcCtx.Dao.MessageClient.MessageSearchByMediaType(c.ctx, &message.TLMessageSearchByMediaType{
			UserId:    c.MD.UserId,
			PeerType:  peerType,
			PeerId:    peerId,
			MediaType: mtproto.MEDIA_GEO_LIVE,
			Offset:    offset,
			Limit:     recentLocationsPage,
		})
		if err != nil {
			return nil, err
		}
		page := make([]locationCandidate, 0, len(boxes.GetBoxList()))
		for _, box := range boxes.GetBoxList() {
			page = append(page, locationCandidate{sender: box.GetSenderUserId(), message: box.ToMessage(c.MD.UserId)})
		}
		return page, nil
	})
	if err != nil {
		return nil, err
	}
	r := emptyMessages()
	r.Messages = messages
	if err = c.fillMessagesPeers(r); err != nil {
		return nil, err
	}
	return r, nil
}

// channelRecentLocations reads the live locations of a supergroup as the
// caller sees it: channel.search checks that he may read it.
func (c *MessagesCore) channelRecentLocations(peer *mtproto.PeerUtil, limit, now int32) (*mtproto.Messages_Messages, error) {
	mc, err := c.svcCtx.Dao.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: peer.PeerId})
	if err != nil {
		return nil, err
	}
	if mc.GetChannel().GetAccessHash() != peer.AccessHash {
		return nil, mtproto.ErrChannelInvalid
	}
	r := mtproto.MakeTLMessagesChannelMessages(&mtproto.Messages_Messages{
		Pts:      mc.GetChannel().GetPts(),
		Messages: []*mtproto.Message{},
		Topics:   []*mtproto.ForumTopic{},
		Chats:    []*mtproto.Chat{},
		Users:    []*mtproto.User{},
	}).To_Messages_Messages()
	if !mc.GetChannel().GetBroadcast() {
		topics := map[int32]bool{}
		r.Messages, err = pickRecentLocations(limit, now, func(offset int32) ([]locationCandidate, error) {
			found, err := c.svcCtx.Dao.Channel.ChannelSearch(c.ctx, &channelpb.TLChannelSearch{
				SelfId:    c.MD.UserId,
				ChannelId: peer.PeerId,
				MediaType: mtproto.MEDIA_GEO_LIVE,
				OffsetId:  offset,
				Limit:     recentLocationsPage,
			})
			if err != nil {
				return nil, err
			}
			// The answer is as of the pts of the first read: a later one
			// could count an edit the earlier pages do not carry, and a
			// client would not ask for it again.
			if offset == math.MaxInt32 {
				r.Pts = found.GetPts()
			}
			for _, t := range found.GetTopics() {
				if !topics[t.GetId()] {
					topics[t.GetId()] = true
					r.Topics = append(r.Topics, t)
				}
			}
			page := make([]locationCandidate, 0, len(found.GetMessages()))
			for _, m := range found.GetMessages() {
				page = append(page, locationCandidate{sender: channelSender(peer.PeerId, m), message: m})
			}
			return page, nil
		})
		if err != nil {
			return nil, err
		}
	}
	r.Count = int32(len(r.Messages))
	if err = c.fillChannelPeers(r, peer.PeerId); err != nil {
		return nil, err
	}
	return r, nil
}

// channelSender is who sent m in the supergroup: a user by his id, a
// channel (the group itself for an anonymous admin, or a send-as channel)
// by its negated id.
func channelSender(channelId int64, m *mtproto.Message) int64 {
	from := m.GetFromId()
	switch from.GetPredicateName() {
	case mtproto.Predicate_peerUser:
		return from.GetUserId()
	case mtproto.Predicate_peerChannel:
		return -from.GetChannelId()
	}
	return -channelId
}

// locationCandidate is a message with a live location and who sent it.
type locationCandidate struct {
	sender  int64
	message *mtproto.Message
}

// pickRecentLocations reads the messages with a live location newest first,
// a page at a time from fetch (below the message id offset,
// math.MaxInt32 for the newest), and keeps the newest live one of each
// sender, up to limit. It reads on while the messages are of the last day;
// past it, it stops after recentLocationsScanMax messages in all.
func pickRecentLocations(limit, now int32, fetch func(offset int32) ([]locationCandidate, error)) ([]*mtproto.Message, error) {
	var (
		out     = []*mtproto.Message{}
		senders = map[int64]bool{}
		scanned = 0
		offset  = int32(math.MaxInt32)
	)
	for {
		page, err := fetch(offset)
		if err != nil {
			return nil, err
		}
		for _, c := range page {
			scanned++
			if senders[c.sender] || !liveLocationNow(c.message, now) {
				continue
			}
			senders[c.sender] = true
			out = append(out, c.message)
			if int32(len(out)) >= limit {
				return out, nil
			}
		}
		if len(page) < recentLocationsPage {
			return out, nil
		}
		oldest := page[len(page)-1].message
		if oldest.GetId() <= 0 || oldest.GetId() >= offset {
			// A page that does not go back would be read again forever.
			return out, nil
		}
		if scanned >= recentLocationsScanMax && int64(oldest.GetDate()) < int64(now)-recentLocationsDay {
			return out, nil
		}
		offset = oldest.GetId()
	}
}

// liveLocationNow tells whether m shares a live location at now.
func liveLocationNow(m *mtproto.Message, now int32) bool {
	media := m.GetMedia()
	if m.GetPredicateName() != mtproto.Predicate_message || media.GetPredicateName() != mtproto.Predicate_messageMediaGeoLive {
		return false
	}
	period := media.GetPeriod()
	return period == liveLocationIndefinite || int64(m.GetDate())+int64(period) > int64(now)
}

// recentLocationsHash is the hash of an answer as core.telegram.org/api/
// offsets counts one: for each message in order its id and its edit_date
// (0 when never edited).
func recentLocationsHash(messages []*mtproto.Message) int64 {
	var h uint64
	update := func(v int64) {
		h ^= h >> 21
		h ^= h << 35
		h ^= h >> 4
		h += uint64(v)
	}
	for _, m := range messages {
		update(int64(m.GetId()))
		update(int64(m.GetEditDate().GetValue()))
	}
	return int64(h)
}
