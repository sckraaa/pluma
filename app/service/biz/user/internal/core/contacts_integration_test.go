//go:build integration

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
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	inboxpb "github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	syncpb "github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/config"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/svc"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/testinfra"

	"github.com/zeromicro/go-zero/core/jsonx"
	"github.com/zeromicro/go-zero/zrpc"
)

// The phone book import, the delivery of sign-ups and top peers on MySQL
// and Redis (task 011).

var (
	itDB    *testinfra.MySQL
	itRedis *testinfra.Redis
	itSvc   *svc.ServiceContext
	itRaw   *sql.DB
	itSeq   atomic.Int64
)

// fakeIds stands for idgen.
type fakeIds struct{ n atomic.Int64 }

func (f *fakeIds) NextIds(_ context.Context, num int) ([]int64, error) {
	ids := make([]int64, num)
	for i := range ids {
		ids[i] = 1<<40 + f.n.Add(1)
	}
	return ids, nil
}

func TestMain(m *testing.M) {
	ctx := context.Background()
	var (
		wg   sync.WaitGroup
		errs = make([]error, 2)
	)
	wg.Add(2)
	go func() { defer wg.Done(); itDB, errs[0] = testinfra.StartMySQL(ctx) }()
	go func() { defer wg.Done(); itRedis, errs[1] = testinfra.StartRedis(ctx) }()
	wg.Wait()

	code := 1
	if err := errors.Join(errs...); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else if err = setUp(); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else {
		code = m.Run()
		_ = itRaw.Close()
	}
	if itDB != nil {
		_ = itDB.Terminate(ctx)
	}
	if itRedis != nil {
		_ = itRedis.Terminate(ctx)
	}
	os.Exit(code)
}

func setUp() error {
	mysql := itDB.Config()
	// The session lock wait of the services' box transactions.
	mysql.DSN += "&innodb_lock_wait_timeout=2"
	// No media or idgen server: the users have no photos, idgen is faked.
	unused := zrpc.RpcClientConf{Endpoints: []string{"127.0.0.1:1"}, NonBlock: true}
	itSvc = svc.NewServiceContext(config.Config{
		Mysql:       mysql,
		Cache:       itRedis.CacheConf(),
		MediaClient: unused,
		IdgenClient: unused,
		Topics:      config.Topics{Inbox: "Inbox-T", Sync: "Sync-T"},
	})
	itSvc.Dao.Ids = &fakeIds{}
	var err error
	itRaw, err = sql.Open("mysql", itDB.DSN())
	return err
}

// npas are area codes whose 555-01XX numbers are reserved for fiction:
// never someone's number.
var npas = []int{201, 202, 203, 205, 206, 207, 208, 209, 210, 212, 213, 214, 215, 216, 217, 218, 219, 224, 225, 228, 229, 231, 234, 239, 240, 248}

// nextPhone returns a number no test of this process used, as users.phone
// keeps it.
func nextPhone() string {
	n := int(itSeq.Add(1))
	return fmt.Sprintf("1%d555%04d", npas[(n/100)%len(npas)], 100+n%100)
}

// formatted writes a number the way a phone book does.
func formatted(phone string) string {
	return fmt.Sprintf("+%s (%s) %s-%s", phone[:1], phone[1:4], phone[4:7], phone[7:])
}

// newUser creates an account with a number, as auth.signUp would but
// without its sign-up: the user registered long ago.
func newUser(t *testing.T, first string) (int64, string) {
	t.Helper()
	phone := nextPhone()
	res, err := itRaw.Exec("INSERT INTO users (user_type, access_hash, phone, country_code, first_name) VALUES (?, ?, ?, 'US', ?)",
		user.UserTypeRegular, itSeq.Add(1), phone, first)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id, phone
}

func userCore(t *testing.T) *UserCore {
	t.Helper()
	return New(context.Background(), itSvc)
}

func phoneContact(clientId int64, phone, first, last string) *mtproto.InputContact {
	return mtproto.MakeTLInputPhoneContact(&mtproto.InputContact{
		ClientId:  clientId,
		Phone:     phone,
		FirstName: first,
		LastName:  last,
	}).To_InputContact()
}

