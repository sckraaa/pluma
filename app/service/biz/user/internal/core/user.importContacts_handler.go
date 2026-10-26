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
	"errors"
	"fmt"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// errImportStale is returned by the transaction of an import when a number
// it keeps in the phone book got an account after the numbers were looked
// up: the import is planned again.
var errImportStale = errors.New("user: an imported number was registered meanwhile")

// importAttempts bounds the plans of one import: a number registered
// during each of them is not realistic.
const importAttempts = 3

// UserImportContacts
// user.importContacts user_id:long contacts:Vector<InputContact> = UserImportedContacts;
//
// The phone book import of contacts.importContacts (task 011, ADR 0015).
// A number of an account the importer may find (its addedByPhone privacy)
// becomes a contact with the name of the phone book; any other number is
// kept in the importer's phone book on the server, and counts as an invite:
// Telegram does not tell whether a number it does not let you find is
// taken. A repeated import renames and adds nothing twice. update_id_list
// holds the contacts the import added or changed; the transaction of the
// import pushes updatePeerSettings with seq of each of them, with the
// contact, to every device of the importer (ADR 0015, ADR 0020).
func (c *UserCore) UserImportContacts(in *user.TLUserImportContacts) (*user.UserImportedContacts, error) {
	me := c.svcCtx.Dao.GetCacheUserData(c.ctx, in.UserId)
	if me.GetUserData() == nil {
		err := mtproto.ErrUserIdInvalid
		c.Logger.Errorf("user.importContacts - error: %v", err)
		return nil, err
	}
	entries, retry := parseImport(in.Contacts, me.GetUserData().GetCountryCode(), me.GetUserData().GetPhone())

	var (
		plan    importPlan
		changes []dao.ContactChange
		err     error
	)
	for attempt := 1; len(entries) > 0; attempt++ {
		var targets map[string]importTarget
		if targets, err = c.importTargets(in.UserId, entries); err != nil {
			c.Logger.Errorf("user.importContacts - error: %v", err)
			return nil, err
		}
		plan, changes, err = c.runImport(in.UserId, entries, targets)
		if !errors.Is(err, errImportStale) || attempt == importAttempts {
			break
		}
	}
	if err != nil {
		c.Logger.Errorf("user.importContacts - user %d: %v", in.UserId, err)
		return nil, err
	}

	result := user.MakeTLUserImportedContacts(&user.UserImportedContacts{
		Imported:       []*mtproto.ImportedContact{},
		PopularInvites: []*mtproto.PopularContact{},
		RetryContacts:  append(retry, plan.retry...),
		Users:          []*mtproto.User{},
		UpdateIdList:   []int64{},
	}).To_UserImportedContacts()

	if len(plan.phoneBook) > 0 {
		phones := make([]string, 0, len(plan.phoneBook))
		for _, e := range plan.phoneBook {
			phones = append(phones, e.phone)
		}
		importers, err := c.svcCtx.Dao.ImportersCount(c.ctx, phones)
		if err != nil {
			c.Logger.Errorf("user.importContacts - user %d: %v", in.UserId, err)
			return nil, err
		}
		for _, e := range plan.phoneBook {
			for _, id := range e.clientIds {
				result.PopularInvites = append(result.PopularInvites, mtproto.MakeTLPopularContact(&mtproto.PopularContact{
					ClientId:  id,
					Importers: importers[e.phone],
				}).To_PopularContact())
			}
		}
	}

	if len(changes) > 0 {
		ids := make([]int64, 0, len(changes))
		for i, ch := range changes {
			ids = append(ids, ch.UserId)
			for _, id := range plan.contacts[i].clientIds {
				result.Imported = append(result.Imported, mtproto.MakeTLImportedContact(&mtproto.ImportedContact{
					UserId:   ch.UserId,
					ClientId: id,
				}).To_ImportedContact())
			}
			if ch.Changed {
				result.UpdateIdList = append(result.UpdateIdList, ch.UserId)
			}
		}
		users, err := c.UserGetMutableUsers(&user.TLUserGetMutableUsers{
			Id: append([]int64{in.UserId}, ids...),
			To: []int64{in.UserId},
		})
		if err != nil {
			c.Logger.Errorf("user.importContacts - user %d: %v", in.UserId, err)
			return nil, err
		}
		// Only the imported users: a client takes the first user of the
		// answer for the contact it imported (iOS ImportContact).
		result.Users = users.GetUserListByIdList(in.UserId, ids...)
	}
	return result, nil
}

// importTargets looks up the accounts of the numbers of an import and
// whether the importer may find each by its number.
func (c *UserCore) importTargets(userId int64, entries []*importEntry) (map[string]importTarget, error) {
	phones := make([]string, 0, len(entries))
	for _, e := range entries {
		phones = append(phones, e.phone)
	}
	rows, err := c.svcCtx.Dao.UsersDAO.SelectUsersByPhoneList(c.ctx, phones)
	if err != nil {
		return nil, err
	}
	targets := make(map[string]importTarget, len(entries))
	var accounts []int64
	byId := make(map[int64]string)
	for _, r := range rows {
		targets[r.Phone] = importTarget{taken: true}
		if r.Id != userId && !r.Deleted && r.UserType == user.UserTypeRegular {
			accounts = append(accounts, r.Id)
			byId[r.Id] = r.Phone
		}
	}
	if len(accounts) == 0 {
		return targets, nil
	}
	datas := c.svcCtx.Dao.GetCacheUserDataListByIdList(c.ctx, accounts)
	if len(datas) != len(accounts) {
		// An account the cache could not load: answering without it would
		// show a registered number as an invite.
		return nil, fmt.Errorf("user: %d of %d accounts of imported numbers not loaded", len(accounts)-len(datas), len(accounts))
	}
	for _, data := range datas {
		owner := data.GetUserData().GetId()
		phone, ok := byId[owner]
		if !ok {
			continue
		}
		rules, err := c.svcCtx.Dao.GetUserPrivacyRules(c.ctx, owner, mtproto.ADDED_BY_PHONE)
		if err != nil {
			return nil, err
		}
		ownerHasUser := false
		for _, id := range data.GetContactIdList() {
			if id == userId {
				ownerHasUser = true
				break
			}
		}
		if addedByPhoneAllows(owner, rules.GetRules(), userId, ownerHasUser) {
			targets[phone] = importTarget{taken: true, userId: owner}
		}
	}
	return targets, nil
}

