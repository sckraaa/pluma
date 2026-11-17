//go:build e2e

package e2e

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/business"
	"github.com/teamgram/teamgram-server/pkg/mtclient"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// linksAddr is the address of the service links of the stand (the pages
// of pluma.chat links), set by run.sh.
var linksAddr = os.Getenv("PLUMA_E2E_LINKS")

// The business chat links (task 105, part 2): the owner creates, edits,
// lists and deletes them, naming a link by the whole link as the clients
// do; another user opens one and gets the chat and the text, which counts
// a view, while the owner's own opening does not; the 101st link is
// refused; the page of the link hands off to plm://message?slug=.
func TestBusinessChatLinks(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	bobForAlice := inputUserOf(ctx, t, alice, bob)

	input := func(message, title string, entities ...*mtproto.MessageEntity) *mtproto.InputBusinessChatLink {
		l := &mtproto.InputBusinessChatLink{
			Constructor: alice.c.Constructor(mtproto.Predicate_inputBusinessChatLink),
			Message:     message,
			Entities:    entities,
		}
		if title != "" {
			l.Title = wrapperspb.String(title)
		}
		return l
	}
	asLink := func(method string, o mtproto.TLObject, err error) *mtproto.BusinessChatLink {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		l, ok := o.(interface {
			To_BusinessChatLink() *mtproto.BusinessChatLink
		})
		if !ok {
			t.Fatalf("%s returned %T", method, o)
		}
		return l.To_BusinessChatLink()
	}
	list := func() *mtproto.Account_BusinessChatLinks {
		t.Helper()
		o, err := alice.c.Invoke(ctx, &mtproto.TLAccountGetBusinessChatLinks{
			Constructor: alice.c.Constructor(mtproto.Predicate_account_getBusinessChatLinks),
		})
		if err != nil {
			t.Fatalf("account.getBusinessChatLinks: %v", err)
		}
		return o.(interface {
			To_Account_BusinessChatLinks() *mtproto.Account_BusinessChatLinks
		}).To_Account_BusinessChatLinks()
	}
	resolve := func(u *user, slug string) (*mtproto.Account_ResolvedBusinessChatLinks, error) {
		o, err := u.c.Invoke(ctx, &mtproto.TLAccountResolveBusinessChatLink{
			Constructor: u.c.Constructor(mtproto.Predicate_account_resolveBusinessChatLink),
			Slug:        slug,
		})
		if err != nil {
			return nil, err
		}
		return o.(interface {
			To_Account_ResolvedBusinessChatLinks() *mtproto.Account_ResolvedBusinessChatLinks
		}).To_Account_ResolvedBusinessChatLinks(), nil
	}
	hasUser := func(users []*mtproto.User, id int64) bool {
		for _, u := range users {
			if u.GetId() == id {
				return true
			}
		}
		return false
	}

	if l := list(); len(l.GetLinks()) != 0 {
		t.Fatalf("a new account has links: %v", l.GetLinks())
	}

	// Created empty, as Android creates a new link, then written: the
	// whole link names it, as every client sends it.
	o, err := alice.c.Invoke(ctx, &mtproto.TLAccountCreateBusinessChatLink{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_createBusinessChatLink),
		Link:        input("", ""),
	})
	link := asLink("account.createBusinessChatLink", o, err)
	slug, ok := strings.CutPrefix(link.GetLink(), "https://pluma.chat/m/")
	if !ok || !business.ValidSlug(slug) || link.GetViews() != 0 || link.GetMessage() != "" {
		t.Fatalf("created %v", link)
	}
	mention := &mtproto.MessageEntity{
		Constructor:      alice.c.Constructor(mtproto.Predicate_inputMessageEntityMentionName),
		Offset:           0,
		Length:           3,
		UserId_INPUTUSER: bobForAlice,
	}
	bold := mtproto.MakeTLMessageEntityBold(&mtproto.MessageEntity{Offset: 5, Length: 5}).To_MessageEntity()
	o, err = alice.c.Invoke(ctx, &mtproto.TLAccountEditBusinessChatLink{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_editBusinessChatLink),
		Slug:        link.GetLink(),
		Link:        input("Bob, hello there", "Sales", mention, bold),
	})
	edited := asLink("account.editBusinessChatLink", o, err)
	if edited.GetLink() != link.GetLink() || edited.GetMessage() != "Bob, hello there" || edited.GetTitle().GetValue() != "Sales" || len(edited.GetEntities()) != 2 {
		t.Fatalf("edited %v", edited)
	}
	l := list()
	if len(l.GetLinks()) != 1 || l.GetLinks()[0].GetLink() != link.GetLink() || !hasUser(l.GetUsers(), bob.self.GetId()) {
		t.Fatalf("the list: %v", l)
	}
	if e := l.GetLinks()[0].GetEntities(); len(e) != 2 || e[0].GetPredicateName() != mtproto.Predicate_messageEntityMentionName || e[0].GetUserId_INT64() != bob.self.GetId() {
		t.Errorf("the entities in the list: %v", e)
	}

	// Bob opens it: the chat with Alice and the text; a view.
	r, err := resolve(bob, slug)
	if err != nil {
		t.Fatalf("account.resolveBusinessChatLink: %v", err)
	}
	if r.GetPeer().GetUserId() != alice.self.GetId() || r.GetMessage() != "Bob, hello there" || len(r.GetEntities()) != 2 ||
		!hasUser(r.GetUsers(), alice.self.GetId()) || !hasUser(r.GetUsers(), bob.self.GetId()) {
		t.Fatalf("resolved %v", r)
	}
	// Alice's own opening is not a view.
	if _, err = resolve(alice, slug); err != nil {
		t.Fatalf("account.resolveBusinessChatLink by the owner: %v", err)
	}
	if v := list().GetLinks()[0].GetViews(); v != 1 {
		t.Errorf("views %d, want 1", v)
	}

	// The errors of core.telegram.org/method/account.resolveBusinessChatLink.
	if _, err = resolve(bob, ""); !mtclient.IsRPCError(err, "CHATLINK_SLUG_EMPTY") {
		t.Errorf("an empty slug: %v, want CHATLINK_SLUG_EMPTY", err)
	}
	if _, err = resolve(bob, "Unknown123456789"); !mtclient.IsRPCError(err, "CHATLINK_SLUG_EXPIRED") {
		t.Errorf("an unknown slug: %v, want CHATLINK_SLUG_EXPIRED", err)
	}
	// Bob cannot delete Alice's link.
	if _, err = bob.c.Invoke(ctx, &mtproto.TLAccountDeleteBusinessChatLink{
		Constructor: bob.c.Constructor(mtproto.Predicate_account_deleteBusinessChatLink),
		Slug:        link.GetLink(),
	}); !mtclient.IsRPCError(err, "CHATLINK_SLUG_EXPIRED") {
		t.Errorf("deleting another account's link: %v, want CHATLINK_SLUG_EXPIRED", err)
	}

	// The page of the link.
	t.Run("page", func(t *testing.T) {
		if linksAddr == "" {
			t.Skip("PLUMA_E2E_LINKS is not set: the stand serves no pages")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+linksAddr+"/m/"+slug, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("the page: %v", err)
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `href="plm://message?slug=`+slug+`"`) ||
			!strings.Contains(string(body), "Alice") {
			t.Errorf("the page: %d, %d bytes, no hand-off to plm://message?slug= or no owner", res.StatusCode, len(body))
		}
	})

	// The limit: 100 links (business_chat_links_limit).
	for i := 1; i < business.ChatLinksLimit; i++ {
		o, err := alice.c.Invoke(ctx, &mtproto.TLAccountCreateBusinessChatLink{
			Constructor: alice.c.Constructor(mtproto.Predicate_account_createBusinessChatLink),
			Link:        input("hi", ""),
		})
		asLink("account.createBusinessChatLink", o, err)
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLAccountCreateBusinessChatLink{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_createBusinessChatLink),
		Link:        input("one too many", ""),
	}); !mtclient.IsRPCError(err, "CHATLINKS_TOO_MUCH") {
		t.Errorf("the 101st link: %v, want CHATLINKS_TOO_MUCH", err)
	}

	// Deleted: it opens no more.
	o, err = alice.c.Invoke(ctx, &mtproto.TLAccountDeleteBusinessChatLink{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_deleteBusinessChatLink),
		Slug:        link.GetLink(),
	})
	if err != nil {
		t.Fatalf("account.deleteBusinessChatLink: %v", err)
	}
	if b, _ := o.(interface{ To_Bool() *mtproto.Bool }); b == nil || !mtproto.FromBool(b.To_Bool()) {
		t.Fatalf("account.deleteBusinessChatLink: %v, want boolTrue", o)
	}
	if _, err = resolve(bob, slug); !mtclient.IsRPCError(err, "CHATLINK_SLUG_EXPIRED") {
		t.Errorf("a deleted link: %v, want CHATLINK_SLUG_EXPIRED", err)
	}
	if n := len(list().GetLinks()); n != business.ChatLinksLimit-1 {
		t.Errorf("%d links after the delete, want %d", n, business.ChatLinksLimit-1)
	}
}