func importContacts(t *testing.T, userId int64, contacts ...*mtproto.InputContact) *user.UserImportedContacts {
	t.Helper()
	r, err := userCore(t).UserImportContacts(&user.TLUserImportContacts{UserId: userId, Contacts: contacts})
	if err != nil {
		t.Fatalf("import of user %d: %v", userId, err)
	}
	return r
}

func count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := itRaw.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

type contactRow struct {
	first, last, phone string
	mutual, deleted    bool
}

func contactOf(t *testing.T, owner, contact int64) *contactRow {
	t.Helper()
	var r contactRow
	err := itRaw.QueryRow("SELECT contact_first_name, contact_last_name, contact_phone, mutual, is_deleted FROM user_contacts WHERE owner_user_id = ? AND contact_user_id = ?",
		owner, contact).Scan(&r.first, &r.last, &r.phone, &r.mutual, &r.deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return &r
}

func TestImportContacts(t *testing.T) {
	ann, _ := newUser(t, "Ann")
	bob, bobPhone := newUser(t, "Bob")
	stranger := nextPhone() // no account

	r := importContacts(t, ann,
		phoneContact(1, formatted(bobPhone), "Bobby", "B"),
		phoneContact(2, "+"+stranger, "Sam", ""),
		phoneContact(3, stranger, "Samuel", ""), // the same number again
		phoneContact(4, "garbage", "X", ""),
	)
	if len(r.Imported) != 1 || r.Imported[0].UserId != bob || r.Imported[0].ClientId != 1 {
		t.Errorf("imported %v", r.Imported)
	}
	if len(r.PopularInvites) != 2 || r.PopularInvites[0].Importers != 1 || len(r.RetryContacts) != 0 {
		t.Errorf("popular %v, retry %v", r.PopularInvites, r.RetryContacts)
	}
	if len(r.Users) != 1 || r.Users[0].GetId() != bob || !r.Users[0].GetContact() || r.Users[0].GetFirstName().GetValue() != "Bobby" {
		t.Errorf("users %v: only the contact, with the name of the phone book", r.Users)
	}
	if len(r.UpdateIdList) != 1 || r.UpdateIdList[0] != bob {
		t.Errorf("update_id_list %v", r.UpdateIdList)
	}
	if c := contactOf(t, ann, bob); c == nil || c.first != "Bobby" || c.last != "B" || c.phone != bobPhone || c.mutual {
		t.Errorf("contact row %+v", c)
	}
	if n := count(t, "SELECT COUNT(*) FROM unregistered_contacts WHERE importer_user_id = ?", ann); n != 1 {
		t.Errorf("%d phone book rows, want 1", n)
	}

	// Again, with Bob renamed: nothing new, the name changes.
	r = importContacts(t, ann, phoneContact(1, "+"+bobPhone, "Robert", "B"), phoneContact(2, "+"+stranger, "Sam", ""))
	if len(r.Imported) != 1 || len(r.UpdateIdList) != 1 {
		t.Errorf("renamed: imported %v, update_id_list %v", r.Imported, r.UpdateIdList)
	}
	if c := contactOf(t, ann, bob); c == nil || c.first != "Robert" {
		t.Errorf("renamed row %+v", c)
	}
	// And the same once more: nothing to tell the other devices.
	r = importContacts(t, ann, phoneContact(1, "+"+bobPhone, "Robert", "B"), phoneContact(2, "+"+stranger, "Sam", ""))
	if len(r.Imported) != 1 || len(r.UpdateIdList) != 0 {
		t.Errorf("repeated: imported %v, update_id_list %v", r.Imported, r.UpdateIdList)
	}
	if n := count(t, "SELECT COUNT(*) FROM user_contacts WHERE owner_user_id = ?", ann); n != 1 {
		t.Errorf("%d contact rows", n)
	}
	if n := count(t, "SELECT COUNT(*) FROM unregistered_contacts WHERE importer_user_id = ?", ann); n != 1 {
		t.Errorf("%d phone book rows", n)
	}
	// Two new numbers counted, once.
	if n := count(t, "SELECT imported FROM contact_import_quota WHERE user_id = ?", ann); n != 2 {
		t.Errorf("quota counted %d new numbers, want 2", n)
	}
}

func TestImportContactsMutual(t *testing.T) {
	ann, annPhone := newUser(t, "Ann")
	bob, bobPhone := newUser(t, "Bob")
	importContacts(t, bob, phoneContact(1, "+"+annPhone, "Ann", ""))
	r := importContacts(t, ann, phoneContact(1, "+"+bobPhone, "Bob", ""))
	if len(r.Users) != 1 || !r.Users[0].GetMutualContact() {
		t.Errorf("users %v: Bob has Ann, the contact is mutual", r.Users)
	}
	if a, b := contactOf(t, ann, bob), contactOf(t, bob, ann); a == nil || b == nil || !a.mutual || !b.mutual {
		t.Errorf("rows %+v %+v: both mutual", a, b)
	}
}

// Two users import each other at once: whichever commits second sees the
// other's row (the deadlock of the locked reads is repeated by Run).
func TestImportContactsEachOtherAtOnce(t *testing.T) {
	for i := range 10 {
		ann, annPhone := newUser(t, "Ann")
		bob, bobPhone := newUser(t, "Bob")
		var wg sync.WaitGroup
		errs := make([]error, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, errs[0] = userCore(t).UserImportContacts(&user.TLUserImportContacts{UserId: ann, Contacts: []*mtproto.InputContact{phoneContact(1, "+"+bobPhone, "Bob", "")}})
		}()
		go func() {
			defer wg.Done()
			_, errs[1] = userCore(t).UserImportContacts(&user.TLUserImportContacts{UserId: bob, Contacts: []*mtproto.InputContact{phoneContact(1, "+"+annPhone, "Ann", "")}})
		}()
		wg.Wait()
		if err := errors.Join(errs...); err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
		if a, b := contactOf(t, ann, bob), contactOf(t, bob, ann); a == nil || b == nil || !a.mutual || !b.mutual {
			t.Fatalf("round %d: rows %+v %+v, want both mutual", i, a, b)
		}
	}
}

func TestImportOneNumberByManyAtOnce(t *testing.T) {
	const importers = 8
	stranger := nextPhone()
	ids := make([]int64, importers)
	for i := range ids {
		ids[i], _ = newUser(t, "Importer")
	}
	var wg sync.WaitGroup
	results := make([]*user.UserImportedContacts, importers)
	errs := make([]error, importers)
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = userCore(t).UserImportContacts(&user.TLUserImportContacts{UserId: id, Contacts: []*mtproto.InputContact{phoneContact(5, formatted(stranger), "Sam", "")}})
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		t.Fatal(err)
	}
	for i, r := range results {
		if len(r.PopularInvites) != 1 || r.PopularInvites[0].Importers < 1 || r.PopularInvites[0].Importers > importers {
			t.Errorf("importer %d: %v", i, r.PopularInvites)
		}
	}
	if n := count(t, "SELECT COUNT(*) FROM unregistered_contacts WHERE phone = ?", stranger); n != importers {
		t.Errorf("%d rows of the number, want %d", n, importers)
	}
	r := importContacts(t, ids[0], phoneContact(5, "+"+stranger, "Sam", ""))
	if r.PopularInvites[0].Importers != importers {
		t.Errorf("importers %d, want %d", r.PopularInvites[0].Importers, importers)
	}
}

