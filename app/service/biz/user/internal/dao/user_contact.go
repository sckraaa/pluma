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

package dao

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/teamgram/marmota/pkg/container2"
	"github.com/teamgram/marmota/pkg/stores/sqlc"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"github.com/zeromicro/go-zero/core/mr"
	"google.golang.org/protobuf/proto"
)

const (
	contactKeyPrefix = "user_contact.2#"
)

var (
	GenContactCacheKey = genContactCacheKey
)

func genContactCacheKey(selfId, contactId int64) string {
	return fmt.Sprintf("%s%d_%d", contactKeyPrefix, selfId, contactId)
}

func isContactCacheKey(k string) bool {
	return strings.HasPrefix(k, contactKeyPrefix)
}

func parseContactCacheKey(k string) (int64, int64) {
	if strings.HasPrefix(k, contactKeyPrefix) {
		v := strings.Split(k[len(contactKeyPrefix):], "_")
		if len(v) != 2 {
			return 0, 0
		}
		v0, _ := strconv.ParseInt(v[0], 10, 64)
		v1, _ := strconv.ParseInt(v[1], 10, 64)

		return v0, v1
	}

	return 0, 0
}

func (d *Dao) GetUserContactList(ctx context.Context, id int64) []*mtproto.ContactData {
	cacheUserData := d.GetCacheUserData(ctx, id)
	if len(cacheUserData.GetContactIdList()) == 0 {
		return nil
	}

	return d.getContactListByIdList(ctx, id, cacheUserData.GetContactIdList())
}

func (d *Dao) GetUserContact(ctx context.Context, id, contactId int64) *mtproto.ContactData {
	contacts := d.GetUserContactListByIdList(ctx, id, contactId)
	if len(contacts) == 0 {
		return nil
	}

	return contacts[0]
}

func (d *Dao) GetUserContactListByIdList(ctx context.Context, id int64, contactId ...int64) []*mtproto.ContactData {
	cacheUserData := d.GetCacheUserData(ctx, id)
	idList := cacheUserData.GetContactIdList()
	if len(idList) == 0 {
		return nil
	}

	idList2 := make([]int64, 0, len(idList))
	for _, id2 := range contactId {
		if ok := container2.ContainsInt64(idList, id2); ok {
			idList2 = append(idList2, id2)
		}
	}
	if len(idList2) == 0 {
		return nil
	}

	return d.getContactListByIdList(ctx, id, idList2)
}

// DeleteContact removes contactId from the box owner's contacts, in b's
// transaction, with the mutuality of the contact's row pointing back. It
// tells whether there was a contact to remove; without one it writes
// nothing. The cache entries are ContactCacheKeys.
func (d *Dao) DeleteContact(b *userbox.Box, contactId int64) (bool, error) {
	rows, err := ContactRows(b, []int64{contactId})
	if err != nil {
		return false, err
	}
	if r, ok := rows[contactId]; !ok || r.IsDeleted {
		return false, nil
	}
	if _, err = d.UserContactsDAO.DeleteContactsTx(b.Tx(), b.UserId(), []int64{contactId}); err != nil {
		return false, err
	}
	if _, err = d.UserContactsDAO.UpdateMutualTx(b.Tx(), false, contactId, b.UserId()); err != nil {
		return false, err
	}
	return true, nil
}

// AsContact returns users with the user contactId among them as the box
// owner's contact after a change of its row: a contact with the mutuality
// and the name of the row, or no contact. users are what the caller saw
// before the change; they are not modified.
func AsContact(users []*mtproto.User, contactId int64, contact *ImportedContact, mutual bool) []*mtproto.User {
	out := make([]*mtproto.User, 0, len(users))
	for _, u := range users {
		if u.GetId() == contactId {
			u = proto.Clone(u).(*mtproto.User)
			u.Contact = contact != nil
			u.MutualContact = contact != nil && mutual
			if contact != nil {
				u.FirstName = mtproto.MakeFlagsString(contact.FirstName)
				u.LastName = mtproto.MakeFlagsString(contact.LastName)
			} else {
				u.CloseFriend = false
				u.StoriesHidden = false
			}
		}
		out = append(out, u)
	}
	return out
}

// ContactCacheKeys are the cache entries a change of the contact rows of
// the pair ownerId, contactId makes stale: the data of both users (their
// contact lists) and both rows.
func ContactCacheKeys(ownerId, contactId int64) []string {
	return []string{
		genCacheUserDataCacheKey(ownerId),
		genCacheUserDataCacheKey(contactId),
		genContactCacheKey(ownerId, contactId),
		genContactCacheKey(contactId, ownerId),
	}
}

func (d *Dao) ClearContactCaches(ctx context.Context, userId int64, contactId ...int64) {
	keys := []string{genCacheUserDataCacheKey(userId)}
	for _, id := range contactId {
		keys = append(keys, genContactCacheKey(userId, id))
		keys = append(keys, genContactCacheKey(id, userId))
	}
	_ = d.CachedConn.DelCache(ctx, keys...)
}

