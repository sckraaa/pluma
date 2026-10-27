/*
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package core

import (
	"math/rand"
	"slices"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// ChatCreateChat2
// chat.createChat2 flags:# creator_id:long user_id_list:Vector<long> title:string bots:flags.0?Vector<long> ttl_period:flags.1?int = MutableChat;
//
// The chat, its participants and the creator's permanent invite link are
// written in the creator's box transaction (ADR 0021), which also serializes
// the creator's chats for the flood limit. The service message that bff
// sends tells the members.
func (c *ChatCore) ChatCreateChat2(in *chat.TLChatCreateChat2) (*mtproto.MutableChat, error) {
	creatorId := in.CreatorId
	var users, bots []int64
	for _, id := range in.UserIdList {
		if id != creatorId && !slices.Contains(users, id) {
			users = append(users, id)
		}
	}
	for _, id := range in.Bots {
		if id != creatorId && !slices.Contains(users, id) && !slices.Contains(bots, id) {
			bots = append(bots, id)
		}
	}
	if 1+len(users)+len(bots) > maxChatMembers {
		err := mtproto.ErrUsersTooMuch
		c.Logger.Errorf("chat.createChat2 - error: %v", err)
		return nil, err
	}

	var created *dao.LockedChat
	d := c.svcCtx.Dao
	_, err := d.RunBox(c.ctx, creatorId, nil, func(b *userbox.Box) error {
		created = nil
		now := b.Date()
		last, err := d.LastChatOfTx(b, creatorId)
		if err != nil {
			return err
		}
		if last != nil && now-last.Date < createChatFlood {
			return mtproto.NewErrFloodWaitX(int32(createChatFlood - (now - last.Date)))
		}

		chatsDO := &dataobject.ChatsDO{
			CreatorUserId:       creatorId,
			AccessHash:          rand.Int63(), //nolint:gosec // a basic group has no access hash in the API (inputPeerChat)
			ParticipantCount:    int32(1 + len(users) + len(bots)),
			Title:               in.Title,
			DefaultBannedRights: int64(mtproto.MakeDefaultBannedRights().ToBannedRights()),
			Date:                now,
		}
		if chatsDO.Id, _, err = d.ChatsDAO.InsertTx(b.Tx(), chatsDO); err != nil {
			return err
		}
		if ttl := in.GetTtlPeriod().GetValue(); ttl != 0 {
			if _, err = d.ChatsDAO.UpdateTTLPeriodTx(b.Tx(), ttl, chatsDO.Id); err != nil {
				return err
			}
		}

		link := chat.GenChatInviteHash()
		participants := []*dataobject.ChatParticipantsDO{{
			ChatId:          chatsDO.Id,
			UserId:          creatorId,
			ParticipantType: mtproto.ChatMemberCreator,
			Link:            link,
			InvitedAt:       now,
			Date2:           now,
		}}
		member := func(id int64, bot bool) *dataobject.ChatParticipantsDO {
			return &dataobject.ChatParticipantsDO{
				ChatId:          chatsDO.Id,
				UserId:          id,
				ParticipantType: mtproto.ChatMemberNormal,
				InviterUserId:   creatorId,
				InvitedAt:       now,
				Date2:           now,
				IsBot:           bot,
			}
		}
		for _, id := range users {
			participants = append(participants, member(id, false))
		}
		for _, id := range bots {
			participants = append(participants, member(id, true))
		}
		if _, _, err = d.ChatParticipantsDAO.InsertBulkTx(b.Tx(), participants); err != nil {
			return err
		}
		if _, _, err = d.ChatInvitesDAO.InsertTx(b.Tx(), &dataobject.ChatInvitesDO{
			ChatId:    chatsDO.Id,
			AdminId:   creatorId,
			Link:      link,
			Permanent: true,
			Date2:     now,
		}); err != nil {
			return err
		}
		// The chat as written, its participants with their ids.
		created, err = d.LockChat(b, chatsDO.Id)
		return err
	})
	if err != nil {
		c.Logger.Errorf("chat.createChat2 - error: %v", err)
		return nil, err
	}
	created.Chat.Photo = mtproto.MakeTLPhotoEmpty(nil).To_Photo()
	return created.MutableChat, nil
}