func TestImportContactsDailyLimit(t *testing.T) {
	ann, _ := newUser(t, "Ann")
	known := nextPhone()
	importContacts(t, ann, phoneContact(1, "+"+known, "Known", ""))

	// Two new numbers left in the window.
	if _, err := itRaw.Exec("UPDATE contact_import_quota SET imported = ? WHERE user_id = ?", ImportDailyLimit-2, ann); err != nil {
		t.Fatal(err)
	}
	a, b, c := nextPhone(), nextPhone(), nextPhone()
	r := importContacts(t, ann,
		phoneContact(10, "+"+a, "A", ""),
		phoneContact(11, "+"+known, "Known", ""),
		phoneContact(12, "+"+b, "B", ""),
		phoneContact(13, "+"+c, "C", ""))
	if len(r.RetryContacts) != 1 || r.RetryContacts[0] != 13 {
		t.Errorf("retry %v, want the third new number", r.RetryContacts)
	}
	if len(r.PopularInvites) != 3 {
		t.Errorf("popular %v: two new and the known number", r.PopularInvites)
	}
	if n := count(t, "SELECT COUNT(*) FROM unregistered_contacts WHERE phone = ?", c); n != 0 {
		t.Errorf("the retried number is kept")
	}
	// Known numbers cost nothing even at the limit.
	if r = importContacts(t, ann, phoneContact(11, "+"+known, "Known", "")); len(r.RetryContacts) != 0 {
		t.Errorf("a known number retried at the limit: %v", r.RetryContacts)
	}
	// A day later the window starts again.
	if _, err := itRaw.Exec("UPDATE contact_import_quota SET window_start = window_start - ? WHERE user_id = ?", int64(ImportWindow.Seconds()), ann); err != nil {
		t.Fatal(err)
	}
	if r = importContacts(t, ann, phoneContact(13, "+"+c, "C", "")); len(r.RetryContacts) != 0 || len(r.PopularInvites) != 1 {
		t.Errorf("a day later: retry %v, popular %v", r.RetryContacts, r.PopularInvites)
	}
	if n := count(t, "SELECT imported FROM contact_import_quota WHERE user_id = ?", ann); n != 1 {
		t.Errorf("new window counted %d", n)
	}
}

