//go:build e2e

// Package e2e tests the whole server through its public MTProto port, the way
// the official clients use it: the stack of docker-compose-env.yaml and
// docker-compose.yaml, started by test/e2e/run.sh (docs/ops/testing.md).
package e2e

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"github.com/teamgram/teamgram-server/pkg/serverkey"
)

// Set by test/e2e/run.sh.
var (
	gatewayAddr = os.Getenv("PLUMA_E2E_ADDR")       // host:port of the MTProto TCP port
	serverKey   = os.Getenv("PLUMA_E2E_SERVER_KEY") // the server RSA key of this run
	runScript   = os.Getenv("PLUMA_E2E_RUN")        // test/e2e/run.sh, for pluma-admin
)

func TestMain(m *testing.M) {
	if gatewayAddr == "" || serverKey == "" || runScript == "" {
		fmt.Fprintln(os.Stderr, "e2e: PLUMA_E2E_ADDR, PLUMA_E2E_SERVER_KEY and PLUMA_E2E_RUN are not set; run the e2e tests with `make e2e` (test/e2e/run.sh)")
		os.Exit(2)
	}
	os.Exit(m.Run())
}

func publicKey(t *testing.T) *rsa.PublicKey {
	t.Helper()
	b, err := os.ReadFile(serverKey)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := serverkey.ParsePublicKeyPEM(b)
	if err != nil {
		t.Fatal(err)
	}
	return pub
}

func config(t *testing.T, device string) mtclient.Config {
	return mtclient.Config{
		Addr:        gatewayAddr,
		PublicKey:   publicKey(t),
		DeviceModel: device,
		Logf:        t.Logf,
	}
}

// closeChecked closes c at the end of the test and fails the test if the
// server sent any message twice (same msg_id): within one test every message
// is acknowledged in time, so the server has no reason to repeat one.
func closeChecked(t *testing.T, name string, c *mtclient.Client) {
	t.Helper()
	t.Cleanup(func() {
		if n := c.Duplicates(); n != 0 {
			t.Errorf("%s: the server sent %d messages again with a msg_id already received", name, n)
		}
		_ = c.Close()
	})
}

// admin runs tools/pluma-admin in the teamgram container.
// adminMu keeps one pluma-admin running at a time: each runs inside the bff
// container, and several at once go past its memory limit.
var adminMu sync.Mutex

