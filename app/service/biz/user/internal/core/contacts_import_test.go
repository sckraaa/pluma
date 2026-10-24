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
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
)

func inputContact(clientId int64, phone, first, last string) *mtproto.InputContact {
	return mtproto.MakeTLInputPhoneContact(&mtproto.InputContact{
		ClientId:  clientId,
		Phone:     phone,
		FirstName: first,
		LastName:  last,
	}).To_InputContact()
}

func TestParseImport(t *testing.T) {
	long := strings.Repeat("я", maxNameLength+10)
	entries, retry := parseImport([]*mtproto.InputContact{
		inputContact(1, "+7 (999) 000-00-01", "Ann", ""),
		inputContact(2, "not a number", "Bob", ""),
		inputContact(3, "89990000001", "Anna", "B"),  // the same number, national
		inputContact(4, "+1 201 555 0100", "Me", ""), // the importer
		inputContact(5, "+1 201 555-0101", " Carl ", long),
	}, "RU", "12015550100")
	if len(retry) != 0 {
		t.Errorf("retry %v", retry)
	}
	if len(entries) != 2 {
		t.Fatalf("entries %+v", entries)
	}
	ann := entries[0]
	if ann.phone != "79990000001" || !reflect.DeepEqual(ann.clientIds, []int64{1, 3}) || ann.firstName != "Anna" || ann.lastName != "B" {
		t.Errorf("one number twice: %+v (the last name of the phone book wins)", ann)
	}
	carl := entries[1]
	if carl.phone != "12015550101" || !reflect.DeepEqual(carl.clientIds, []int64{5}) {
		t.Errorf("carl %+v", carl)
	}
	if carl.firstName != "Carl" || utf8.RuneCountInString(carl.lastName) != maxNameLength {
		t.Errorf("names are trimmed and cut to %d characters: %q, %d", maxNameLength, carl.firstName, utf8.RuneCountInString(carl.lastName))
	}
}

func TestParseImportRetriesOverTheRequestLimit(t *testing.T) {
	contacts := make([]*mtproto.InputContact, 0, MaxImportContacts+3)
	for i := range MaxImportContacts + 3 {
		contacts = append(contacts, inputContact(int64(i), fmt.Sprintf("+1201555%04d", 100+i%100), "N", ""))
	}
	entries, retry := parseImport(contacts, "US", "")
	if len(retry) != 3 || retry[0] != MaxImportContacts {
		t.Errorf("retry %v, want the 3 contacts after %d", retry, MaxImportContacts)
	}
	// 100 distinct numbers, each with 5 client_ids.
	if len(entries) != 100 || len(entries[0].clientIds) != MaxImportContacts/100 {
		t.Errorf("%d entries, %d client ids of the first", len(entries), len(entries[0].clientIds))
	}
}

func TestPlanImport(t *testing.T) {
	e := func(phone string, ids ...int64) *importEntry { return &importEntry{phone: phone, clientIds: ids} }
	entries := []*importEntry{e("1", 11), e("2", 21, 22), e("3", 31), e("4", 41), e("5", 51)}
	targets := map[string]importTarget{
		"1": {taken: true, userId: 100}, // a contact to be
		"2": {taken: true},              // taken, hidden by privacy
		"4": {taken: true, userId: 400},
	}
	known := map[string]bool{"3": true, "4": true}
	p := planImport(entries, targets, func(e *importEntry) bool { return known[e.phone] }, 2)

	phones := func(list []*importEntry) []string {
		var out []string
		for _, e := range list {
			out = append(out, e.phone)
		}
		return out
	}
	// New numbers in request order while the quota lasts (1 and 2), known
	// ones always (3 and 4), the rest retried (5).
	if got := phones(p.contacts); !reflect.DeepEqual(got, []string{"1", "4"}) {
		t.Errorf("contacts %v", got)
	}
	if got := phones(p.phoneBook); !reflect.DeepEqual(got, []string{"2", "3"}) {
		t.Errorf("phone book %v (a hidden account is kept as a number)", got)
	}
	if !reflect.DeepEqual(p.retry, []int64{51}) || p.newCount != 2 {
		t.Errorf("retry %v, new %d", p.retry, p.newCount)
	}

	p = planImport(entries, targets, func(*importEntry) bool { return false }, 0)
	if len(p.contacts)+len(p.phoneBook) != 0 || !reflect.DeepEqual(p.retry, []int64{11, 21, 22, 31, 41, 51}) {
		t.Errorf("without quota: %+v", p)
	}
}

func TestImportQuota(t *testing.T) {
	const now = int64(1_000_000)
	day := int64(ImportWindow.Seconds())
	if start, left := importQuota(0, 0, now); start != now || left != ImportDailyLimit {
		t.Errorf("first import: %d, %d", start, left)
	}
	if start, left := importQuota(now-10, 500, now); start != now-10 || left != ImportDailyLimit-500 {
		t.Errorf("in the window: %d, %d", start, left)
	}
	if _, left := importQuota(now-10, ImportDailyLimit+5, now); left != 0 {
		t.Errorf("over the limit: %d left", left)
	}
	if start, left := importQuota(now-day, ImportDailyLimit, now); start != now || left != ImportDailyLimit {
		t.Errorf("a day later: %d, %d", start, left)
	}
}

func TestAddedByPhoneAllows(t *testing.T) {
	const owner, importer = 1, 2
	all := []*mtproto.PrivacyRule{mtproto.MakeTLPrivacyValueAllowAll(nil).To_PrivacyRule()}
	contacts := []*mtproto.PrivacyRule{mtproto.MakeTLPrivacyValueAllowContacts(nil).To_PrivacyRule()}
	for _, c := range []struct {
		name         string
		rules        []*mtproto.PrivacyRule
		ownerHasUser bool
		want         bool
	}{
		{"no rules: everybody, the default", nil, false, true},
		{"everybody", all, false, true},
		{"everybody but the importer", append(all, mtproto.MakeTLPrivacyValueDisallowUsers(&mtproto.PrivacyRule{Users: []int64{importer}}).To_PrivacyRule()), false, false},
		{"my contacts, not one", contacts, false, false},
		{"my contacts, one", contacts, true, true},
		{"my contacts and the importer", append(contacts, mtproto.MakeTLPrivacyValueAllowUsers(&mtproto.PrivacyRule{Users: []int64{importer}}).To_PrivacyRule()), false, true},
	} {
		if got := addedByPhoneAllows(owner, c.rules, importer, c.ownerHasUser); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