func setAddedByPhone(t *testing.T, userId int64, rule *mtproto.PrivacyRule) {
	t.Helper()
	if _, err := userCore(t).UserSetPrivacy(&user.TLUserSetPrivacy{UserId: userId, KeyType: mtproto.ADDED_BY_PHONE, Rules: []*mtproto.PrivacyRule{rule}}); err != nil {
		t.Fatal(err)
	}
}

// A user who lets only contacts find them by the number is, to anyone
// else, a number without an account (Telegram does not say it is taken).
func TestImportContactsAddedByPhonePrivacy(t *testing.T) {
	ann, _ := newUser(t, "Ann")
	bob, bobPhone := newUser(t, "Bob")
	setAddedByPhone(t, bob, mtproto.MakeTLPrivacyValueAllowContacts(nil).To_PrivacyRule())

	r := importContacts(t, ann, phoneContact(1, "+"+bobPhone, "Bob", ""))
	if len(r.Imported) != 0 || len(r.Users) != 0 || len(r.PopularInvites) != 1 || r.PopularInvites[0].Importers != 1 {
		t.Errorf("hidden: %v", r)
	}
	if contactOf(t, ann, bob) != nil {
		t.Errorf("Ann got Bob as a contact")
	}

	// Bob adds Ann: now she may find him.
	if _, err := userCore(t).UserAddContact(&user.TLUserAddContact{UserId: bob, Id: ann, FirstName: "Ann", AddPhonePrivacyException: mtproto.BoolFalse}); err != nil {
		t.Fatal(err)
	}
	r = importContacts(t, ann, phoneContact(1, "+"+bobPhone, "Bob", ""))
	if len(r.Imported) != 1 || r.Imported[0].UserId != bob || len(r.PopularInvites) != 0 {
		t.Errorf("Bob's contact: %v", r)
	}
	if c := contactOf(t, ann, bob); c == nil || !c.mutual {
		t.Errorf("row %+v", c)
	}
	if n := count(t, "SELECT COUNT(*) FROM unregistered_contacts WHERE importer_user_id = ?", ann); n != 0 {
		t.Errorf("the number stayed in the phone book")
	}
}

// add_phone_privacy_exception (contacts.addContact, contacts.acceptContact)
// shares the phone number with the contact, whatever the rules: exceptions
// added at once all stay, and the others still do not see the number.
func TestPhonePrivacyExceptions(t *testing.T) {
	ann, _ := newUser(t, "Ann")
	const peers = 6
	ids := make([]int64, peers)
	for i := range ids {
		ids[i], _ = newUser(t, "Peer")
	}
	var wg sync.WaitGroup
	errs := make([]error, peers)
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = userCore(t).UserAddContact(&user.TLUserAddContact{UserId: ann, Id: id, FirstName: "Peer", AddPhonePrivacyException: mtproto.BoolTrue})
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		t.Fatal(err)
	}

	stranger, _ := newUser(t, "Stranger")
	rules, err := itSvc.Dao.GetUserPrivacyRules(context.Background(), ann, mtproto.PHONE_NUMBER)
	if err != nil {
		t.Fatal(err)
	}
	sees := func(id int64) bool {
		return mtproto.CheckPrivacyIsAllow(ann, rules.GetRules(), id,
			func(owner, id int64) bool { return contactOf(t, owner, id) != nil },
			func(int64, []int64) bool { return false })
	}
	for _, id := range ids {
		if !sees(id) {
			t.Errorf("user %d does not see the number: %v", id, rules.GetRules())
		}
	}
	if sees(stranger) {
		t.Errorf("a stranger sees the number: %v", rules.GetRules())
	}
	if n := count(t, "SELECT COUNT(*) FROM user_privacies WHERE user_id = ? AND key_type = ?", ann, mtproto.PHONE_NUMBER); n != 1 {
		t.Errorf("%d rows of the rules", n)
	}
}

