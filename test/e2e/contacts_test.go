//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// Contacts by phone number (task 011): the phone book sync of the official
// clients (contacts.importContacts), the sign-up of an imported number, the
// addedByPhone privacy and the phone book on the server.

// formattedPhone writes +12015550123 the way a phone book does.
func formattedPhone(phone string) string {
	d := strings.TrimPrefix(phone, "+")
	return fmt.Sprintf("+%s (%s) %s-%s", d[:1], d[1:4], d[4:7], d[7:])
}

func phoneContact(c *mtclient.Client, clientId int64, phone, first, last string) *mtproto.InputContact {
	return &mtproto.InputContact{
		Constructor: c.Constructor(mtproto.Predicate_inputPhoneContact),
		ClientId:    clientId,
		Phone:       phone,
		FirstName:   first,
		LastName:    last,
	}
}

func importContacts(ctx context.Context, t *testing.T, u *user, contacts ...*mtproto.InputContact) *mtproto.Contacts_ImportedContacts {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLContactsImportContacts{
		Constructor: u.c.Constructor(mtproto.Predicate_contacts_importContacts),
		Contacts:    contacts,
	})
	if err != nil {
		t.Fatalf("contacts.importContacts: %v", err)
	}
	r, ok := o.(interface {
		To_Contacts_ImportedContacts() *mtproto.Contacts_ImportedContacts
	})
	if !ok {
		t.Fatalf("contacts.importContacts returned %T", o)
	}
	return r.To_Contacts_ImportedContacts()
}

// contactIds returns the user ids of u's contacts.getContacts, in order.
func contactIds(ctx context.Context, t *testing.T, u *user) []int64 {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLContactsGetContacts{Constructor: u.c.Constructor(mtproto.Predicate_contacts_getContacts)})
	if err != nil {
		t.Fatalf("contacts.getContacts: %v", err)
	}
	r, ok := o.(interface {
		To_Contacts_Contacts() *mtproto.Contacts_Contacts
	})
	if !ok {
		t.Fatalf("contacts.getContacts returned %T", o)
	}
	var ids []int64
	for _, c := range r.To_Contacts_Contacts().GetContacts() {
		ids = append(ids, c.GetUserId())
	}
	return ids
}

func countOf(ids []int64, id int64) int {
	n := 0
	for _, v := range ids {
		if v == id {
			n++
		}
	}
	return n
}

// savedPhones returns u's contacts.getSaved, a method of the takeout API:
// wrapped in invokeWithTakeout, by phone.
func savedPhones(ctx context.Context, t *testing.T, u *user) map[string]*mtproto.SavedContact {
	t.Helper()
	x := mtproto.NewEncodeBuf(16)
	if err := (&mtproto.TLContactsGetSaved{Constructor: u.c.Constructor(mtproto.Predicate_contacts_getSaved)}).Encode(x, u.c.Layer()); err != nil {
		t.Fatal(err)
	}
	raw, err := u.c.InvokeRaw(ctx, &mtproto.TLInvokeWithTakeout{
		Constructor: u.c.Constructor(mtproto.Predicate_invokeWithTakeout),
		TakeoutId:   randomID() | 1,
		Query:       x.GetBuf(),
	})
	if err != nil {
		t.Fatalf("contacts.getSaved: %v", err)
	}
	list, err := mtclient.DecodeVector(raw)
	if err != nil {
		t.Fatal(err)
	}
	saved := make(map[string]*mtproto.SavedContact, len(list))
	for _, o := range list {
		s, ok := o.(interface{ To_SavedContact() *mtproto.SavedContact })
		if !ok {
			t.Fatalf("contacts.getSaved holds %T", o)
		}
		saved[s.To_SavedContact().GetPhone()] = s.To_SavedContact()
	}
	return saved
}

// freePhone returns a test phone without an account: contacts.resolvePhone
// of asker does not find it.
func freePhone(ctx context.Context, t *testing.T, asker *user) string {
	t.Helper()
	for range 100 {
		phone := testPhones(t, 1)[0]
		_, err := asker.c.Invoke(ctx, &mtproto.TLContactsResolvePhone{
			Constructor: asker.c.Constructor(mtproto.Predicate_contacts_resolvePhone),
			Phone:       strings.TrimPrefix(phone, "+"),
		})
		if mtclient.IsRPCError(err, "PHONE_NOT_OCCUPIED") {
			return phone
		}
		if err != nil {
			t.Fatalf("contacts.resolvePhone: %v", err)
		}
	}
	t.Fatal("no free test phone")
	return ""
}

