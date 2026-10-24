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
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// ContactsAcceptContact
// contacts.acceptContact#f831a20f id:InputUser = Updates;
//
// The "share my phone number" action bar (core.telegram.org/api/action-bar):
// the user id added us as a contact without our number, and we accept the
// contact request — id becomes our contact, if not yet, and sees our phone
// number (an exception of our phone number privacy, as
// add_phone_privacy_exception of contacts.addContact). tdesktop calls it
// from the bar's button (ContactStatus::setupShareHandler), Android from
// ChatActivity.shareMyContact. Without the request: CONTACT_REQ_MISSING.
func (c *ContactsCore) ContactsAcceptContact(in *mtproto.TLContactsAcceptContact) (*mtproto.Updates, error) {
	id := mtproto.FromInputUser(c.MD.UserId, in.GetId())
	if !id.IsUser() || id.PeerId == c.MD.UserId {
		err := mtproto.ErrContactIdInvalid
		c.Logger.Errorf("contacts.acceptContact - error: %v", err)
		return nil, err
	}

	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{
		Id: []int64{c.MD.UserId, id.PeerId},
		To: []int64{c.MD.UserId},
	})
	if err != nil {
		c.Logger.Errorf("contacts.acceptContact - error: %v", err)
		return nil, err
	}
	me, _ := users.GetImmutableUser(c.MD.UserId)
	peer, _ := users.GetImmutableUser(id.PeerId)
	if me == nil || peer == nil || peer.Deleted() {
		err = mtproto.ErrContactIdInvalid
		c.Logger.Errorf("contacts.acceptContact - error: %v", err)
		return nil, err
	}
	if requested, _ := peer.CheckContact(c.MD.UserId); !requested {
		err = mtproto.ErrContactReqMissing
		c.Logger.Errorf("contacts.acceptContact - error: %v", err)
		return nil, err
	}

	// A contact keeps the name and the phone it has; a new one gets the
	// user's own name.
	firstName, lastName, phone := peer.FirstName(), peer.LastName(), ""
	if contact := me.GetContactData(id.PeerId); contact != nil {
		firstName = contact.GetFirstName().GetValue()
		lastName = contact.GetLastName().GetValue()
		phone = contact.GetPhone().GetValue()
	}
	if _, err = c.svcCtx.Dao.UserClient.UserAddContact(c.ctx, &userpb.TLUserAddContact{
		UserId:                   c.MD.UserId,
		AddPhonePrivacyException: mtproto.BoolTrue,
		Id:                       id.PeerId,
		FirstName:                firstName,
		LastName:                 lastName,
		Phone:                    phone,
	}); err != nil {
		c.Logger.Errorf("contacts.acceptContact - error: %v", err)
		return nil, err
	}

	users, err = c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{
		Id: []int64{c.MD.UserId, id.PeerId},
		To: []int64{c.MD.UserId},
	})
	if err != nil {
		c.Logger.Errorf("contacts.acceptContact - error: %v", err)
		return nil, err
	}
	contact, err := users.GetUnsafeUser(c.MD.UserId, id.PeerId)
	if err != nil {
		c.Logger.Errorf("contacts.acceptContact - error: %v", err)
		return nil, err
	}

	// The bar is gone: our phone is shared, the user is our contact.
	rUpdates := mtproto.MakeUpdatesByUpdatesUsers(
		[]*mtproto.User{contact},
		mtproto.MakeTLUpdatePeerSettings(&mtproto.Update{
			Peer_PEER: id.ToPeer(),
			Settings:  mtproto.MakeTLPeerSettings(&mtproto.PeerSettings{}).To_PeerSettings(),
		}).To_Update())
	if _, err = c.svcCtx.Dao.SyncClient.SyncUpdatesNotMe(c.ctx, &sync.TLSyncUpdatesNotMe{
		UserId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		Updates:       rUpdates,
	}); err != nil {
		// Committed: the other devices get the contact with their next
		// contacts.getContacts.
		c.Logger.Errorf("contacts.acceptContact - push to the other devices of user %d: %v", c.MD.UserId, err)
	}

	return rUpdates, nil
}
