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
	"errors"
	"regexp"
	"strings"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/phonenumber"
)

const (
	// maxSearchLimit is the most peers contacts.search returns in each of
	// its lists; 0 asks for the most.
	maxSearchLimit = 50
	// minGlobalSearch is the shortest query searched among all users.
	minGlobalSearch = 3
)

// phoneQuery is a query that is a phone number: digits, with a '+' and
// formatting.
var phoneQuery = regexp.MustCompile(`^\+?[0-9][0-9 ()\-]{4,}$`)

// ContactsSearch
// contacts.search#5f58d0f flags:# broadcasts:flags.0?true bots:flags.1?true q:string limit:int = contacts.Found;
//
// my_results are the user's contacts whose name (as the user saved it, or
// their own) or username starts a word with q; results the other users
// found by the public username, as Telegram finds people outside the
// contacts — a user without a username is not found by name — and the user
// whose phone number q is, if that number is visible to the searcher
// (phone number privacy). "@name" searches usernames only.
//
// Public channels are found by their username as users are (task 055):
// one the searcher is in goes to my_results, another to results, and its
// chat to chats; a deleted one is left out. broadcasts keeps only
// broadcast channels, bots only bot users
// (core.telegram.org/method/contacts.search).
func (c *ContactsCore) ContactsSearch(in *mtproto.TLContactsSearch) (*mtproto.Contacts_Found, error) {
	q := strings.TrimSpace(in.GetQ())
	byUsername := strings.HasPrefix(q, "@")
	if byUsername {
		q = strings.TrimSpace(q[1:])
	}
	if q == "" {
		err := mtproto.ErrSearchQueryEmpty
		c.Logger.Errorf("contacts.search - error: %v", err)
		return nil, err
	}
	limit := int(in.GetLimit())
	if limit <= 0 || limit > maxSearchLimit {
		limit = maxSearchLimit
	}

	found := mtproto.MakeTLContactsFound(&mtproto.Contacts_Found{
		MyResults: []*mtproto.Peer{},
		Results:   []*mtproto.Peer{},
		Users:     []*mtproto.User{},
		Chats:     []*mtproto.Chat{},
	}).To_Contacts_Found()

	contactList, err := c.svcCtx.Dao.UserClient.UserGetContactIdList(c.ctx, &userpb.TLUserGetContactIdList{
		UserId: c.MD.UserId,
	})
	if err != nil {
		c.Logger.Errorf("contacts.search - error: %v", err)
		return nil, err
	}
	contactIds := contactList.GetDatas()
	isContact := make(map[int64]bool, len(contactIds))
	for _, id := range contactIds {
		isContact[id] = true
	}

	// Candidates: the contacts, the users and channels of the username, the
	// user of the phone number.
	var others, channelIds []int64
	if len(q) >= minGlobalSearch {
		names, err := c.svcCtx.Dao.UserClient.UserSearchUsername(c.ctx, &userpb.TLUserSearchUsername{
			Q:                q,
			ExcludedContacts: append([]int64{c.MD.UserId}, contactIds...),
			Limit:            int32(limit),
		})
		if err != nil {
			c.Logger.Errorf("contacts.search - error: %v", err)
			return nil, err
		}
		for _, n := range names.GetDatas() {
			switch n.GetPeer().GetPredicateName() {
			case mtproto.Predicate_peerUser:
				others = append(others, n.GetPeer().GetUserId())
			case mtproto.Predicate_peerChannel:
				channelIds = append(channelIds, n.GetPeer().GetChannelId())
			}
		}
	}
	var byPhone int64
	if !byUsername && phoneQuery.MatchString(q) {
		if byPhone, err = c.userByPhone(q); err != nil {
			c.Logger.Errorf("contacts.search - error: %v", err)
			return nil, err
		}
	}

	ids := append(append([]int64{c.MD.UserId}, contactIds...), others...)
	if byPhone != 0 {
		ids = append(ids, byPhone)
	}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{
		Id: ids,
		To: []int64{c.MD.UserId},
	})
	if err != nil {
		c.Logger.Errorf("contacts.search - error: %v", err)
		return nil, err
	}

	picked := make(map[int64]bool)
	pick := func(id int64, mine bool) bool {
		if picked[id] || id == c.MD.UserId {
			return false
		}
		it, ok := users.GetImmutableUser(id)
		if !ok || it.Deleted() || in.GetBots() && !it.IsBot() {
			return false
		}
		u, err := users.GetUnsafeUser(c.MD.UserId, id)
		if err != nil {
			return false
		}
		picked[id] = true
		found.Users = append(found.Users, u)
		if mine {
			found.MyResults = append(found.MyResults, mtproto.MakePeerUser(id))
		} else {
			found.Results = append(found.Results, mtproto.MakePeerUser(id))
		}
		return true
	}

	for _, id := range contactIds {
		if len(found.MyResults) >= limit {
			break
		}
		it, ok := users.GetImmutableUser(id)
		if !ok {
			continue
		}
		u, err := users.GetUnsafeUser(c.MD.UserId, id)
		if err != nil {
			continue
		}
		var matched bool
		if byUsername {
			matched = usernameMatches(q, it)
		} else {
			matched = usernameMatches(q, it) ||
				nameMatches(q, u.GetFirstName().GetValue(), u.GetLastName().GetValue()) ||
				nameMatches(q, it.FirstName(), it.LastName())
		}
		if matched {
			pick(id, true)
		}
	}
	for _, id := range others {
		if len(found.Results) >= limit {
			break
		}
		pick(id, false)
	}
	if len(channelIds) > 0 && !in.GetBots() {
		if err = c.searchChannels(found, channelIds, in.GetBroadcasts(), limit); err != nil {
			c.Logger.Errorf("contacts.search - error: %v", err)
			return nil, err
		}
	}
	if byPhone != 0 && byPhone != c.MD.UserId && !picked[byPhone] {
		// A number finds its user only when "who can find me by my number"
		// lets the searcher (task 034), as contacts.resolvePhone.
		me, ok := users.GetImmutableUser(c.MD.UserId)
		owner, ok2 := users.GetImmutableUser(byPhone)
		if ok && ok2 && owner.CheckPrivacyFor(mtproto.ADDED_BY_PHONE, me) {
			pick(byPhone, isContact[byPhone])
		}
	}

	return found, nil
}

