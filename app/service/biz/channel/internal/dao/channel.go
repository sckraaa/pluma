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

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/app/service/media/media"
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
	return channel.MakeTLMutableChannel(&channel.MutableChannel{
		Channel:      channels[0],
		Participants: participants,
	}).To_MutableChannel(), nil
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
	usernames := make(map[int64]string, len(names))
	for _, n := range names {
		usernames[n.PeerId] = n.Username
	}

	byId := make(map[int64]*channel.ImmutableChannel, len(dos))
	for i := range dos {
		c := makeChannel(&dos[i], usernames[dos[i].Id])
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

func makeChannel(do *dataobject.ChannelsDO, username string) *channel.ImmutableChannel {
	return channel.MakeTLImmutableChannel(&channel.ImmutableChannel{
		Broadcast:           do.Broadcast,
		Megagroup:           do.Megagroup,
		Forum:               do.Forum,
		Signatures:          do.Signatures,
		Noforwards:          do.Noforwards,
		JoinToSend:          do.JoinToSend,
		JoinRequest:         do.JoinRequest,
		ParticipantsHidden:  do.ParticipantsHidden,
		HiddenPrehistory:    do.HiddenPrehistory,
		Deleted:             do.Deleted,
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
		Date:           do.Date,
	}).To_ImmutableChannelParticipant()
}
