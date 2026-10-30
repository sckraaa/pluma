//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// authorizations returns u's account.getAuthorizations: its own session and
// the others.
func authorizations(ctx context.Context, t *testing.T, u *user) (current *mtproto.Authorization, others []*mtproto.Authorization) {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLAccountGetAuthorizations{Constructor: u.c.Constructor(mtproto.Predicate_account_getAuthorizations)})
	if err != nil {
		t.Fatalf("account.getAuthorizations: %v", err)
	}
	list, ok := o.(*mtproto.TLAccountAuthorizations)
	if !ok {
		t.Fatalf("account.getAuthorizations returned %T", o)
	}
	for _, a := range list.GetAuthorizations() {
		if a.GetCurrent() {
			current = a
		} else {
			others = append(others, a)
		}
	}
	return current, others
}

// newAuthorization waits for u's push of updateNewAuthorization of hash with
// unconfirmed as given.
func newAuthorization(ctx context.Context, t *testing.T, u *user, name string, hash int64, unconfirmed bool) *mtproto.Update {
	t.Helper()
	wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var got *mtproto.Update
	_, err := u.c.WaitUpdate(wctx, func(us *mtproto.Updates) bool {
		for _, x := range pushedUpdates(us) {
			if x.GetPredicateName() == mtproto.Predicate_updateNewAuthorization && x.GetHash() == hash && x.GetUnconfirmed() == unconfirmed {
				got = x
				return true
			}
		}
		return false
	})
	if err != nil {
		t.Fatalf("%s: no updateNewAuthorization of %d (unconfirmed %v): %v", name, hash, unconfirmed, err)
	}
	return got
}

// A new login of an account that has other sessions (task 019), as Telegram
// does it (docs/tasks/019-service-messages.md): the other sessions get the
// New login message and updateNewAuthorization with unconfirmed, on which
// official clients ask "Is it you?"; the new session gets neither. The login
// stays unconfirmed in the other sessions' account.getAuthorizations until
// one of them confirms it; then every session gets updateNewAuthorization
// without unconfirmed and drops the banner. The new session never sees
// itself unconfirmed. A session that missed the push finds the update in
// getDifference.
func TestNewLogin(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]

	// The first session has nobody to ask: it is confirmed.
	phoneApp := loginIn(ctx, t, phone, "Lena", "ru")
	if current, others := authorizations(ctx, t, phoneApp); current == nil || current.GetUnconfirmed() || len(others) != 0 {
		t.Fatalf("the first session: %v, others %v", current, others)
	}
	st := getState(ctx, t, phoneApp)

	// A login with a code: the other session is told in its language, the
	// one that connected last.
	web := login(ctx, t, phone, "Lena")
	getState(ctx, t, web)
	_, others := authorizations(ctx, t, phoneApp)
	if len(others) != 1 || !others[0].GetUnconfirmed() || others[0].GetHash() == 0 {
		t.Fatalf("the new login as the first session sees it: %v", others)
	}
	hash := others[0].GetHash()
	// The banner asks about the login of another session, and Web K shows
	// one for every authorization in this list with unconfirmed: the new
	// session does not see itself unconfirmed. It sees the first session
	// confirmed.
	current, firstSession := authorizations(ctx, t, web)
	if current == nil || current.GetUnconfirmed() || current.GetHash() != 0 {
		t.Fatalf("the new login as it sees itself: %v", current)
	}
	if len(firstSession) != 1 || firstSession[0].GetUnconfirmed() {
		t.Fatalf("the first session as the new login sees it: %v", firstSession)
	}

	banner := newAuthorization(ctx, t, phoneApp, "the first session", hash, true)
	if banner.GetDate_FLAGINT32().GetValue() == 0 || banner.GetDevice().GetValue() == "" || banner.GetLocation() == nil {
		t.Errorf("banner without its details: %v", banner)
	}
	// The New login message, in the language of the session that connected
	// last: Russian. The account has no password, and no Russian text of
	// Telegram has the paragraph about one: the English message, whole.
	wctx, wcancel := context.WithTimeout(ctx, 30*time.Second)
	var message *mtproto.Update
	_, err := phoneApp.c.WaitUpdate(wctx, func(us *mtproto.Updates) bool {
		for _, x := range pushedUpdates(us) {
			if x.GetPredicateName() == mtproto.Predicate_updateServiceNotification {
				message = x
				return true
			}
		}
		return false
	})
	wcancel()
	if err != nil {
		t.Fatalf("no New login message: %v", err)
	}
	if text := message.GetMessage_STRING(); message.GetInboxDate() == nil || len(message.GetEntities()) != 4 ||
		!strings.HasPrefix(text, "New login. Dear Lena E2E, we detected a login into your account from a new device on ") ||
		!strings.HasSuffix(text, "you can enable Two-Step Verification in Privacy and Security settings.") {
		t.Fatalf("New login message %q, %d entities", text, len(message.GetEntities()))
	}

	// The session that missed the push finds the banner in its difference.
	d := differenceUntil(ctx, t, phoneApp, st, func(d *mtproto.Updates_Difference) bool {
		for _, x := range d.GetOtherUpdates() {
			if x.GetPredicateName() == mtproto.Predicate_updateNewAuthorization && x.GetHash() == hash {
				return true
			}
		}
		return false
	})
	found := false
	for _, x := range d.GetOtherUpdates() {
		found = found || (x.GetPredicateName() == mtproto.Predicate_updateNewAuthorization && x.GetHash() == hash && x.GetUnconfirmed())
	}
	if !found {
		t.Fatalf("no banner in the difference: %v", d.GetOtherUpdates())
	}

	// The new session is told nothing about its own login.
	noPushOf(ctx, t, web, "the new session", 2*time.Second, mtproto.Predicate_updateNewAuthorization, func(*mtproto.Update) bool { return true })
	noPushOf(ctx, t, web, "the new session", time.Second, mtproto.Predicate_updateServiceNotification, func(*mtproto.Update) bool { return true })

	// A session younger than a day may not terminate another one.
	if _, err = web.c.Invoke(ctx, &mtproto.TLAccountResetAuthorization{
		Constructor: web.c.Constructor(mtproto.Predicate_account_resetAuthorization),
		Hash:        1,
	}); !rpcErrorIs(err, "FRESH_RESET_AUTHORISATION_FORBIDDEN") {
		t.Fatalf("a fresh session terminates another: %v", err)
	}

	// "Yes, it's me".
	changeSettings := func(u *user, hash int64, confirmed bool) error {
		_, err := u.c.Invoke(ctx, &mtproto.TLAccountChangeAuthorizationSettings{
			Constructor: u.c.Constructor(mtproto.Predicate_account_changeAuthorizationSettings),
			Hash:        hash,
			Confirmed:   confirmed,
		})
		return err
	}
	if err = changeSettings(phoneApp, hash+1, true); !rpcErrorIs(err, "HASH_INVALID") {
		t.Fatalf("an unknown session: %v, want HASH_INVALID", err)
	}
	if err = changeSettings(phoneApp, hash, true); err != nil {
		t.Fatalf("account.changeAuthorizationSettings: %v", err)
	}
	newAuthorization(ctx, t, phoneApp, "the first session", hash, false)
	newAuthorization(ctx, t, web, "the new session", hash, false)
	if _, others = authorizations(ctx, t, phoneApp); len(others) != 1 || others[0].GetUnconfirmed() {
		t.Fatalf("after the confirmation: %v", others)
	}
}
