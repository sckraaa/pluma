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

package core

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/phonenumber"
)

// Limits of contacts.importContacts (task 011, ADR 0015). Telegram returns
// what it does not import now in retry_contacts; the official clients send
// those numbers again later: Android in 5 minutes (ContactsController), iOS
// a day later (ContactSyncManager).
const (
	// MaxImportContacts is the most contacts one request imports. Android
	// and iOS send a phone book 500 contacts at a time.
	MaxImportContacts = 500
	// ImportDailyLimit is the most new numbers a user imports in one
	// ImportWindow: numbers that are neither in the user's phone book on
	// the server nor among the user's contacts. Syncing the same phone book
	// again costs nothing; finding out which of a list of numbers use Pluma
	// is bounded by it.
	ImportDailyLimit = 2000
	// ImportWindow is the window of ImportDailyLimit: it starts with the
	// first new number after the previous window ended.
	ImportWindow = 24 * time.Hour
	// maxNameLength is the length of a contact's first and last name, as
	// Telegram limits them.
	maxNameLength = 64
)

// importEntry is one number of an import request, with every client_id the
// request gave it (a phone book may hold one number twice) and the name of
// its last occurrence.
type importEntry struct {
	phone     string
	clientIds []int64
	firstName string
	lastName  string
}

// parseImport normalizes the numbers of a request as the login does
// (phonenumber.NormalizeContact, national numbers in the importer's
// region). Numbers that do not parse and the importer's own number are
// skipped: they are not imported and not answered. Contacts after the
// first MaxImportContacts come back in retry. The entries keep the order
// of the request.
func parseImport(contacts []*mtproto.InputContact, region, selfPhone string) (entries []*importEntry, retry []int64) {
	byPhone := make(map[string]*importEntry, len(contacts))
	for i, c := range contacts {
		if i >= MaxImportContacts {
			retry = append(retry, c.GetClientId())
			continue
		}
		phone, err := phonenumber.NormalizeContact(c.GetPhone(), region)
		if err != nil || phone == selfPhone {
			continue
		}
		e, ok := byPhone[phone]
		if !ok {
			e = &importEntry{phone: phone}
			byPhone[phone] = e
			entries = append(entries, e)
		}
		e.clientIds = append(e.clientIds, c.GetClientId())
		e.firstName = contactName(c.GetFirstName())
		e.lastName = contactName(c.GetLastName())
	}
	return entries, retry
}

// contactName is a name of a phone book as a contact keeps it: trimmed and
// at most maxNameLength characters.
func contactName(s string) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= maxNameLength {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:maxNameLength]))
}

// importTarget is what a number of an import is, as read before the
// transaction.
type importTarget struct {
	taken  bool  // the number belongs to an account
	userId int64 // the account the importer may add as a contact, or 0
}

// importPlan is what one import does.
type importPlan struct {
	contacts  []*importEntry // numbers that become (or stay) contacts
	phoneBook []*importEntry // numbers kept in the phone book
	retry     []int64        // client_ids of numbers over the limit
	newCount  int32          // numbers the limit counts
}

// planImport splits the entries of an import: known numbers (in the
// importer's phone book or contacts already) are always imported, new ones
// while quota lasts, the rest come back in retry, in the order of the
// request.
func planImport(entries []*importEntry, targets map[string]importTarget, known func(e *importEntry) bool, quota int32) importPlan {
	var p importPlan
	for _, e := range entries {
		if !known(e) {
			if p.newCount >= quota {
				p.retry = append(p.retry, e.clientIds...)
				continue
			}
			p.newCount++
		}
		if targets[e.phone].userId != 0 {
			p.contacts = append(p.contacts, e)
		} else {
			p.phoneBook = append(p.phoneBook, e)
		}
	}
	return p
}

// importQuota returns the start of the import window at now and how many
// new numbers are left in it, from the stored window.
func importQuota(windowStart int64, imported int32, now int64) (start int64, left int32) {
	if now-windowStart >= int64(ImportWindow/time.Second) {
		return now, ImportDailyLimit
	}
	return windowStart, max(0, ImportDailyLimit-imported)
}

// addedByPhoneAllows tells whether the rules of inputPrivacyKeyAddedByPhone
// of owner let userId find owner by the number: everybody, or owner's
// contacts, with the users of the exceptions.
func addedByPhoneAllows(owner int64, rules []*mtproto.PrivacyRule, userId int64, ownerHasUser bool) bool {
	if len(rules) == 0 {
		return true
	}
	return mtproto.CheckPrivacyIsAllow(owner, rules, userId,
		func(_, _ int64) bool { return ownerHasUser },
		// Exceptions for chat members are not offered for this key.
		func(_ int64, _ []int64) bool { return false })
}