// signUp creates an account as auth.signUp does: with its sign-up queued.
func signUp(t *testing.T, phone, first string) int64 {
	t.Helper()
	u, err := userCore(t).UserCreateNewUser(&user.TLUserCreateNewUser{SecretKeyId: itSeq.Add(1), Phone: phone, CountryCode: "US", FirstName: first})
	if err != nil {
		t.Fatal(err)
	}
	return u.Id()
}

// deliver delivers the queued sign-up of userId, as the worker does.
func deliver(t *testing.T, userId int64) {
	t.Helper()
	s := dao.ContactSignUp{UserId: userId}
	if err := itRaw.QueryRow("SELECT phone, created_at FROM contact_signups WHERE user_id = ?", userId).Scan(&s.Phone, &s.CreatedAt); err != nil {
		t.Fatalf("sign-up of %d: %v", userId, err)
	}
	if err := DeliverContactSignUp(context.Background(), itSvc, s); err != nil {
		t.Fatal(err)
	}
}

type outboxRow struct {
	topic     string
	recipient int64
	method    string
	payload   []byte
}

func eventsOf(t *testing.T, initiator int64) []outboxRow {
	t.Helper()
	rows, err := itRaw.Query("SELECT topic, recipient_id, method, payload FROM mq_outbox WHERE initiator_id = ? ORDER BY id", initiator)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []outboxRow
	for rows.Next() {
		var r outboxRow
		if err = rows.Scan(&r.topic, &r.recipient, &r.method, &r.payload); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestContactSignUpDelivered(t *testing.T) {
	ann, _ := newUser(t, "Ann")
	dan, _ := newUser(t, "Dan")
	eve, _ := newUser(t, "Eve")
	phone := nextPhone()
	importContacts(t, ann, phoneContact(1, "+"+phone, "Carl", "Phonebook"))
	importContacts(t, dan, phoneContact(1, "+"+phone, "C.", ""))
	importContacts(t, eve, phoneContact(1, "+"+phone, "Carl", ""))
	if _, err := userCore(t).UserSetContactSignUpNotification(&user.TLUserSetContactSignUpNotification{UserId: dan, Silent: mtproto.BoolTrue}); err != nil {
		t.Fatal(err)
	}
	// Eve deleted her account: she gets nothing, her row goes.
	if _, err := itRaw.Exec("UPDATE users SET deleted = 1, phone = CONCAT('-', id) WHERE id = ?", eve); err != nil {
		t.Fatal(err)
	}
	if err := itSvc.Dao.CachedConn.DelCache(context.Background(), dao.GenCacheUserDataCacheKey(eve)); err != nil {
		t.Fatal(err)
	}

	carl := signUp(t, phone, "Carl")
	deliver(t, carl)

	if n := count(t, "SELECT COUNT(*) FROM contact_signups WHERE user_id = ?", carl); n != 0 {
		t.Errorf("the sign-up is still queued")
	}
	if n := count(t, "SELECT COUNT(*) FROM unregistered_contacts WHERE phone = ?", phone); n != 0 {
		t.Errorf("%d phone book rows of the number left", n)
	}
	if c := contactOf(t, ann, carl); c == nil || c.first != "Carl" || c.last != "Phonebook" || c.phone != phone {
		t.Errorf("Ann's contact %+v", c)
	}
	if c := contactOf(t, dan, carl); c == nil || c.first != "C." {
		t.Errorf("Dan's contact %+v", c)
	}
	if contactOf(t, eve, carl) != nil {
		t.Errorf("a deleted importer got the contact")
	}

	events := eventsOf(t, carl)
	silent := map[int64]bool{}
	pushes := map[int64]bool{}
	for _, e := range events {
		switch e.topic {
		case "Inbox-T":
			var in inboxpb.TLInboxSendUserMessageToInboxV2
			if err := jsonx.Unmarshal(e.payload, &in); err != nil {
				t.Fatal(err)
			}
			box := in.GetBoxList()[0]
			if in.GetFromId() != carl || in.GetUserId() != e.recipient || box.GetDialogMessageId() <= 0 ||
				box.GetMessage().GetAction().GetPredicateName() != mtproto.Predicate_messageActionContactSignUp {
				t.Errorf("message to %d: %v", e.recipient, &in)
			}
			if len(in.GetUsers()) != 1 || in.GetUsers()[0].GetId() != carl || !in.GetUsers()[0].GetContact() {
				t.Errorf("the new user as %d sees them: %v", e.recipient, in.GetUsers())
			}
			silent[e.recipient] = box.GetMessage().GetSilent()
		case "Sync-T":
			var in syncpb.TLSyncPushUpdates
			if err := jsonx.Unmarshal(e.payload, &in); err != nil {
				t.Fatal(err)
			}
			ups := in.GetUpdates().GetUpdates()
			if len(ups) != 1 || ups[0].GetPredicateName() != mtproto.Predicate_updatePeerSettings || ups[0].GetPeer_PEER().GetUserId() != carl {
				t.Errorf("push to %d: %v", e.recipient, ups)
			}
			pushes[e.recipient] = true
		}
	}
	if len(silent) != 2 || silent[ann] || !silent[dan] {
		t.Errorf("messages %v: one to Ann, one silent to Dan, who turned the notifications off", silent)
	}
	if len(pushes) != 2 || !pushes[ann] || !pushes[dan] {
		t.Errorf("pushes %v", pushes)
	}

	// Once: a second delivery finds nothing to do.
	if err := DeliverContactSignUp(context.Background(), itSvc, dao.ContactSignUp{UserId: carl, Phone: phone}); err != nil {
		t.Fatal(err)
	}
	if n := len(eventsOf(t, carl)); n != len(events) {
		t.Errorf("%d events after a second delivery, want %d", n, len(events))
	}
}

// The worker delivers a sign-up it is told about.
func TestSignUpWorker(t *testing.T) {
	ann, _ := newUser(t, "Ann")
	phone := nextPhone()
	importContacts(t, ann, phoneContact(1, "+"+phone, "Fred", ""))

	w := NewSignUpWorker(itSvc)
	defer func() { itSvc.SignUps = nil }()
	w.Start()
	fred := signUp(t, phone, "Fred") // Notify
	deadline := time.Now().Add(20 * time.Second)
	for count(t, "SELECT COUNT(*) FROM contact_signups WHERE user_id = ?", fred) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the sign-up was not delivered")
		}
		time.Sleep(50 * time.Millisecond)
	}
	w.Stop()
	if contactOf(t, ann, fred) == nil {
		t.Errorf("Ann did not get Fred as a contact")
	}
}