// importPeers returns the accounts an import may make contacts of, as the
// importer sees them before the import, by id: the users of the pushes.
func (c *UserCore) importPeers(userId int64, targets map[string]importTarget) (map[int64]*mtproto.User, error) {
	var ids []int64
	for _, t := range targets {
		if t.userId != 0 {
			ids = append(ids, t.userId)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	users, err := c.UserGetMutableUsers(&user.TLUserGetMutableUsers{
		Id: append([]int64{userId}, ids...),
		To: []int64{userId},
	})
	if err != nil {
		return nil, err
	}
	peers := make(map[int64]*mtproto.User, len(ids))
	for _, u := range users.GetUserListByIdList(userId, ids...) {
		peers[u.GetId()] = u
	}
	return peers, nil
}

// runImport writes an import in the importer's box transaction: the
// window of the limit, the contacts and the phone book, and
// updatePeerSettings with seq of each contact added or changed, with the
// user as the importer's contact. It returns the plan and, in the order of
// plan.contacts, what happened to each contact.
func (c *UserCore) runImport(userId int64, entries []*importEntry, targets map[string]importTarget) (importPlan, []dao.ContactChange, error) {
	var (
		plan    importPlan
		changes []dao.ContactChange
	)
	peers, err := c.importPeers(userId, targets)
	if err != nil {
		return plan, nil, err
	}
	contactIds := make([]int64, 0, len(entries))
	phones := make([]string, 0, len(entries))
	for _, e := range entries {
		phones = append(phones, e.phone)
		if id := targets[e.phone].userId; id != 0 {
			contactIds = append(contactIds, id)
		}
	}

	_, err = c.svcCtx.Dao.RunBox(c.ctx, userId, []string{dao.GenCacheUserDataCacheKey(userId)}, func(b *userbox.Box) error {
		// A retried attempt starts over.
		plan, changes = importPlan{}, nil

		windowStart, imported, err := dao.ImportQuota(b)
		if err != nil {
			return err
		}
		start, left := importQuota(windowStart, imported, b.Date())
		saved, err := dao.SavedPhones(b, phones)
		if err != nil {
			return err
		}
		rows, err := dao.ContactRows(b, contactIds)
		if err != nil {
			return err
		}
		plan = planImport(entries, targets, func(e *importEntry) bool {
			if saved[e.phone] {
				return true
			}
			r, ok := rows[targets[e.phone].userId]
			return ok && !r.IsDeleted
		}, left)
		if len(plan.contacts) == 0 && len(plan.phoneBook) == 0 {
			return nil
		}

		contacts := make([]dao.ImportedContact, 0, len(plan.contacts))
		contactPhones := make([]string, 0, len(plan.contacts))
		for _, e := range plan.contacts {
			contacts = append(contacts, dao.ImportedContact{
				UserId:    targets[e.phone].userId,
				Phone:     e.phone,
				FirstName: e.firstName,
				LastName:  e.lastName,
			})
			contactPhones = append(contactPhones, e.phone)
		}
		if changes, err = dao.PutContacts(b, contacts); err != nil {
			return err
		}
		if err = dao.ForgetPhones(b, contactPhones); err != nil {
			return err
		}

		book := make([]dao.ImportedPhone, 0, len(plan.phoneBook))
		bookPhones := make([]string, 0, len(plan.phoneBook))
		for _, e := range plan.phoneBook {
			book = append(book, dao.ImportedPhone{Phone: e.phone, FirstName: e.firstName, LastName: e.lastName})
			bookPhones = append(bookPhones, e.phone)
		}
		if err = dao.SavePhones(b, book); err != nil {
			return err
		}
		// A number that got an account after the look-up: with its row
		// written first, the sign-up either waits for this transaction and
		// sees the row (dao.LockImporters) or committed before this read.
		registered, err := dao.RegisteredPhones(b, bookPhones)
		if err != nil {
			return err
		}
		for p := range registered {
			if !targets[p].taken {
				return errImportStale
			}
		}

		if plan.newCount > 0 {
			if err = dao.SetImportQuota(b, start, ImportDailyLimit-left+plan.newCount); err != nil {
				return err
			}
		}
		var (
			changed []int64
			users   = make(map[int64][]*mtproto.User)
		)
		for i, ch := range changes {
			b.InvalidateCache(dao.ContactCacheKeys(userId, ch.UserId)...)
			if !ch.Changed {
				continue
			}
			changed = append(changed, ch.UserId)
			if u, ok := peers[ch.UserId]; ok {
				users[ch.UserId] = dao.AsContact([]*mtproto.User{u}, ch.UserId, &contacts[i], ch.Mutual)
			}
		}
		return c.svcCtx.Dao.EmitContactSettings(b, changed, func(id int64) []*mtproto.User { return users[id] })
	})
	return plan, changes, err
}