func (d *Dao) GetCloseFriendList(ctx context.Context, id int64) []*mtproto.ContactData {
	cacheUserData := d.GetCacheUserData(ctx, id)
	if len(cacheUserData.GetContactIdList()) == 0 {
		return nil
	}

	return d.getCloseFriendListByIdList(ctx, id, cacheUserData.GetContactIdList())
}

func (d *Dao) getCloseFriendListByIdList(ctx context.Context, id int64, idList []int64) []*mtproto.ContactData {
	closeFriendList := make([]*mtproto.ContactData, len(idList))
	mr.ForEach(
		func(source chan<- interface{}) {
			for i, v := range idList {
				source <- idxId{i, v}
			}
		},
		func(item interface{}) {
			idx := item.(idxId)
			do := new(dataobject.UserContactsDO)
			err := d.CachedConn.QueryRow(
				ctx,
				do,
				genContactCacheKey(id, idx.id),
				func(ctx context.Context, conn *sqlx.DB, v interface{}) error {
					do2, _ := d.UserContactsDAO.SelectContact(ctx, id, idx.id)
					if do2 == nil {
						return sqlc.ErrNotFound
					}
					*v.(*dataobject.UserContactsDO) = *do2
					return nil
				})
			if err == nil && do.CloseFriend {
				closeFriendList[idx.idx] = mtproto.MakeTLContactData(&mtproto.ContactData{
					UserId:        id,
					ContactUserId: do.ContactUserId,
					FirstName:     mtproto.MakeFlagsString(do.ContactFirstName),
					LastName:      mtproto.MakeFlagsString(do.ContactLastName),
					MutualContact: do.Mutual,
					Phone:         mtproto.MakeFlagsString(do.ContactPhone),
					CloseFriend:   do.CloseFriend,
				}).To_ContactData()
			}
		})

	for _, v := range closeFriendList {
		if v == nil {
			// has hole, internal error
			return nil
		}
	}

	return closeFriendList
}

func (d *Dao) getContactListByIdList(ctx context.Context, id int64, idList []int64) []*mtproto.ContactData {
	var (
		cKeys = make([]string, 0, len(idList))
	)

	for _, v := range idList {
		cKeys = append(cKeys, genContactCacheKey(id, v))
	}

	return d.getContactListByKeyList(ctx, cKeys...)
}

func (d *Dao) getReverseContactListByIdList(ctx context.Context, id int64, idList []int64) []*mtproto.ContactData {
	var (
		cKeys = make([]string, 0, len(idList))
	)

	for _, v := range idList {
		cKeys = append(cKeys, genContactCacheKey(v, id))
	}

	return d.getContactListByKeyList(ctx, cKeys...)
}

func (d *Dao) getContactListByKeyList(ctx context.Context, cKeys ...string) []*mtproto.ContactData {
	var (
		contactList = make([]*mtproto.ContactData, len(cKeys))
	)

	if len(cKeys) == 0 {
		return contactList
	}

	_ = d.CachedConn.QueryRows(
		ctx,
		func(ctx context.Context, conn *sqlx.DB, keys ...string) (map[string]interface{}, error) {
			noCaches := make(map[string]interface{}, len(keys))
			for _, key := range keys {
				id0, id1 := parseContactCacheKey(key)
				contact, _ := d.UserContactsDAO.SelectContact(ctx, id0, id1)
				if contact == nil {
					// return sqlc.ErrNotFound
					continue
				}
				noCaches[key] = contact
				contactList = append(contactList, mtproto.MakeTLContactData(&mtproto.ContactData{
					UserId:        contact.OwnerUserId,
					ContactUserId: contact.ContactUserId,
					FirstName:     mtproto.MakeFlagsString(contact.ContactFirstName),
					LastName:      mtproto.MakeFlagsString(contact.ContactLastName),
					MutualContact: contact.Mutual,
					Phone:         mtproto.MakeFlagsString(contact.ContactPhone),
					CloseFriend:   contact.CloseFriend,
				}).To_ContactData())
			}
			return noCaches, nil
		},
		func(k, v string) (interface{}, error) {
			var (
				contact *dataobject.UserContactsDO
			)

			err := jsonx.UnmarshalFromString(v, &contact)
			if err != nil {
				return nil, err
			}

			contactList = append(contactList, mtproto.MakeTLContactData(&mtproto.ContactData{
				UserId:        contact.OwnerUserId,
				ContactUserId: contact.ContactUserId,
				FirstName:     mtproto.MakeFlagsString(contact.ContactFirstName),
				LastName:      mtproto.MakeFlagsString(contact.ContactLastName),
				MutualContact: contact.Mutual,
				Phone:         mtproto.MakeFlagsString(contact.ContactPhone),
				CloseFriend:   contact.CloseFriend,
			}).To_ContactData())

			return contact, nil
		},
		cKeys...)

	return removeAllNil(contactList)
}