// A number registered between the look-up and the transaction of an
// import is planned again; one registered before the import is a contact
// right away and gets no sign-up message.
func TestImportDuringSignUp(t *testing.T) {
	ann, _ := newUser(t, "Ann")
	phone := nextPhone()
	gus := signUp(t, phone, "Gus")

	entries := []*importEntry{{phone: phone, clientIds: []int64{1}, firstName: "Gus"}}
	_, _, err := userCore(t).runImport(ann, entries, map[string]importTarget{})
	if !errors.Is(err, errImportStale) {
		t.Fatalf("import planned before the sign-up: %v, want errImportStale", err)
	}
	if n := count(t, "SELECT COUNT(*) FROM unregistered_contacts WHERE importer_user_id = ?", ann); n != 0 {
		t.Errorf("the stale import wrote %d rows", n)
	}

	r := importContacts(t, ann, phoneContact(1, "+"+phone, "Gus", ""))
	if len(r.Imported) != 1 || r.Imported[0].UserId != gus {
		t.Errorf("imported %v", r.Imported)
	}
	deliver(t, gus)
	for _, e := range eventsOf(t, gus) {
		if e.recipient == ann {
			t.Errorf("Ann, who imported Gus after his sign-up, got %s", e.method)
		}
	}
}

func TestSavedContacts(t *testing.T) {
	ann, _ := newUser(t, "Ann")
	_, bobPhone := newUser(t, "Bob")
	stranger := nextPhone()
	importContacts(t, ann, phoneContact(1, "+"+bobPhone, "Bob", "B"), phoneContact(2, "+"+stranger, "Sam", "S"))

	saved, err := userCore(t).UserGetSavedContacts(&user.TLUserGetSavedContacts{UserId: ann})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Datas) != 2 || saved.Datas[0].GetPhone() != bobPhone || saved.Datas[0].GetFirstName() != "Bob" ||
		saved.Datas[1].GetPhone() != stranger || saved.Datas[1].GetLastName() != "S" || saved.Datas[1].GetDate() == 0 {
		t.Errorf("saved %v", saved.Datas)
	}

	if _, err = userCore(t).UserResetSavedContacts(&user.TLUserResetSavedContacts{UserId: ann}); err != nil {
		t.Fatal(err)
	}
	if saved, err = userCore(t).UserGetSavedContacts(&user.TLUserGetSavedContacts{UserId: ann}); err != nil || len(saved.Datas) != 1 || saved.Datas[0].GetPhone() != bobPhone {
		t.Errorf("after the reset: %v, %v (the contact stays)", saved, err)
	}
	if n := count(t, "SELECT COUNT(*) FROM unregistered_contacts WHERE importer_user_id = ?", ann); n != 0 {
		t.Errorf("%d phone book rows after the reset", n)
	}
}