// The phone book sync finds a registered user: imported with the client_id
// of the number, the user as a contact, and a repeated sync adds nothing.
func TestContactsImportRegistered(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")

	for round := range 2 {
		r := importContacts(ctx, t, alice, phoneContact(alice.c, 7001, formattedPhone(bob.phone), "Bobby", "Phonebook"))
		if len(r.GetImported()) != 1 || r.GetImported()[0].GetUserId() != bob.self.GetId() || r.GetImported()[0].GetClientId() != 7001 {
			t.Fatalf("round %d: imported %v, want Bob (%d) with client_id 7001", round, r.GetImported(), bob.self.GetId())
		}
		if len(r.GetUsers()) != 1 || r.GetUsers()[0].GetId() != bob.self.GetId() || !r.GetUsers()[0].GetContact() ||
			r.GetUsers()[0].GetFirstName().GetValue() != "Bobby" || r.GetUsers()[0].GetAccessHash() == nil {
			t.Errorf("round %d: users %v, want Bob as a contact named as in the phone book", round, r.GetUsers())
		}
		if len(r.GetPopularInvites()) != 0 || len(r.GetRetryContacts()) != 0 {
			t.Errorf("round %d: popular %v, retry %v", round, r.GetPopularInvites(), r.GetRetryContacts())
		}
		if n := countOf(contactIds(ctx, t, alice), bob.self.GetId()); n != 1 {
			t.Errorf("round %d: Bob %d times in contacts.getContacts", round, n)
		}
		if s, ok := savedPhones(ctx, t, alice)[strings.TrimPrefix(bob.phone, "+")]; !ok || s.GetFirstName() != "Bobby" {
			t.Errorf("round %d: contacts.getSaved has no Bob: %v", round, s)
		}
	}
}

// An imported number without an account is an invite; when it signs up,
// the importer gets the service message messageActionContactSignUp in the
// chat with the new user, and the user among the contacts.
func TestContactsSignUpOfImported(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	alice := login(ctx, t, testPhones(t, 1)[0], "Alice")
	// As the clients do after signing in: the session takes pushed updates
	// once it asked for the update state.
	getState(ctx, t, alice)
	carolPhone := freePhone(ctx, t, alice)

	r := importContacts(ctx, t, alice, phoneContact(alice.c, 42, carolPhone, "Carol", "Phonebook"))
	if len(r.GetImported()) != 0 || len(r.GetPopularInvites()) != 1 || r.GetPopularInvites()[0].GetClientId() != 42 || r.GetPopularInvites()[0].GetImporters() < 1 {
		t.Fatalf("imported %v, popular %v: want an invite for client_id 42", r.GetImported(), r.GetPopularInvites())
	}
	// The notifications are on: not silent.
	o, err := alice.c.Invoke(ctx, &mtproto.TLAccountGetContactSignUpNotification{Constructor: alice.c.Constructor(mtproto.Predicate_account_getContactSignUpNotification)})
	if err != nil {
		t.Fatalf("account.getContactSignUpNotification: %v", err)
	}
	if b, ok := o.(*mtproto.TLBoolFalse); !ok {
		t.Errorf("account.getContactSignUpNotification %T %v, want boolFalse (not silent)", o, b)
	}
	st := getState(ctx, t, alice)

	carol := login(ctx, t, carolPhone, "Carol")

	signUp, err := alice.c.WaitUpdate(ctx, func(u *mtproto.Updates) bool {
		for _, m := range newMessages(u) {
			if m.GetAction().GetPredicateName() == mtproto.Predicate_messageActionContactSignUp && m.GetPeerId().GetUserId() == carol.self.GetId() {
				return true
			}
		}
		return false
	})
	if err != nil {
		t.Fatalf("Alice got no messageActionContactSignUp of Carol: %v", err)
	}
	for _, m := range newMessages(signUp) {
		if m.GetAction().GetPredicateName() == mtproto.Predicate_messageActionContactSignUp && (m.GetOut() || m.GetSilent()) {
			t.Errorf("the sign-up message: out %v, silent %v", m.GetOut(), m.GetSilent())
		}
	}
	var seen *mtproto.User
	for _, u := range signUp.GetUsers() {
		if u.GetId() == carol.self.GetId() {
			seen = u
		}
	}
	if seen == nil || !seen.GetContact() || seen.GetFirstName().GetValue() != "Carol" || seen.GetLastName().GetValue() != "Phonebook" {
		t.Errorf("Carol with the message: %v, want a contact with the name of the phone book", seen)
	}
	// The contact comes as from contacts.addContact: updatePeerSettings
	// with the user (Android ContactsController applies contacts from it),
	// with Alice's next seq (ADR 0021).
	isCarol := func(up *mtproto.Update) bool { return up.GetPeer_PEER().GetUserId() == carol.self.GetId() }
	settings := seqPush(ctx, t, alice, "Alice", st.GetSeq()+1, mtproto.Predicate_updatePeerSettings, isCarol)
	if u := userIn(settings.GetUsers(), carol.self.GetId()); !u.GetContact() {
		t.Errorf("Carol with updatePeerSettings: %v, want a contact", u)
	}
	// A device that missed the push finds it in its difference.
	d := differenceOnce(ctx, t, alice, st)
	var inDiff []*mtproto.Update
	for _, up := range updatesOfType(d, mtproto.Predicate_updatePeerSettings) {
		if isCarol(up) {
			inDiff = append(inDiff, up)
		}
	}
	if len(inDiff) != 1 || !userIn(d.GetUsers(), carol.self.GetId()).GetContact() {
		t.Errorf("updatePeerSettings of Carol in the difference %v with Carol %v, want one with Carol as a contact", inDiff, userIn(d.GetUsers(), carol.self.GetId()))
	}
	if got, want := d.GetState().GetSeq(), st.GetSeq()+1; got != want {
		t.Errorf("state seq of the difference %d, want %d", got, want)
	}
	if n := countOf(contactIds(ctx, t, alice), carol.self.GetId()); n != 1 {
		t.Errorf("Carol %d times in Alice's contacts.getContacts", n)
	}
	if _, ok := savedPhones(ctx, t, alice)[strings.TrimPrefix(carolPhone, "+")]; !ok {
		t.Errorf("Carol's number left contacts.getSaved: it is a contact's now")
	}
}

