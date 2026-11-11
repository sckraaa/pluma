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

package dao

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/app/service/media/media"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// GetMutableChannel returns the channel channelId with the participant rows
// of the users of idList that have one (a user who never joined has none).
// A channel that does not exist is mtproto.ErrChannelInvalid.
func (d *Dao) GetMutableChannel(ctx context.Context, channelId int64, idList []int64) (*channel.MutableChannel, error) {
	channels, err := d.getChannels(ctx, []int64{channelId})
	if err != nil {
		return nil, err
	}
	if len(channels) == 0 {
		return nil, mtproto.ErrChannelInvalid
	}
	dos, err := d.ChannelParticipantsDAO.SelectListByUserIdList(ctx, channelId, uniqueIds(idList))
	if err != nil {
		return nil, fmt.Errorf("channel %d: participants: %w", channelId, err)
	}
	participants := make([]*channel.ImmutableChannelParticipant, 0, len(dos))
	for i := range dos {
		participants = append(participants, makeParticipant(&dos[i]))
	}
	if participants, err = d.withLinkedReaders(ctx, channels[0], uniqueIds(idList), participants); err != nil {
		return nil, err
	}
	return channel.MakeTLMutableChannel(&channel.MutableChannel{
		Channel:      channels[0],
		Participants: participants,
	}).To_MutableChannel(), nil
}

// withLinkedReaders adds to participants, for each user of ids that is not
// in the discussion group ch but in its channel, a participant in
// StateLinked: the user reads the group through the channel (task 048).
// A user kicked from the group stays kicked.
func (d *Dao) withLinkedReaders(ctx context.Context, ch *channel.ImmutableChannel, ids []int64, participants []*channel.ImmutableChannelParticipant) ([]*channel.ImmutableChannelParticipant, error) {
	if !ch.GetMegagroup() || ch.GetLinkedChatId() == 0 || len(ids) == 0 {
		return participants, nil
	}
	rows := map[int64]int{}
	for i, p := range participants {
		rows[p.GetUserId()] = i
	}
	var outside []int64
	for _, id := range ids {
		i, ok := rows[id]
		if !ok || participants[i].GetState() == channel.StateLeft {
			outside = append(outside, id)
		}
	}
	if len(outside) == 0 {
		return participants, nil
	}
	args := []any{ch.GetLinkedChatId(), ch.GetId()}
	for _, id := range outside {
		args = append(args, id)
	}
	var readers []int64
	if err := d.Mysql.QueryRowsPartial(ctx, &readers,
		"SELECT p.user_id FROM channels b JOIN channel_participants p ON p.channel_id = b.id AND p.state = 0"+
			" WHERE b.id = ? AND b.broadcast = 1 AND b.deleted = 0 AND b.linked_chat_id = ? AND p.user_id IN (?"+strings.Repeat(", ?", len(outside)-1)+")",
		args...); err != nil {
		return nil, fmt.Errorf("channel %d: readers through %d: %w", ch.GetId(), ch.GetLinkedChatId(), err)
	}
	for _, id := range readers {
		linked := channel.MakeTLImmutableChannelParticipant(&channel.ImmutableChannelParticipant{
			ChannelId: ch.GetId(), UserId: id, State: channel.StateLinked,
		}).To_ImmutableChannelParticipant()
		if i, ok := rows[id]; ok {
			// The row of one who writes through the channel keeps its last
			// send under the slow mode.
			linked.LastSendDate = participants[i].GetLastSendDate()
			participants[i] = linked
		} else {
			participants = append(participants, linked)
		}
	}
	return participants, nil
}

// GetChannelListByIdList returns the channels of idList that exist, in its
// order, each with the participant row of selfId when it has one.
func (d *Dao) GetChannelListByIdList(ctx context.Context, selfId int64, idList []int64) ([]*channel.MutableChannel, error) {
	channels, err := d.getChannels(ctx, uniqueIds(idList))
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(channels))
	for _, c := range channels {
		ids = append(ids, c.Id)
	}
	dos, err := d.ChannelParticipantsDAO.SelectListByChannelIdList(ctx, selfId, ids)
	if err != nil {
		return nil, fmt.Errorf("participant %d of channels %v: %w", selfId, ids, err)
	}
	mine := make(map[int64]*channel.ImmutableChannelParticipant, len(dos))
	for i := range dos {
		mine[dos[i].ChannelId] = makeParticipant(&dos[i])
	}
	out := make([]*channel.MutableChannel, 0, len(channels))
	for _, c := range channels {
		participants := []*channel.ImmutableChannelParticipant{}
		if p, ok := mine[c.Id]; ok {
			participants = append(participants, p)
		}
		if participants, err = d.withLinkedReaders(ctx, c, []int64{selfId}, participants); err != nil {
			return nil, err
		}
		out = append(out, channel.MakeTLMutableChannel(&channel.MutableChannel{
			Channel:      c,
			Participants: participants,
		}).To_MutableChannel())
	}
	return out, nil
}