func TestTopPeers(t *testing.T) {
	ann, _ := newUser(t, "Ann")
	bob, _ := newUser(t, "Bob")
	cid, _ := newUser(t, "Cid")
	dan, _ := newUser(t, "Dan")
	const group = 4242
	now := time.Now().Unix()
	box := int32(0)
	insert := func(sender int64, peerType int32, peerId int64, date int64, data string) {
		t.Helper()
		box++
		if _, err := itRaw.Exec("INSERT INTO messages (user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id, peer_type, peer_id, message_data, date2)"+
			" VALUES (?, ?, 0, 0, ?, ?, ?, ?, ?, ?)", ann, box, itSeq.Add(1), sender, peerType, peerId, data, date); err != nil {
			t.Fatal(err)
		}
	}
	const text, forwarded, service = `{"predicate_name":"message","message":"hi"}`, `{"predicate_name":"message","fwd_from":{"predicate_name":"messageFwdHeader"}}`, `{"predicate_name":"messageService"}`
	insert(ann, mtproto.PEER_USER, cid, now-40*86400, text)
	insert(ann, mtproto.PEER_USER, cid, now-40*86400, forwarded)
	insert(ann, mtproto.PEER_USER, bob, now-3600, text)
	insert(ann, mtproto.PEER_USER, bob, now-60, text)
	insert(ann, mtproto.PEER_USER, bob, now-30, service) // does not count
	insert(dan, mtproto.PEER_USER, dan, now-10, text)    // incoming: does not count
	insert(ann, mtproto.PEER_CHAT, group, now-100, text)

	categories := []*mtproto.TopPeerCategory{
		mtproto.MakeTLTopPeerCategoryCorrespondents(nil).To_TopPeerCategory(),
		mtproto.MakeTLTopPeerCategoryGroups(nil).To_TopPeerCategory(),
		mtproto.MakeTLTopPeerCategoryForwardUsers(nil).To_TopPeerCategory(),
		mtproto.MakeTLTopPeerCategoryPhoneCalls(nil).To_TopPeerCategory(),
	}
	peersOf := func() map[string][]int64 {
		t.Helper()
		r, err := userCore(t).UserGetTopPeers(&user.TLUserGetTopPeers{UserId: ann, Categories: categories})
		if err != nil {
			t.Fatal(err)
		}
		if r.GetPredicateName() == mtproto.Predicate_contacts_topPeersDisabled {
			return nil
		}
		out := map[string][]int64{}
		for _, c := range r.GetCategories() {
			ids := []int64{}
			for _, p := range c.GetPeers() {
				if p.GetPeer().GetPredicateName() == mtproto.Predicate_peerChat {
					ids = append(ids, p.GetPeer().GetChatId())
				} else {
					ids = append(ids, p.GetPeer().GetUserId())
				}
			}
			out[c.GetCategory().GetPredicateName()] = ids
		}
		return out
	}
	got := peersOf()
	want := map[string][]int64{
		mtproto.Predicate_topPeerCategoryCorrespondents: {bob, cid},
		mtproto.Predicate_topPeerCategoryGroups:         {group},
		mtproto.Predicate_topPeerCategoryForwardUsers:   {cid},
		mtproto.Predicate_topPeerCategoryPhoneCalls:     {},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("top peers %v, want %v", got, want)
	}

	if _, err := userCore(t).UserResetTopPeerRating(&user.TLUserResetTopPeerRating{UserId: ann, Category: categories[0], PeerType: mtproto.PEER_USER, PeerId: bob}); err != nil {
		t.Fatal(err)
	}
	if got = peersOf(); fmt.Sprint(got[mtproto.Predicate_topPeerCategoryCorrespondents]) != fmt.Sprint([]int64{cid}) {
		t.Errorf("after resetting Bob: %v", got)
	}

	if _, err := userCore(t).UserToggleTopPeers(&user.TLUserToggleTopPeers{UserId: ann, Enabled: mtproto.BoolFalse}); err != nil {
		t.Fatal(err)
	}
	if got = peersOf(); got != nil {
		t.Errorf("off: %v", got)
	}
	if n := count(t, "SELECT COUNT(*) FROM user_top_peer_resets WHERE user_id = ?", ann); n != 0 {
		t.Errorf("resets kept while off")
	}
	// Back on: the messages before count no more.
	if _, err := userCore(t).UserToggleTopPeers(&user.TLUserToggleTopPeers{UserId: ann, Enabled: mtproto.BoolTrue}); err != nil {
		t.Fatal(err)
	}
	if got = peersOf(); len(got[mtproto.Predicate_topPeerCategoryCorrespondents]) != 0 || len(got) != len(categories) {
		t.Errorf("back on: %v", got)
	}
	insert(ann, mtproto.PEER_USER, dan, time.Now().Unix()+1, text)
	if got = peersOf(); fmt.Sprint(got[mtproto.Predicate_topPeerCategoryCorrespondents]) != fmt.Sprint([]int64{dan}) {
		t.Errorf("a new message: %v", got)
	}
}