// searchChannels adds the public channels of channelIds to found: those
// the searcher is in to my_results, the others to results, each list up to
// limit, with their chats.
func (c *ContactsCore) searchChannels(found *mtproto.Contacts_Found, channelIds []int64, broadcastsOnly bool, limit int) error {
	list, err := c.svcCtx.Dao.Channel.ChannelGetChannelListByIdList(c.ctx, &channelpb.TLChannelGetChannelListByIdList{
		SelfId: c.MD.UserId,
		IdList: channelIds,
	})
	if err != nil {
		return err
	}
	kept := &channelpb.Vector_MutableChannel{}
	for _, mc := range list.GetDatas() {
		ch := mc.GetChannel()
		// The username row may outlive the channel's link (deleted, made
		// private since): only a channel public now is found.
		if ch == nil || ch.GetDeleted() || !ch.Public() || broadcastsOnly && !ch.GetBroadcast() {
			continue
		}
		peer := mtproto.MakePeerChannel(ch.GetId())
		if p, ok := mc.GetParticipant(c.MD.UserId); ok && p.Member() {
			if len(found.MyResults) >= limit {
				continue
			}
			found.MyResults = append(found.MyResults, peer)
		} else {
			if len(found.Results) >= limit {
				continue
			}
			found.Results = append(found.Results, peer)
		}
		kept.Datas = append(kept.Datas, mc)
	}
	found.Chats = append(found.Chats, kept.GetChannelListByIdList(c.MD.UserId)...)
	return nil
}

// userByPhone returns the user of the phone number q, read as the searcher
// would dial it (the national numbers of the searcher's country), or 0.
func (c *ContactsCore) userByPhone(q string) (int64, error) {
	me, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{
		Id: c.MD.UserId,
	})
	if err != nil {
		return 0, err
	}
	phone, err := phonenumber.NormalizeContact(q, me.GetUser().GetCountryCode())
	if err != nil {
		// phoneQuery checks only the shape: q is not a number after all.
		return 0, nil //nolint:nilerr // an invalid number finds nobody, the search goes on
	}
	id, err := c.svcCtx.Dao.UserClient.UserGetUserIdByPhone(c.ctx, &userpb.TLUserGetUserIdByPhone{
		Phone: phone,
	})
	switch {
	case errors.Is(err, mtproto.ErrPhoneNotOccupied):
		return 0, nil
	case err != nil:
		return 0, err
	}
	return id.GetV(), nil
}

// nameMatches tells whether a word of the name, or the whole name, starts
// with q, ignoring case.
func nameMatches(q string, firstName, lastName string) bool {
	q = strings.ToLower(q)
	full := strings.ToLower(strings.TrimSpace(firstName + " " + lastName))
	if full == "" {
		return false
	}
	if strings.HasPrefix(full, q) {
		return true
	}
	for _, w := range strings.Fields(full) {
		if strings.HasPrefix(w, q) {
			return true
		}
	}
	return false
}

// usernameMatches tells whether a username of the user starts with q,
// ignoring case.
func usernameMatches(q string, u *mtproto.ImmutableUser) bool {
	q = strings.ToLower(q)
	if u.Username() != "" && strings.HasPrefix(strings.ToLower(u.Username()), q) {
		return true
	}
	for _, n := range u.Usernames() {
		if n.GetActive() && strings.HasPrefix(strings.ToLower(n.GetUsername()), q) {
			return true
		}
	}
	return false
}