func setAddedByPhone(ctx context.Context, t *testing.T, u *user, rule string) {
	t.Helper()
	if _, err := u.c.Invoke(ctx, &mtproto.TLAccountSetPrivacy{
		Constructor: u.c.Constructor(mtproto.Predicate_account_setPrivacy),
		Key:         &mtproto.InputPrivacyKey{Constructor: u.c.Constructor(mtproto.Predicate_inputPrivacyKeyAddedByPhone)},
		Rules:       []*mtproto.InputPrivacyRule{{Constructor: u.c.Constructor(rule)}},
	}); err != nil {
		t.Fatalf("account.setPrivacy: %v", err)
	}
}

// Who lets only contacts find them by the number is, to anyone else, a
// number without an account.
func TestContactsAddedByPhonePrivacy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	setAddedByPhone(ctx, t, bob, mtproto.Predicate_inputPrivacyValueAllowContacts)
	t.Cleanup(func() {
		// Other tests may pick Bob's number.
		setAddedByPhone(context.Background(), t, bob, mtproto.Predicate_inputPrivacyValueAllowAll)
	})

	r := importContacts(ctx, t, alice, phoneContact(alice.c, 9, bob.phone, "Bob", ""))
	if len(r.GetImported()) != 0 || len(r.GetUsers()) != 0 {
		t.Errorf("imported %v, users %v: Bob is not to be found", r.GetImported(), r.GetUsers())
	}
	if len(r.GetPopularInvites()) != 1 || r.GetPopularInvites()[0].GetClientId() != 9 {
		t.Errorf("popular %v: the number answers as one without an account", r.GetPopularInvites())
	}
	if countOf(contactIds(ctx, t, alice), bob.self.GetId()) != 0 {
		t.Errorf("Bob is among Alice's contacts")
	}
}

// contacts.getSaved returns the synced numbers; contacts.resetSaved, which
// the clients call when sync is turned off, removes those without an
// account.
func TestContactsSavedAndReset(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	stranger := freePhone(ctx, t, alice)
	importContacts(ctx, t, alice,
		phoneContact(alice.c, 1, bob.phone, "Bob", ""),
		phoneContact(alice.c, 2, stranger, "Stranger", ""))

	_, err := alice.c.Invoke(ctx, &mtproto.TLContactsGetSaved{Constructor: alice.c.Constructor(mtproto.Predicate_contacts_getSaved)})
	if !mtclient.IsRPCError(err, "TAKEOUT_REQUIRED") {
		t.Errorf("contacts.getSaved outside a takeout: %v, want TAKEOUT_REQUIRED", err)
	}
	saved := savedPhones(ctx, t, alice)
	bobDigits, strangerDigits := strings.TrimPrefix(bob.phone, "+"), strings.TrimPrefix(stranger, "+")
	if saved[bobDigits] == nil || saved[strangerDigits] == nil || saved[strangerDigits].GetFirstName() != "Stranger" {
		t.Fatalf("contacts.getSaved %v, want Bob's and the stranger's numbers", saved)
	}

	o, err := alice.c.Invoke(ctx, &mtproto.TLContactsResetSaved{Constructor: alice.c.Constructor(mtproto.Predicate_contacts_resetSaved)})
	if err != nil {
		t.Fatalf("contacts.resetSaved: %v", err)
	}
	if _, ok := o.(*mtproto.TLBoolTrue); !ok {
		t.Errorf("contacts.resetSaved returned %T", o)
	}
	saved = savedPhones(ctx, t, alice)
	if saved[strangerDigits] != nil || saved[bobDigits] == nil {
		t.Errorf("after contacts.resetSaved: %v, want only Bob's number (a contact)", saved)
	}
	if countOf(contactIds(ctx, t, alice), bob.self.GetId()) != 1 {
		t.Errorf("contacts.resetSaved removed the contact")
	}
}
