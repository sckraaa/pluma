// Copyright 2025 Teamgram Authors
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
	"math/rand"
	"time"

	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"google.golang.org/grpc/status"
)

var (
	// errBirthdayAlready: "The target user already has a birthday set"
	// (core.telegram.org/method/users.suggestBirthday).
	errBirthdayAlready = status.Error(mtproto.ErrBadRequest, "BIRTHDAY_ALREADY")
	// errBirthdayInvalid: "An invalid age was specified, must be between 0
	// and 150 years" (core.telegram.org/method/account.updateBirthday).
	errBirthdayInvalid = status.Error(mtproto.ErrBadRequest, "BIRTHDAY_INVALID")
)

// birthdayAgeMax is the oldest a birthday may make its owner
// (BIRTHDAY_INVALID: "must be between 0 and 150 years").
const birthdayAgeMax = 150

// validBirthday tells whether b is a day of the calendar, with a year or
// without one, not after now and at most birthdayAgeMax years before it.
// The 29th of February without a year is a day.
func validBirthday(b *mtproto.Birthday, now time.Time) bool {
	day, month := int(b.GetDay()), int(b.GetMonth())
	if b == nil || month < 1 || month > 12 || day < 1 {
		return false
	}
	if b.GetYear() == nil {
		// 2000 is a leap year.
		return day <= time.Date(2000, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day()
	}
	year := int(b.GetYear().GetValue())
	if year < 1 || day > time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day() {
		return false
	}
	date := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return !date.After(today) && !date.Before(today.AddDate(-birthdayAgeMax, 0, 0))
}

// UsersSuggestBirthday
// users.suggestBirthday#fc533372 id:InputUser birthday:Birthday = Updates;
//
// Suggests a birthday to a user who has none: a service message of the
// private chat with them (messageActionSuggestBirthday), which their
// client shows with a button that sets the date (tdesktop
// history_item.cpp:8402-8416 makes a gift box of it, Android offers it on
// the page of a contact, ContactAddActivity.java:476-490). Nothing is set
// here: the user accepts with account.updateBirthday.
//
// The errors are those of core.telegram.org/method/users.suggestBirthday:
// USER_ID_INVALID for nobody, the account itself, a bot or a deleted
// account, BIRTHDAY_ALREADY for a user who has a birthday; a date that is
// none is BIRTHDAY_INVALID, as account.updateBirthday answers. What may be
// written to the user is checked by msg, as for any message.
func (c *UserChannelProfilesCore) UsersSuggestBirthday(in *mtproto.TLUsersSuggestBirthday) (*mtproto.Updates, error) {
	if in.GetId() == nil {
		err := mtproto.ErrUserIdInvalid
		c.Logger.Errorf("users.suggestBirthday - error: %v", err)
		return nil, err
	}
	peer := mtproto.FromInputUser(c.MD.UserId, in.GetId())
	if peer.PeerType != mtproto.PEER_USER || peer.PeerId == c.MD.UserId {
		err := mtproto.ErrUserIdInvalid
		c.Logger.Errorf("users.suggestBirthday - error: %v", err)
		return nil, err
	}
	if !validBirthday(in.GetBirthday(), time.Now()) {
		err := errBirthdayInvalid
		c.Logger.Errorf("users.suggestBirthday - error: %v", err)
		return nil, err
	}
	to, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{
		Id: peer.PeerId,
	})
	if err != nil {
		// Nobody with this id is USER_ID_INVALID of biz user itself.
		c.Logger.Errorf("users.suggestBirthday - user %d: %v", peer.PeerId, err)
		return nil, err
	}
	if to == nil || to.Deleted() || to.IsBot() {
		err = mtproto.ErrUserIdInvalid
		c.Logger.Errorf("users.suggestBirthday - user %d: %v", peer.PeerId, err)
		return nil, err
	}
	if to.Birthday() != nil {
		err = errBirthdayAlready
		c.Logger.Errorf("users.suggestBirthday - error: %v", err)
		return nil, err
	}

	birthday := &mtproto.Birthday{Day: in.GetBirthday().GetDay(), Month: in.GetBirthday().GetMonth()}
	if y := in.GetBirthday().GetYear(); y != nil {
		birthday.Year = y
	}
	updates, err := c.svcCtx.Dao.MsgClient.MsgSendMessageV2(c.ctx, &msgpb.TLMsgSendMessageV2{
		UserId:    c.MD.UserId,
		AuthKeyId: c.MD.PermAuthKeyId,
		PeerType:  mtproto.PEER_USER,
		PeerId:    peer.PeerId,
		Message: []*msgpb.OutboxMessage{
			msgpb.MakeTLOutboxMessage(&msgpb.OutboxMessage{
				NoWebpage: true,
				RandomId:  rand.Int63(), //nolint:gosec // random_id of a service message, not a secret
				Message: mtproto.MakeTLMessageService(&mtproto.Message{
					Out:    true,
					FromId: mtproto.MakePeerUser(c.MD.UserId),
					PeerId: mtproto.MakePeerUser(peer.PeerId),
					Date:   int32(time.Now().Unix()),
					Action: mtproto.MakeTLMessageActionSuggestBirthday(&mtproto.MessageAction{
						Birthday: mtproto.MakeTLBirthday(birthday).To_Birthday(),
					}).To_MessageAction(),
				}).To_Message(),
			}).To_OutboxMessage(),
		},
	})
	if err != nil {
		c.Logger.Errorf("users.suggestBirthday - error: %v", err)
		return nil, err
	}
	return updates, nil
}