// The migration finds the phone book as the previous version left it:
// rows of signed-up numbers marked imported, no index of the importer.
func TestContactsMigration(t *testing.T) {
	path, err := testinfra.SchemaFile("migrate-20261024-contacts.sql")
	if err != nil {
		t.Fatal(err)
	}
	importer, _ := newUser(t, "Old")
	live, signedUp := nextPhone(), nextPhone()
	if _, err = itRaw.Exec("INSERT INTO unregistered_contacts (phone, importer_user_id, import_first_name, import_last_name, imported) VALUES (?, ?, 'L', '', 0), (?, ?, 'S', '', 1)",
		live, importer, signedUp, importer); err != nil {
		t.Fatal(err)
	}
	if _, err = itRaw.Exec("ALTER TABLE unregistered_contacts DROP INDEX importer_user_id"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for run := range 2 {
		if err = itDB.ApplySQLFile(ctx, path); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if n := count(t, "SELECT COUNT(*) FROM unregistered_contacts WHERE importer_user_id = ?", importer); n != 1 {
			t.Errorf("run %d: %d rows, want the live one", run, n)
		}
		if n := count(t, "SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'unregistered_contacts' AND INDEX_NAME = 'importer_user_id'"); n != 1 {
			t.Errorf("run %d: index importer_user_id %d", run, n)
		}
		for _, table := range []string{"contact_import_quota", "contact_signups", "user_top_peer_resets"} {
			if n := count(t, "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?", table); n != 1 {
				t.Errorf("run %d: table %s", run, table)
			}
		}
	}
}
