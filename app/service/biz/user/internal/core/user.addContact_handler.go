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
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// UserAddContact
// user.addContact user_id:long add_phone_privacy_exception:Bool id:long first_name:string last_name:string phone:string users:Vector<User> = Bool;
//
// The contact, the phone number exception and their updates with seq for
// every device of the account are one box transaction of the user (ADR
// 0020): updatePeerSettings of the contact when its row changed, and
// updatePrivacy of the phone number when the exception changed the rules.
// users are the contact as the user saw it before, for the pushes; the
// transaction makes it a contact with the mutuality and the name it
// writes. The answer tells whether the contact is mutual.
func (c *UserCore) UserAddContact(in *user.TLUserAddContact) (*mtproto.Bool, error) {
	if in.GetId() <= 0 || in.GetId() == in.GetUserId() {
		err := mtproto.ErrContactIdInvalid
		c.Logger.Errorf("user.addContact - error: %v", err)
		return nil, err
	}
	contact := dao.ImportedContact{
		UserId:    in.GetId(),
		Phone:     in.GetPhone(),
		FirstName: in.GetFirstName(),
		LastName:  in.GetLastName(),
	}

	var mutual bool
	_, err := c.svcCtx.Dao.RunBox(c.ctx, in.GetUserId(), dao.ContactCacheKeys(in.GetUserId(), in.GetId()), func(b *userbox.Box) error {
		changes, err := dao.PutContacts(b, []dao.ImportedContact{contact})
		if err != nil {
			return err
		}
		mutual = changes[0].Mutual
		users := dao.AsContact(in.GetUsers(), in.GetId(), &contact, mutual)
		if changes[0].Changed {
			if err = c.svcCtx.Dao.EmitContactSettings(b, []int64{in.GetId()}, func(int64) []*mtproto.User { return users }); err != nil {
				return err
			}
		}
		if !mtproto.FromBool(in.GetAddPhonePrivacyException()) {
			return nil
		}
		rules, err := dao.AddPhonePrivacyException(b, in.GetId())
		if err != nil || rules == nil {
			return err
		}
		return c.svcCtx.Dao.EmitSeqUpdate(b, mtproto.MakeTLUpdatePrivacy(&mtproto.Update{
			Key:   mtproto.ToPrivacyKey(mtproto.PHONE_NUMBER),
			Rules: rules,
		}).To_Update(), users, nil)
	})
	if err != nil {
		c.Logger.Errorf("user.addContact - user %d: %v", in.GetUserId(), err)
		return nil, err
	}

	return mtproto.ToBool(mutual), nil
}