// getChannels reads the channels of idList that exist, in its order, with
// their public usernames and photos.
func (d *Dao) getChannels(ctx context.Context, idList []int64) ([]*channel.ImmutableChannel, error) {
	if len(idList) == 0 {
		return []*channel.ImmutableChannel{}, nil
	}
	dos, err := d.ChannelsDAO.SelectByIdList(ctx, idList)
	if err != nil {
		return nil, fmt.Errorf("channels %v: %w", idList, err)
	}
	found := make([]int64, 0, len(dos))
	for i := range dos {
		found = append(found, dos[i].Id)
	}
	names, err := d.ChannelUsernameDAO.SelectListByChannelIdList(ctx, found)
	if err != nil {
		return nil, fmt.Errorf("usernames of channels %v: %w", found, err)
	}
	usernames := make(map[int64][]dataobject.ChannelUsernameDO, len(found))
	for _, n := range names {
		usernames[n.PeerId] = append(usernames[n.PeerId], n)
	}

	byId := make(map[int64]*channel.ImmutableChannel, len(dos))
	for i := range dos {
		c := makeChannel(&dos[i], usernames[dos[i].Id])
		c.AvailableReactionsType = dos[i].AvailableReactionsType
		if dos[i].AvailableReactions.Valid {
			if err = jsonx.UnmarshalFromString(dos[i].AvailableReactions.String, &c.AvailableReactions); err != nil {
				return nil, fmt.Errorf("reactions of channel %d: %w", dos[i].Id, err)
			}
		}
		if dos[i].PhotoId != 0 {
			if c.Photo, err = d.Photos.MediaGetPhoto(ctx, &media.TLMediaGetPhoto{PhotoId: dos[i].PhotoId}); err != nil {
				return nil, fmt.Errorf("photo %d of channel %d: %w", dos[i].PhotoId, dos[i].Id, err)
			}
		}
		byId[c.Id] = c
	}
	out := make([]*channel.ImmutableChannel, 0, len(byId))
	for _, id := range idList {
		if c, ok := byId[id]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}

// uniqueIds is idList without zeros and repeats, in its order.
func uniqueIds(idList []int64) []int64 {
	out := make([]int64, 0, len(idList))
	for _, id := range idList {
		if id != 0 && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// makeChannel is the channel of its row and its rows of username, in their
// order: the editable one is its public link, and with others they are all
// listed (channel.usernames).
func makeChannel(do *dataobject.ChannelsDO, names []dataobject.ChannelUsernameDO) *channel.ImmutableChannel {
	var (
		username  string
		usernames []*mtproto.Username
	)
	for _, n := range names {
		if n.Editable {
			username = n.Username
		}
		usernames = append(usernames, mtproto.MakeTLUsername(&mtproto.Username{
			Editable: n.Editable,
			Active:   n.Active,
			Username: n.Username,
		}).To_Username())
	}
	return channel.MakeTLImmutableChannel(&channel.ImmutableChannel{
		Broadcast:           do.Broadcast,
		Megagroup:           do.Megagroup,
		Gigagroup:           do.Gigagroup,
		Forum:               do.Forum,
		ForumTabs:           do.ForumTabs,
		ForumSidebar:        do.ForumSidebar,
		Signatures:          do.Signatures,
		SignatureProfiles:   do.SignatureProfiles,
		Noforwards:          do.Noforwards,
		JoinToSend:          do.JoinToSend,
		JoinRequest:         do.JoinRequest,
		ParticipantsHidden:  do.ParticipantsHidden,
		HiddenPrehistory:    do.HiddenPrehistory,
		Deleted:             do.Deleted,
		Blocked:             do.Blocked,
		Id:                  do.Id,
		AccessHash:          do.AccessHash,
		CreatorUserId:       do.CreatorUserId,
		Title:               do.Title,
		About:               do.About,
		Photo:               mtproto.MakeTLPhotoEmpty(nil).To_Photo(),
		Username:            username,
		ParticipantsCount:   do.ParticipantsCount,
		AdminsCount:         do.AdminsCount,
		KickedCount:         do.KickedCount,
		BannedCount:         do.BannedCount,
		DefaultBannedRights: do.DefaultBannedRights,
		SlowmodeSeconds:     do.SlowmodeSeconds,
		LinkedChatId:        do.LinkedChatId,
		MigratedFromChatId:  do.MigratedFromChatId,
		MigratedFromMaxId:   do.MigratedFromMaxId,
		Pts:                 do.Pts,
		TopMessage:          do.TopMessage,
		Version:             do.Version,
		Date:                do.Date,
		Color:               channel.MakePeerColor(do.Color, do.ColorBackgroundEmojiId),
		ProfileColor:        channel.MakePeerColor(do.ProfileColor, do.ProfileColorBackgroundEmojiId),
		EmojiStatus:         channel.MakeEmojiStatus(do.EmojiStatusDocumentId, do.EmojiStatusUntil),
		Usernames:           usernames,
		MainTab:             do.MainTab,
	}).To_ImmutableChannel()
}

func makeParticipant(do *dataobject.ChannelParticipantsDO) *channel.ImmutableChannelParticipant {
	return channel.MakeTLImmutableChannelParticipant(&channel.ImmutableChannelParticipant{
		ChannelId:      do.ChannelId,
		UserId:         do.UserId,
		Role:           do.Role,
		State:          do.State,
		AdminRights:    do.AdminRights,
		Rank:           do.Rank,
		BannedRights:   do.BannedRights,
		UntilDate:      do.UntilDate,
		InviterUserId:  do.InviterUserId,
		PromotedBy:     do.PromotedBy,
		KickedBy:       do.KickedBy,
		AvailableMinId: do.AvailableMinId,
		LastSendDate:   do.LastSendDate,
		Date:           do.Date,
	}).To_ImmutableChannelParticipant()
}
