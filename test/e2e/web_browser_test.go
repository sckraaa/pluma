//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// The in-app browser settings (task 098): iOS asks for them on start and
// every hour and, while the method failed, repeated it every few seconds
// (TelegramCore/Sources/Network/Network.swift:1513-1525); Android keeps
// them and changes them from its settings screen. The other devices of the
// account learn every change by push.
func TestWebBrowserSettings(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	alice := login(ctx, t, phone, "Alice")
	laptop := login(ctx, t, phone, "Alice")
	getState(ctx, t, alice)
	getState(ctx, t, laptop)

	get := func(hash int64) *mtproto.Account_WebBrowserSettings {
		t.Helper()
		o, err := alice.c.Invoke(ctx, &mtproto.TLAccountGetWebBrowserSettings{
			Constructor: alice.c.Constructor(mtproto.Predicate_account_getWebBrowserSettings),
			Hash:        hash,
		})
		if err != nil {
			t.Fatalf("account.getWebBrowserSettings: %v", err)
		}
		return o.(interface {
			To_Account_WebBrowserSettings() *mtproto.Account_WebBrowserSettings
		}).To_Account_WebBrowserSettings()
	}
	toggle := func(del bool, external *mtproto.Bool, url string) *mtproto.Update {
		t.Helper()
		o, err := alice.c.Invoke(ctx, &mtproto.TLAccountToggleWebBrowserSettingsException60ED4229{
			Constructor:         alice.c.Constructor(mtproto.Predicate_account_toggleWebBrowserSettingsException60ED4229),
			Delete:              del,
			OpenExternalBrowser: external,
			Url:                 url,
		})
		if err != nil {
			t.Fatalf("account.toggleWebBrowserSettingsException %s: %v", url, err)
		}
		ups := updatesOf(t, "account.toggleWebBrowserSettingsException", o).GetUpdates()
		if len(ups) != 1 || ups[0].GetPredicateName() != mtproto.Predicate_updateWebBrowserException {
			t.Fatalf("account.toggleWebBrowserSettingsException %s: updates %v", url, ups)
		}
		return ups[0]
	}
	// waitException waits on the laptop for the push of an
	// updateWebBrowserException of domain with delete del.
	waitException := func(domain string, del bool) *mtproto.Update {
		t.Helper()
		var found *mtproto.Update
		_, err := laptop.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
			for _, x := range us.GetUpdates() {
				if x.GetPredicateName() == mtproto.Predicate_updateWebBrowserException &&
					x.GetException_WEBDOMAINEXCEPTION().GetDomain() == domain && x.GetDelete() == del {
					found = x
					return true
				}
			}
			return false
		})
		if err != nil {
			t.Fatalf("no updateWebBrowserException %s (delete %v) on the other device: %v", domain, del, err)
		}
		return found
	}
	domainsOf := func(list []*mtproto.WebDomainException) []string {
		var out []string
		for _, e := range list {
			out = append(out, e.GetDomain())
		}
		return out
	}

	// A new account: the defaults, with a hash; the same hash again is
	// webBrowserSettingsNotModified.
	s := get(0)
	if s.GetPredicateName() != mtproto.Predicate_account_webBrowserSettings || s.GetOpenExternalBrowser() || !s.GetDisplayCloseButton() || s.GetHash() == 0 {
		t.Fatalf("settings of a new account: %v", s)
	}
	if got := get(s.GetHash()); got.GetPredicateName() != mtproto.Predicate_account_webBrowserSettingsNotModified {
		t.Errorf("the same hash: %s, want account.webBrowserSettingsNotModified", got.GetPredicateName())
	}

	// The external browser: the answer has it, the laptop learns it.
	o, err := alice.c.Invoke(ctx, &mtproto.TLAccountUpdateWebBrowserSettings{
		Constructor:         alice.c.Constructor(mtproto.Predicate_account_updateWebBrowserSettings),
		OpenExternalBrowser: true,
		DisplayCloseButton:  true,
	})
	if err != nil {
		t.Fatalf("account.updateWebBrowserSettings: %v", err)
	}
	if r := o.(interface {
		To_Account_WebBrowserSettings() *mtproto.Account_WebBrowserSettings
	}).To_Account_WebBrowserSettings(); !r.GetOpenExternalBrowser() || !r.GetDisplayCloseButton() || r.GetHash() == s.GetHash() {
		t.Errorf("account.updateWebBrowserSettings: %v", r)
	}
	if _, err = laptop.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, x := range us.GetUpdates() {
			if x.GetPredicateName() == mtproto.Predicate_updateWebBrowserSettings && x.GetOpenExternalBrowser_FLAGBOOLEAN() && x.GetDisplayCloseButton() {
				return true
			}
		}
		return false
	}); err != nil {
		t.Fatalf("no updateWebBrowserSettings on the other device: %v", err)
	}

	// An exception: in the answer and on the laptop; into the other list
	// it moves; deleted, it is gone.
	u := toggle(false, mtproto.BoolFalse, "https://Example.org/page")
	if e := u.GetException_WEBDOMAINEXCEPTION(); e.GetDomain() != "example.org" || e.GetUrl() != "https://Example.org/page" || e.GetTitle() != "example.org" ||
		mtproto.FromBool(u.GetOpenExternalBrowser_FLAGBOOL()) || u.GetDelete() {
		t.Errorf("update of the new exception: %v", u)
	}
	waitException("example.org", false)
	if s = get(0); len(s.GetInappExceptions()) != 1 || len(s.GetExternalExceptions()) != 0 {
		t.Fatalf("after the exception: %v / %v", domainsOf(s.GetExternalExceptions()), domainsOf(s.GetInappExceptions()))
	}
	toggle(false, mtproto.BoolTrue, "example.org")
	if s = get(0); len(s.GetInappExceptions()) != 0 || len(s.GetExternalExceptions()) != 1 || s.GetExternalExceptions()[0].GetDomain() != "example.org" {
		t.Fatalf("after the move: %v / %v", domainsOf(s.GetExternalExceptions()), domainsOf(s.GetInappExceptions()))
	}
	if u = toggle(true, nil, "https://example.org/page"); !u.GetDelete() {
		t.Errorf("update of the deletion: %v", u)
	}
	waitException("example.org", true)
	if s = get(0); len(s.GetInappExceptions())+len(s.GetExternalExceptions()) != 0 {
		t.Fatalf("after the deletion: %v / %v", domainsOf(s.GetExternalExceptions()), domainsOf(s.GetInappExceptions()))
	}

	// An exception without a host is refused.
	if _, err = alice.c.Invoke(ctx, &mtproto.TLAccountToggleWebBrowserSettingsException60ED4229{
		Constructor:         alice.c.Constructor(mtproto.Predicate_account_toggleWebBrowserSettingsException60ED4229),
		OpenExternalBrowser: mtproto.BoolTrue,
		Url:                 "https://",
	}); !mtclient.IsRPCError(err, "URL_INVALID") {
		t.Errorf("an address without a host: %v, want URL_INVALID", err)
	}

	// Delete all: the lists are empty, the flags stay; the laptop learns
	// each domain removed.
	toggle(false, mtproto.BoolFalse, "a.example.net")
	toggle(false, mtproto.BoolTrue, "b.example.net")
	o, err = alice.c.Invoke(ctx, &mtproto.TLAccountDeleteWebBrowserSettingsExceptions{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_deleteWebBrowserSettingsExceptions),
	})
	if err != nil {
		t.Fatalf("account.deleteWebBrowserSettingsExceptions: %v", err)
	}
	r := o.(interface {
		To_Account_WebBrowserSettings() *mtproto.Account_WebBrowserSettings
	}).To_Account_WebBrowserSettings()
	if !r.GetOpenExternalBrowser() || !r.GetDisplayCloseButton() || len(r.GetExternalExceptions())+len(r.GetInappExceptions()) != 0 {
		t.Errorf("account.deleteWebBrowserSettingsExceptions: %v", r)
	}
	if _, err = laptop.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		deleted := map[string]bool{}
		for _, x := range us.GetUpdates() {
			if x.GetPredicateName() == mtproto.Predicate_updateWebBrowserException && x.GetDelete() {
				deleted[x.GetException_WEBDOMAINEXCEPTION().GetDomain()] = true
			}
		}
		return deleted["a.example.net"] && deleted["b.example.net"]
	}); err != nil {
		t.Fatalf("no push of the deleted exceptions on the other device: %v", err)
	}
	if got := get(r.GetHash()); got.GetPredicateName() != mtproto.Predicate_account_webBrowserSettingsNotModified {
		t.Errorf("the hash of the answer: %s, want account.webBrowserSettingsNotModified", got.GetPredicateName())
	}
}
