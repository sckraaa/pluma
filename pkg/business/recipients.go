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

package business

import (
	"encoding/json"
	"slices"

	"github.com/teamgram/proto/mtproto"
)

// Recipients are the private chats an automatic reply or the connected bot
// serves (inputBusinessRecipients, inputBusinessBotRecipients), with the
// users resolved to ids. Without ExcludeSelected the categories and Users
// are the chats served; with it they are the chats left out ("inverts the
// selection", core.telegram.org/constructor/inputBusinessRecipients), as
// tdesktop builds them (data/business/data_business_common.cpp:55-112).
// ExcludeUsers is the bot's alone: the users left out of the chats chosen
// by category, set only without ExcludeSelected (ForBotsToMTP there).
type Recipients struct {
	ExistingChats   bool    `json:"existing_chats,omitempty"`
	NewChats        bool    `json:"new_chats,omitempty"`
	Contacts        bool    `json:"contacts,omitempty"`
	NonContacts     bool    `json:"non_contacts,omitempty"`
	ExcludeSelected bool    `json:"exclude_selected,omitempty"`
	Users           []int64 `json:"users,omitempty"`
	ExcludeUsers    []int64 `json:"exclude_users,omitempty"`
}

// Peer is what Match knows of the other side of a private chat: whether
// the chat is new (it began after the setting was first turned on) and
// whether the user is a contact of the account.
type Peer struct {
	UserID  int64
	New     bool
	Contact bool
}

// Empty tells whether r chooses nothing: no category and no user while
// choosing only the selected chats, BUSINESS_RECIPIENTS_EMPTY
// (core.telegram.org/method/account.updateConnectedBot). Every client turns
// an empty choice into "all but" before sending it (tdesktop
// BusinessRecipients::MakeValid, data_business_common.cpp:72-77).
func (r Recipients) Empty() bool {
	return !r.ExcludeSelected && !r.ExistingChats && !r.NewChats && !r.Contacts && !r.NonContacts && len(r.Users) == 0
}

// Match tells whether the chat with p is among r. Without ExcludeSelected
// it is when p is one of Users or one of its categories is chosen, and p
// is not one of ExcludeUsers; with it, when p is not one of Users and none
// of its categories is chosen.
func (r Recipients) Match(p Peer) bool {
	if slices.Contains(r.ExcludeUsers, p.UserID) {
		return false
	}
	category := (p.New && r.NewChats) || (!p.New && r.ExistingChats) ||
		(p.Contact && r.Contacts) || (!p.Contact && r.NonContacts)
	listed := slices.Contains(r.Users, p.UserID)
	if r.ExcludeSelected {
		return !listed && !category
	}
	return listed || category
}

// Normalize returns r with its lists sorted and without repeats, as it is
// kept.
func (r Recipients) Normalize() Recipients {
	r.Users = sortedIDs(r.Users)
	r.ExcludeUsers = sortedIDs(r.ExcludeUsers)
	return r
}

func sortedIDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	out := slices.Clone(ids)
	slices.Sort(out)
	return slices.Compact(out)
}

// MarshalRecipients is r as it is kept, in JSON.
func MarshalRecipients(r Recipients) (string, error) {
	data, err := json.Marshal(r.Normalize())
	return string(data), err
}

// UnmarshalRecipients reads recipients kept by MarshalRecipients.
func UnmarshalRecipients(data string) (Recipients, error) {
	var r Recipients
	err := json.Unmarshal([]byte(data), &r)
	return r, err
}

// RecipientsFromInput is in with users, the ids of in.users, resolved by
// the caller.
func RecipientsFromInput(in *mtproto.InputBusinessRecipients, users []int64) Recipients {
	return Recipients{
		ExistingChats:   in.GetExistingChats(),
		NewChats:        in.GetNewChats(),
		Contacts:        in.GetContacts(),
		NonContacts:     in.GetNonContacts(),
		ExcludeSelected: in.GetExcludeSelected(),
		Users:           users,
	}.Normalize()
}

// RecipientsFromBotInput is in with users and excluded, the ids of
// in.users and in.exclude_users, resolved by the caller.
func RecipientsFromBotInput(in *mtproto.InputBusinessBotRecipients, users, excluded []int64) Recipients {
	return Recipients{
		ExistingChats:   in.GetExistingChats(),
		NewChats:        in.GetNewChats(),
		Contacts:        in.GetContacts(),
		NonContacts:     in.GetNonContacts(),
		ExcludeSelected: in.GetExcludeSelected(),
		Users:           users,
		ExcludeUsers:    excluded,
	}.Normalize()
}

// ToTL is r as businessRecipients, of an automatic reply.
func (r Recipients) ToTL() *mtproto.BusinessRecipients {
	return mtproto.MakeTLBusinessRecipients(&mtproto.BusinessRecipients{
		ExistingChats:   r.ExistingChats,
		NewChats:        r.NewChats,
		Contacts:        r.Contacts,
		NonContacts:     r.NonContacts,
		ExcludeSelected: r.ExcludeSelected,
		Users:           r.Users,
	}).To_BusinessRecipients()
}

// ToBotTL is r as businessBotRecipients, of the connected bot.
func (r Recipients) ToBotTL() *mtproto.BusinessBotRecipients {
	return mtproto.MakeTLBusinessBotRecipients(&mtproto.BusinessBotRecipients{
		ExistingChats:   r.ExistingChats,
		NewChats:        r.NewChats,
		Contacts:        r.Contacts,
		NonContacts:     r.NonContacts,
		ExcludeSelected: r.ExcludeSelected,
		Users:           r.Users,
		ExcludeUsers:    r.ExcludeUsers,
	}).To_BusinessBotRecipients()
}

// UserIDs are all the users r names, for the users of an answer.
func (r Recipients) UserIDs() []int64 {
	return sortedIDs(append(slices.Clone(r.Users), r.ExcludeUsers...))
}
