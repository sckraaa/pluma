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
	"time"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"google.golang.org/grpc/status"
)

// errContactMissing: "The specified user is not a contact"
// (core.telegram.org/method/contacts.updateContactNote).
var errContactMissing = status.Error(mtproto.ErrBadRequest, "CONTACT_MISSING")

// ContactsUpdateContactNote
// contacts.updateContactNote#139f63fb id:InputUser note:TextWithEntities = Bool;
//
// The private note of the account on one of its contacts, which only the
// account sees, in userFull.note (core.telegram.org/api/contacts). An empty
// note removes it: tdesktop deletes a note so
// (info_profile_actions.cpp:787-800). The clients set the note they sent on
// the user themselves and the other devices read it with the full user, so
// nothing is pushed.
func (c *ContactsCore) ContactsUpdateContactNote(in *mtproto.TLContactsUpdateContactNote) (*mtproto.Bool, error) {
	if in.GetId() == nil {
		err := mtproto.ErrContactIdInvalid
		c.Logger.Errorf("contacts.updateContactNote - error: %v", err)
		return nil, err
	}
	id := mtproto.FromInputUser(c.MD.UserId, in.GetId())
	if !id.IsUser() || id.IsSelf() || id.PeerId == c.MD.UserId {
		err := mtproto.ErrContactIdInvalid
		c.Logger.Errorf("contacts.updateContactNote - error: %v", err)
		return nil, err
	}
	if err := c.checkContact(id.PeerId); err != nil {
		c.Logger.Errorf("contacts.updateContactNote - error: %v", err)
		return nil, err
	}
	if err := c.svcCtx.Dao.Notes.Set(c.ctx, c.MD.UserId, id.PeerId, in.GetNote(), time.Now().Unix()); err != nil {
		c.Logger.Errorf("contacts.updateContactNote - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

// checkContact answers CONTACT_ID_INVALID for a user that does not exist
// and CONTACT_MISSING for one that is not a contact of the account.
func (c *ContactsCore) checkContact(userId int64) error {
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      []int64{c.MD.UserId, userId},
		Privacy: true,
		HasTo:   true,
		To:      []int64{c.MD.UserId},
	})
	if err != nil {
		return err
	}
	me, ok := users.GetImmutableUser(c.MD.UserId)
	contact, ok2 := users.GetImmutableUser(userId)
	if !ok || !ok2 || contact.Deleted() {
		return mtproto.ErrContactIdInvalid
	}
	if contact.ContactRowOf(me) == nil {
		return errContactMissing
	}
	return nil
}
