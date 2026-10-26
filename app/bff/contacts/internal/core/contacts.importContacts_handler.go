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
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// ContactsImportContacts
// contacts.importContacts#2c800be5 contacts:Vector<InputContact> = contacts.ImportedContacts;
//
// The phone book sync (user.importContacts, task 011): imported holds the
// contacts the numbers found, with the client_id of each number;
// popular_invites the numbers to invite, with how many users imported each;
// retry_contacts what to send again later (the limits). Every device of
// the importer learns about added and renamed contacts as from
// contacts.addContact: biz pushes updatePeerSettings with seq of the peer
// and the user with its contact flags in the transaction of the import
// (ADR 0020; Android ContactsController applies contacts from these).
func (c *ContactsCore) ContactsImportContacts(in *mtproto.TLContactsImportContacts) (*mtproto.Contacts_ImportedContacts, error) {
	result := mtproto.MakeTLContactsImportedContacts(&mtproto.Contacts_ImportedContacts{
		Imported:       []*mtproto.ImportedContact{},
		PopularInvites: []*mtproto.PopularContact{},
		RetryContacts:  []int64{},
		Users:          []*mtproto.User{},
	}).To_Contacts_ImportedContacts()
	if len(in.GetContacts()) == 0 {
		return result, nil
	}

	imported, err := c.svcCtx.Dao.UserClient.UserImportContacts(c.ctx, &userpb.TLUserImportContacts{
		UserId:   c.MD.UserId,
		Contacts: in.GetContacts(),
	})
	if err != nil {
		c.Logger.Errorf("contacts.importContacts - error: %v", err)
		return nil, err
	}
	result.Imported = append(result.Imported, imported.GetImported()...)
	result.PopularInvites = append(result.PopularInvites, imported.GetPopularInvites()...)
	result.RetryContacts = append(result.RetryContacts, imported.GetRetryContacts()...)
	result.Users = append(result.Users, imported.GetUsers()...)

	return result, nil
}