func admin(t *testing.T, args ...string) string {
	t.Helper()
	adminMu.Lock()
	defer adminMu.Unlock()
	out, err := exec.Command(runScript, append([]string{"admin"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("pluma-admin %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

var loginCodeRe = regexp.MustCompile(`Login code for \+\d+: (\d+)`)

// testAreaCodes are the area codes of the pool: 201..220 but for 211, a
// service code that is no number, and 212, which test/conformance has.
// pkg/phonenumber takes 555-0100..0199 in each of them (checked for
// 201..999 when the pool grew past 201..208).
var testAreaCodes = []int64{201, 202, 203, 204, 205, 206, 207, 208, 209, 210, 213, 214, 215, 216, 217, 218, 219, 220}

// testPhonePool is how many phones testPhones has: 100 in each area code.
// A full run takes about 420 of them, and a stand kept between runs keeps
// them (phoneBook): the pool lasts four full runs on one stand.
var testPhonePool = int64(100 * len(testAreaCodes))

// poolPhone is the i-th phone of the pool: <area code>-555-0100..0199,
// numbers kept for fiction in the North American plan (NANPA keeps
// 555-0100..0199 so in every area code; its page was not read when the
// second area code was added). They never leave the stand: codes come from
// pluma-admin, not by SMS.
func poolPhone(i int64) string {
	return fmt.Sprintf("+1%d555%04d", testAreaCodes[i/100], 100+i%100)
}

// phoneBook hands out the phones of the pool. No number goes to two tests,
// so that its login limits (pkg/logincode: 8 codes an hour from one
// address) are one test's own, nor to a test while the stand has an account
// on it: a stand kept between runs (E2E_KEEP=1, `run.sh test` again) keeps
// the accounts of the earlier runs, and a test would sign in to one of them
// with its name and whatever else the earlier test left (another run's Bob
// signed in as this test's Alice and signed her posts "Bob E2E").
type phoneBook struct {
	mu sync.Mutex
	// taken: the numbers of this run's tests and, read from the stand once,
	// those with an account; nil until then.
	taken map[string]bool
}

// testPhoneBook is the run's.
var testPhoneBook = &phoneBook{}

// take returns n phones of the pool, each from next until it gives one that
// is not taken.
func (b *phoneBook) take(t *testing.T, n int, next func() (string, error)) []string {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.taken == nil {
		b.taken = map[string]bool{}
		for _, p := range standPoolPhones(t) {
			b.taken[p] = true
		}
	}
	if int64(len(b.taken)+n) > testPhonePool {
		t.Fatalf("%d more test phones needed, %d of %d are taken by this run or by accounts of earlier runs on the stand (test/e2e/run.sh down and up give a fresh one)", n, len(b.taken), testPhonePool)
	}
	var phones []string
	for len(phones) < n {
		p, err := next()
		if err != nil {
			t.Fatal(err)
		}
		if !b.taken[p] {
			b.taken[p] = true
			phones = append(phones, p)
		}
	}
	return phones
}

// standPoolPhones returns the phones of the pool that have an account on the
// stand, from the stack's MySQL (users.phone holds the digits without +).
// The password stays in the container's environment.
func standPoolPhones(t *testing.T) []string {
	t.Helper()
	out := stackOut(t, "exec", "-T", "mysql", "sh", "-c",
		`MYSQL_PWD="$MYSQL_PASSWORD" mysql -N -B -u "$MYSQL_USER" "$MYSQL_DATABASE" -e "SELECT phone FROM users WHERE phone LIKE '1___55501__'"`)
	pool := make(map[string]bool, testPhonePool)
	for i := range testPhonePool {
		pool[poolPhone(i)] = true
	}
	var phones []string
	for _, line := range strings.Fields(out) {
		if p := "+" + line; pool[p] {
			phones = append(phones, p)
		}
	}
	return phones
}

// testPhones returns n phones of the pool that no other test of the run gets
// and that have no account on the stand yet.
func testPhones(t *testing.T, n int) []string {
	t.Helper()
	return testPhoneBook.take(t, n, func() (string, error) {
		v, err := rand.Int(rand.Reader, big.NewInt(testPhonePool))
		if err != nil {
			return "", err
		}
		return poolPhone(v.Int64()), nil
	})
}

// TestPhonesOfEarlierRuns: a phone with an account on the stand, as a run
// on a kept stand leaves it, goes to no test of a later run.
func TestPhonesOfEarlierRuns(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	earlier := testPhones(t, 1)[0]
	login(ctx, t, earlier, "Earlier")

	// A later run has a book of its own; its first candidate is the number
	// of the earlier run, the others are the pool's in order.
	later := &phoneBook{}
	var i int64
	got := later.take(t, 1, func() (string, error) {
		if i++; i == 1 {
			return earlier, nil
		}
		return poolPhone((i - 2) % testPhonePool), nil
	})
	if got[0] == earlier {
		t.Errorf("a later run got %s, which has the account of an earlier run", earlier)
	}
}

func randomID() int64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return int64(binary.LittleEndian.Uint64(b[:]))
}

type user struct {
	c     *mtclient.Client
	phone string
	self  *mtproto.User
}

// login allows phone, connects a new device and logs in with the code the
// administrator issues: auth.sendCode, pluma-admin login-code, auth.signIn
// and, for a new phone, auth.signUp.
func login(ctx context.Context, t *testing.T, phone, firstName string) *user {
	t.Helper()
	return loginIn(ctx, t, phone, firstName, "")
}

// resetLoginsPerAddress forgets the codes asked from one address
// (logincode.SendCodePerIP): every login of the run comes from the test host,
// and the run has more of them than one person's network ever would.
func resetLoginsPerAddress(t *testing.T) {
	t.Helper()
	out, err := exec.Command(runScript, "compose", "exec", "-T", "redis", "sh", "-c",
		"redis-cli --scan --pattern 'login_flood_send_code_ip_*' | xargs -r redis-cli del").CombinedOutput()
	if err != nil {
		t.Fatalf("reset the login limit: %v\n%s", err, out)
	}
}

// loginIn is login from a device whose app is in langCode (initConnection),
// "" for the default.
func loginIn(ctx context.Context, t *testing.T, phone, firstName, langCode string) *user {
	t.Helper()
	cfg := config(t, "e2e "+firstName)
	if langCode != "" {
		cfg.LangCode = langCode
	}
	return loginWith(ctx, t, phone, firstName, cfg)
}

// loginAtLayer is login from a client of an older or newer API layer than the
// default.
func loginAtLayer(ctx context.Context, t *testing.T, phone, firstName string, layer int32) *user {
	t.Helper()
	cfg := config(t, "e2e "+firstName)
	cfg.Layer = layer
	return loginWith(ctx, t, phone, firstName, cfg)
}

func loginWith(ctx context.Context, t *testing.T, phone, firstName string, cfg mtclient.Config) *user {
	t.Helper()
	admin(t, "allowlist", "add", phone, "e2e test")

	c, err := mtclient.Dial(ctx, cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	closeChecked(t, firstName, c)

	sendCode := &mtproto.TLAuthSendCode{
		Constructor: c.Constructor(mtproto.Predicate_auth_sendCode),
		PhoneNumber: phone,
		ApiId:       1,
		ApiHash:     "e2e",
		Settings: &mtproto.CodeSettings{
			Constructor: c.Constructor(mtproto.Predicate_codeSettings),
		},
	}
	o, err := c.Invoke(ctx, sendCode)
	if e := (*mtclient.RPCError)(nil); errors.As(err, &e) && strings.HasPrefix(e.Message, "FLOOD_WAIT_") {
		resetLoginsPerAddress(t)
		o, err = c.Invoke(ctx, sendCode)
	}
	if err != nil {
		t.Fatalf("auth.sendCode: %v", err)
	}
	sent, ok := o.(*mtproto.TLAuthSentCode)
	if !ok {
		t.Fatalf("auth.sendCode returned %T, want auth.sentCode", o)
	}
	hash := sent.GetPhoneCodeHash()

	m := loginCodeRe.FindStringSubmatch(admin(t, "login-code", phone))
	if m == nil {
		t.Fatal("pluma-admin login-code printed no code")
	}
	code := m[1]

	o, err = c.Invoke(ctx, &mtproto.TLAuthSignIn{
		Constructor:          c.Constructor(mtproto.Predicate_auth_signIn),
		PhoneNumber:          phone,
		PhoneCodeHash:        hash,
		PhoneCode_FLAGSTRING: mtproto.MakeFlagsString(code),
	})
	if err != nil {
		t.Fatalf("auth.signIn: %v", err)
	}
	if _, required := o.(*mtproto.TLAuthAuthorizationSignUpRequired); required {
		o, err = c.Invoke(ctx, &mtproto.TLAuthSignUp{
			Constructor:   c.Constructor(mtproto.Predicate_auth_signUp),
			PhoneNumber:   phone,
			PhoneCodeHash: hash,
			FirstName:     firstName,
			LastName:      "E2E",
		})
		if err != nil {
			t.Fatalf("auth.signUp: %v", err)
		}
	}
	auth, ok := o.(*mtproto.TLAuthAuthorization)
	if !ok {
		t.Fatalf("login returned %T, want auth.authorization", o)
	}
	self := auth.GetUser()
	if self == nil || self.GetId() == 0 || !self.GetSelf() {
		t.Fatalf("auth.authorization without the own user: %v", self)
	}
	t.Logf("%s logged in as user %d", firstName, self.GetId())
	return &user{c: c, phone: phone, self: self}
}

// newMessages returns the messages of updateNewMessage (or
// updateShortMessage) in u.
func newMessages(u *mtproto.Updates) []*mtproto.Message {
	var out []*mtproto.Message
	add := func(up *mtproto.Update) {
		if up.GetPredicateName() == mtproto.Predicate_updateNewMessage && up.GetMessage_MESSAGE() != nil {
			out = append(out, up.GetMessage_MESSAGE())
		}
	}
	switch u.GetPredicateName() {
	case mtproto.Predicate_updateShortMessage:
		out = append(out, &mtproto.Message{
			Id:      u.GetId(),
			Message: u.GetMessage(),
			Out:     u.GetOut(),
			PeerId:  mtproto.MakePeerUser(u.GetUserId()),
		})
	case mtproto.Predicate_updateShort:
		if u.GetUpdate() != nil {
			add(u.GetUpdate())
		}
	case mtproto.Predicate_updates, mtproto.Predicate_updatesCombined:
		for _, up := range u.GetUpdates() {
			add(up)
		}
	}
	return out
}

// TestPrivateMessage is the full path: two users log in through the closed
// registration, one sends the other a message, which arrives pushed and is in
// updates.getDifference.
func TestPrivateMessage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")

	// Bob's update state before the message.
	o, err := bob.c.Invoke(ctx, &mtproto.TLUpdatesGetState{Constructor: bob.c.Constructor(mtproto.Predicate_updates_getState)})
	if err != nil {
		t.Fatalf("updates.getState: %v", err)
	}
	state, ok := o.(*mtproto.TLUpdatesState)
	if !ok {
		t.Fatalf("updates.getState returned %T", o)
	}

	// Alice finds Bob by his phone. (contacts.importContacts, which clients
	// use to add a contact, is an upstream enterprise stub that returns
	// nothing; contacts.resolvePhone is what a phone link resolves with.)
	o, err = alice.c.Invoke(ctx, &mtproto.TLContactsResolvePhone{
		Constructor: alice.c.Constructor(mtproto.Predicate_contacts_resolvePhone),
		Phone:       strings.TrimPrefix(bob.phone, "+"),
	})
	if err != nil {
		t.Fatalf("contacts.resolvePhone: %v", err)
	}
	resolved, ok := o.(interface {
		To_Contacts_ResolvedPeer() *mtproto.Contacts_ResolvedPeer
	})
	if !ok {
		t.Fatalf("contacts.resolvePhone returned %T", o)
	}
	var bobForAlice *mtproto.User
	for _, u := range resolved.To_Contacts_ResolvedPeer().GetUsers() {
		if u.GetId() == bob.self.GetId() {
			bobForAlice = u
		}
	}
	if bobForAlice == nil || bobForAlice.GetAccessHash() == nil {
		t.Fatalf("contacts.resolvePhone did not return Bob (%d) with an access hash", bob.self.GetId())
	}

	text := fmt.Sprintf("e2e message %d", randomID())
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer: &mtproto.InputPeer{
			Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerUser),
			UserId:      bobForAlice.GetId(),
			AccessHash:  bobForAlice.GetAccessHash().GetValue(),
		},
		Message:  text,
		RandomId: randomID(),
	})
	if err != nil {
		t.Fatalf("messages.sendMessage: %v", err)
	}
	if _, ok = o.(interface{ To_Updates() *mtproto.Updates }); !ok {
		t.Fatalf("messages.sendMessage returned %T, want Updates", o)
	}

	// Bob gets the message pushed, without asking.
	pushed, err := bob.c.WaitUpdate(ctx, func(u *mtproto.Updates) bool {
		for _, m := range newMessages(u) {
			if m.GetMessage() == text {
				return true
			}
		}
		return false
	})
	if err != nil {
		t.Fatalf("Bob did not receive the message: %v", err)
	}
	var got *mtproto.Message
	for _, m := range newMessages(pushed) {
		if m.GetMessage() == text {
			got = m
		}
	}
	// An incoming private message: peer_id is the sender, out is false
	// (from_id is not set in private chats).
	if got.GetOut() || got.GetPeerId().GetUserId() != alice.self.GetId() {
		t.Errorf("pushed message: out %v, peer user %d; want an incoming message from Alice (%d)",
			got.GetOut(), got.GetPeerId().GetUserId(), alice.self.GetId())
	}
	if got.GetId() <= 0 {
		t.Errorf("pushed message id %d", got.GetId())
	}
	t.Logf("Bob received message %d pushed (%s)", got.GetId(), pushed.GetPredicateName())

	// And the same message through updates.getDifference from the old state.
	o, err = bob.c.Invoke(ctx, &mtproto.TLUpdatesGetDifference{
		Constructor: bob.c.Constructor(mtproto.Predicate_updates_getDifference),
		Pts:         state.GetPts(),
		Date:        state.GetDate(),
		Qts:         state.GetQts(),
	})
	if err != nil {
		t.Fatalf("updates.getDifference: %v", err)
	}
	diff, ok := o.(interface {
		To_Updates_Difference() *mtproto.Updates_Difference
	})
	if !ok {
		t.Fatalf("updates.getDifference returned %T", o)
	}
	d := diff.To_Updates_Difference()
	var inDiff *mtproto.Message
	for _, m := range d.GetNewMessages() {
		if m.GetMessage() == text {
			inDiff = m
		}
	}
	if inDiff == nil {
		t.Fatalf("updates.getDifference (%s) has %d new messages, none is the sent one", d.GetPredicateName(), len(d.GetNewMessages()))
	}
	if inDiff.GetId() != got.GetId() {
		t.Errorf("message id in getDifference %d, pushed %d", inDiff.GetId(), got.GetId())
	}
	if inDiff.GetOut() || inDiff.GetPeerId().GetUserId() != alice.self.GetId() {
		t.Errorf("message in getDifference: out %v, peer user %d; want an incoming message from Alice (%d)",
			inDiff.GetOut(), inDiff.GetPeerId().GetUserId(), alice.self.GetId())
	}

	t.Run("temporary key bound to the permanent key", func(t *testing.T) {
		testTempKey(ctx, t, alice)
	})
}

// testTempKey creates a temporary key (PFS), binds it to the user's permanent
// key with auth.bindTempAuthKey and checks that requests over it act for the
// user.
func testTempKey(ctx context.Context, t *testing.T, u *user) {
	temp, err := mtclient.DialNewKey(ctx, config(t, "e2e pfs"), mtclient.KeyOptions{Temp: true, ExpiresIn: 3600})
	if err != nil {
		t.Fatalf("temporary key: %v", err)
	}
	closeChecked(t, "temporary key", temp)

	if err = temp.BindTempAuthKey(ctx, u.c.AuthKey(), mtclient.TempKeyExpiresAt(temp.AuthKey(), time.Hour)); err != nil {
		t.Fatalf("auth.bindTempAuthKey: %v", err)
	}
	if _, err = temp.InitConnection(ctx); err != nil {
		t.Fatalf("initConnection over the bound key: %v", err)
	}

	raw, err := temp.InvokeRaw(ctx, &mtproto.TLUsersGetUsers{
		Constructor: temp.Constructor(mtproto.Predicate_users_getUsers),
		Id:          []*mtproto.InputUser{{Constructor: temp.Constructor(mtproto.Predicate_inputUserSelf)}},
	})
	if err != nil {
		t.Fatalf("users.getUsers over the bound key: %v", err)
	}
	users, err := mtclient.DecodeVector(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 {
		t.Fatalf("users.getUsers returned %d users", len(users))
	}
	self, ok := users[0].(interface{ To_User() *mtproto.User })
	if !ok || self.To_User().GetId() != u.self.GetId() {
		t.Fatalf("over the bound temporary key the user is %v, want %d", users[0], u.self.GetId())
	}

	// A second permanent key cannot take over the bound temporary key.
	other, err := mtclient.DialNewKey(ctx, config(t, "e2e other"), mtclient.KeyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	closeChecked(t, "second permanent key", other)
	err = temp.BindTempAuthKey(ctx, other.AuthKey(), mtclient.TempKeyExpiresAt(temp.AuthKey(), time.Hour))
	if !mtclient.IsRPCError(err, "TEMP_AUTH_KEY_ALREADY_BOUND") {
		t.Fatalf("binding the bound temporary key to another permanent key: %v, want TEMP_AUTH_KEY_ALREADY_BOUND", err)
	}
}
