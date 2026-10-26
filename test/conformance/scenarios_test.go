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

//go:build e2e

package conformance

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"github.com/teamgram/teamgram-server/pkg/serverkey"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The scenarios run against our stand (test/e2e/run.sh test: PLUMA_E2E_*)
// and compare with the fixtures. With PLUMA_CONFORMANCE_RECORD=telegram and
// TELEGRAM_API_ID/TELEGRAM_API_HASH they run against Telegram and write the
// fixtures: the scenarios without an account in the test environment, the
// ones with an account in the environment PLUMA_CONFORMANCE_ACCOUNT names
// (test or production), with the session the owner saved there by
// cmd/tglogin (PLUMA_CONFORMANCE_SESSION, default main). See
// docs/tasks/007-conformance-harness.md.

// target is a server the scenarios run against.
type target struct {
	name    string // where the scenarios without an account run
	record  bool   // write the fixtures instead of comparing
	cfg     mtclient.Config
	apiHash string
	// phone returns a number that may get a login code: a test number of
	// Telegram, an allowlisted one of the stand.
	phone func(t *testing.T) string
	// login signs in the account of the scenarios that need one.
	login func(ctx context.Context, t *testing.T) *account
}

// account is a signed-in authorization. Each connection is a new MTProto
// session of it. A scenario keeps one connection at a time: Telegram
// invalidates an authorization that sends requests over two connections at
// once (AUTH_KEY_DUPLICATED, https://core.telegram.org/api/errors).
type account struct {
	source string // where it runs, for the fixture
	self   int64  // the user id of the account
	cfg    mtclient.Config
	key    *mtclient.AuthKey
}

// open opens a connection of a; the caller closes it.
func (a *account) open(ctx context.Context, t *testing.T) (*mtclient.Client, error) {
	cfg := a.cfg
	cfg.Logf = t.Logf
	return open(ctx, cfg, a.key)
}

// connect opens a connection of a that is closed when t ends.
func (a *account) connect(ctx context.Context, t *testing.T) *mtclient.Client {
	t.Helper()
	c, err := a.open(ctx, t)
	if err != nil {
		t.Fatalf("%s: connect: %v", a.source, err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// dial opens a connection with a new key, closed when t ends.
func (tg *target) dial(ctx context.Context, t *testing.T) *mtclient.Client {
	t.Helper()
	return (&account{source: tg.name, cfg: tg.cfg}).connect(ctx, t)
}

// open connects with cfg and key, or a new key if key is nil, and sends
// initConnection once, as the official clients do.
func open(ctx context.Context, cfg mtclient.Config, key *mtclient.AuthKey) (*mtclient.Client, error) {
	if key == nil {
		return mtclient.Dial(ctx, cfg)
	}
	c, err := mtclient.DialKey(ctx, cfg, key)
	if err != nil {
		return nil, err
	}
	if _, err = c.InitConnection(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

func pickTarget(t *testing.T) *target {
	t.Helper()
	if os.Getenv("PLUMA_CONFORMANCE_RECORD") == "telegram" {
		return telegramTarget(t)
	}
	return standTarget(t)
}

func telegramTarget(t *testing.T) *target {
	t.Helper()
	test, err := TelegramFromEnv(true)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := test.Config(2, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return &target{
		name: "telegram test DC 2", record: true, cfg: cfg, apiHash: test.APIHash,
		phone: func(t *testing.T) string { return fmt.Sprintf("999662%04d", randomInt(t, 10000)) },
		login: func(ctx context.Context, t *testing.T) *account {
			t.Helper()
			env := os.Getenv("PLUMA_CONFORMANCE_ACCOUNT")
			if env != "test" && env != "production" {
				t.Skip("a scenario with an account records with a saved session: set PLUMA_CONFORMANCE_ACCOUNT=test or production (docs/ops/testing.md)")
			}
			name := os.Getenv("PLUMA_CONFORMANCE_SESSION")
			if name == "" {
				name = "main"
			}
			s, err := LoadSession(env == "test", name)
			if err != nil {
				flag := ""
				if env == "test" {
					flag = " -test"
				}
				t.Fatalf("no saved session of the Telegram %s environment, sign in with go run ./test/conformance/cmd/tglogin -qr%s: %v", env, flag, err)
			}
			// A session saved before the environments had their own
			// directories may be of the other one.
			if s.Test != (env == "test") {
				t.Fatalf("the session %q is not of the %s environment: sign in again with tglogin", name, env)
			}
			tg, err := TelegramFromEnv(s.Test)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := tg.Config(s.DC, s.Addr, nil)
			if err != nil {
				t.Fatal(err)
			}
			return &account{source: fmt.Sprintf("telegram %s DC %d", env, s.DC), self: s.UserID, cfg: cfg, key: s.AuthKey}
		},
	}
}

func standTarget(t *testing.T) *target {
	t.Helper()
	addr, keyFile, run := os.Getenv("PLUMA_E2E_ADDR"), os.Getenv("PLUMA_E2E_SERVER_KEY"), os.Getenv("PLUMA_E2E_RUN")
	if addr == "" || keyFile == "" || run == "" {
		t.Skip("PLUMA_E2E_ADDR, PLUMA_E2E_SERVER_KEY and PLUMA_E2E_RUN are not set: run with test/e2e/run.sh test")
	}
	b, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	key, err := serverkey.ParsePublicKeyPEM(b)
	if err != nil {
		t.Fatal(err)
	}
	tg := &target{
		name:    "stand",
		cfg:     mtclient.Config{Addr: addr, PublicKey: key, DC: 2, APIID: 1, DeviceModel: "Pluma conformance", AppVersion: "1.0"},
		apiHash: "conformance",
		// Numbers reserved for fiction in the North American plan, as in
		// test/e2e, in another area code than its numbers: the packages run
		// at once, and each number has its own login limits.
		phone: func(t *testing.T) string {
			p := fmt.Sprintf("+1212555%04d", 100+randomInt(t, 100))
			if out, err := exec.Command(run, "admin", "allowlist", "add", p, "conformance").CombinedOutput(); err != nil {
				t.Fatalf("allowlist %s: %v\n%s", p, err, out)
			}
			return p
		},
	}
	tg.login = func(ctx context.Context, t *testing.T) *account { return standLogin(ctx, t, tg, run) }
	return tg
}

var loginCodeRe = regexp.MustCompile(`Login code for \+\d+: (\d+)`)

// standLogin signs up a new user of the stand with the code pluma-admin
// issues. The login is not a step of the scenario.
func standLogin(ctx context.Context, t *testing.T, tg *target, run string) *account {
	t.Helper()
	phone := tg.phone(t)
	c := tg.dial(ctx, t)
	o, err := c.Invoke(ctx, &mtproto.TLAuthSendCode{
		Constructor: c.Constructor(mtproto.Predicate_auth_sendCode),
		PhoneNumber: phone, ApiId: tg.cfg.APIID, ApiHash: tg.apiHash,
		Settings: &mtproto.CodeSettings{Constructor: c.Constructor(mtproto.Predicate_codeSettings)},
	})
	if err != nil {
		t.Fatalf("stand: auth.sendCode: %v", err)
	}
	hash := o.(*mtproto.TLAuthSentCode).GetPhoneCodeHash()
	out, err := exec.Command(run, "admin", "login-code", phone).CombinedOutput()
	m := loginCodeRe.FindSubmatch(out)
	if err != nil || m == nil {
		t.Fatalf("stand: pluma-admin login-code: %v", err)
	}
	o, err = c.Invoke(ctx, &mtproto.TLAuthSignIn{
		Constructor: c.Constructor(mtproto.Predicate_auth_signIn), PhoneNumber: phone, PhoneCodeHash: hash,
		PhoneCode_FLAGSTRING: mtproto.MakeFlagsString(string(m[1])),
	})
	if _, required := o.(*mtproto.TLAuthAuthorizationSignUpRequired); err == nil && required {
		o, err = c.Invoke(ctx, &mtproto.TLAuthSignUp{
			Constructor: c.Constructor(mtproto.Predicate_auth_signUp), PhoneNumber: phone, PhoneCodeHash: hash, FirstName: "Conformance",
		})
	}
	auth, ok := o.(*mtproto.TLAuthAuthorization)
	if err != nil || !ok {
		t.Fatalf("stand: sign in: %T %v", o, err)
	}
	// One connection at a time, as with Telegram.
	_ = c.Close()
	return &account{source: tg.name, self: auth.GetUser().GetId(), cfg: tg.cfg, key: c.AuthKey()}
}

func randomInt(t *testing.T, n int64) int64 {
	v, err := rand.Int(rand.Reader, big.NewInt(n))
	if err != nil {
		t.Fatal(err)
	}
	return v.Int64()
}

// scenario is one sequence of calls and how it compares.
type scenario struct {
	name    string
	account bool // runs signed in; the login is not a step
	// pending says why there is no fixture yet: until it is recorded, the
	// scenario runs on the stand but is not compared.
	pending string
	rules   Rules
	run     func(ctx context.Context, t *testing.T, tg *target, a *account, r *Recorder)
}

// textPrefix starts every text a scenario sends: a recording from a real
// account must hold no other text (Foreign).
const textPrefix = "conformance: "

// sentMessageID returns the id of the message a messages.sendMessage answer
// is about.
func sentMessageID(t *testing.T, o mtproto.TLObject) int32 {
	t.Helper()
	u, ok := o.(interface{ To_Updates() *mtproto.Updates })
	if !ok {
		t.Fatalf("messages.sendMessage returned %T", o)
	}
	if u.To_Updates().GetPredicateName() == mtproto.Predicate_updateShortSentMessage {
		return u.To_Updates().GetId()
	}
	for _, up := range u.To_Updates().GetUpdates() {
		switch up.GetPredicateName() {
		case mtproto.Predicate_updateMessageID:
			return up.GetId_INT32()
		case mtproto.Predicate_updateNewMessage:
			return up.GetMessage_MESSAGE().GetId()
		}
	}
	t.Fatalf("messages.sendMessage: no message id in %s", u.To_Updates().GetPredicateName())
	return 0
}

// deleteMessages deletes message id for everyone.
func deleteMessages(c *mtclient.Client, id int32) *mtproto.TLMessagesDeleteMessages {
	return &mtproto.TLMessagesDeleteMessages{
		Constructor: c.Constructor(mtproto.Predicate_messages_deleteMessages), Revoke: true, Id: []int32{id},
	}
}

// sendCode asks for a login code for phone.
func sendCode(ctx context.Context, t *testing.T, tg *target, r *Recorder, c *mtclient.Client, phone string) (*mtproto.TLAuthSentCode, error) {
	t.Helper()
	o, err := r.Call(ctx, c, "auth.sendCode", &mtproto.TLAuthSendCode{
		Constructor: c.Constructor(mtproto.Predicate_auth_sendCode),
		PhoneNumber: phone, ApiId: tg.cfg.APIID, ApiHash: tg.apiHash,
		Settings: &mtproto.CodeSettings{Constructor: c.Constructor(mtproto.Predicate_codeSettings)},
	})
	if err != nil {
		return nil, err
	}
	sent, ok := o.(*mtproto.TLAuthSentCode)
	if !ok {
		t.Fatalf("auth.sendCode returned %T", o)
	}
	return sent, nil
}

var scenarios = []scenario{
	{
		// The config of the server: which fields there are. Their values
		// (data centers, limits) are the server's own.
		name:  "help_getConfig",
		rules: Rules{Loose: []string{"*"}},
		run: func(ctx context.Context, t *testing.T, tg *target, _ *account, r *Recorder) {
			c := tg.dial(ctx, t)
			_, _ = r.Call(ctx, c, "help.getConfig", &mtproto.TLHelpGetConfig{Constructor: c.Constructor(mtproto.Predicate_help_getConfig)})
		},
	},
	{
		name:  "help_getNearestDc",
		rules: Rules{Loose: []string{"country", "this_dc", "nearest_dc"}},
		run: func(ctx context.Context, t *testing.T, tg *target, _ *account, r *Recorder) {
			c := tg.dial(ctx, t)
			_, _ = r.Call(ctx, c, "help.getNearestDc", &mtproto.TLHelpGetNearestDc{Constructor: c.Constructor(mtproto.Predicate_help_getNearestDc)})
		},
	},
	{
		// A login code for a number that may sign in: the code type and its
		// length, the hash of the request.
		name: "auth_sendCode",
		rules: Rules{
			Loose: []string{"type.length"},
			Deviations: []Deviation{
				{Step: 0, Path: "next_type", Reason: "task 001: codes come in the app or from pluma-admin, no SMS or call to offer"},
				{Step: 0, Path: "timeout", Reason: "task 001: no other way to send the code, nothing to wait for"},
			},
		},
		run: func(ctx context.Context, t *testing.T, tg *target, _ *account, r *Recorder) {
			c := tg.dial(ctx, t)
			if _, err := sendCode(ctx, t, tg, r, c, tg.phone(t)); err != nil {
				t.Fatalf("auth.sendCode: %v", err)
			}
		},
	},
	{
		name: "auth_sendCode_invalid_phone",
		run: func(ctx context.Context, t *testing.T, tg *target, _ *account, r *Recorder) {
			c := tg.dial(ctx, t)
			if _, err := sendCode(ctx, t, tg, r, c, "123"); err == nil {
				t.Fatal("auth.sendCode of 123 succeeded")
			}
		},
	},
	{
		// A wrong code, then none, then a code without the hash of the
		// request.
		name: "auth_signIn_wrong_code",
		rules: Rules{
			Loose: []string{"type.length"},
			Deviations: []Deviation{
				{Step: 0, Path: "next_type", Reason: "task 001: codes come in the app or from pluma-admin, no SMS or call to offer"},
				{Step: 0, Path: "timeout", Reason: "task 001: no other way to send the code, nothing to wait for"},
			},
		},
		run: func(ctx context.Context, t *testing.T, tg *target, _ *account, r *Recorder) {
			c := tg.dial(ctx, t)
			phone := tg.phone(t)
			sent, err := sendCode(ctx, t, tg, r, c, phone)
			if err != nil {
				t.Fatalf("auth.sendCode: %v", err)
			}
			wrong := strings.Repeat("0", int(sent.GetType().GetLength()))
			_, _ = r.Call(ctx, c, "auth.signIn", &mtproto.TLAuthSignIn{
				Constructor: c.Constructor(mtproto.Predicate_auth_signIn), PhoneNumber: phone, PhoneCodeHash: sent.GetPhoneCodeHash(),
				PhoneCode_FLAGSTRING: mtproto.MakeFlagsString(wrong),
			})
			_, _ = r.Call(ctx, c, "auth.signIn", &mtproto.TLAuthSignIn{
				Constructor: c.Constructor(mtproto.Predicate_auth_signIn), PhoneNumber: phone, PhoneCodeHash: sent.GetPhoneCodeHash(),
			})
			_, _ = r.Call(ctx, c, "auth.signIn", &mtproto.TLAuthSignIn{
				Constructor: c.Constructor(mtproto.Predicate_auth_signIn), PhoneNumber: phone,
				PhoneCode_FLAGSTRING: mtproto.MakeFlagsString(wrong),
			})
		},
	},
	{
		// Saved Messages: a message sent, edited and deleted, and what
		// another session of the account gets from updates.getDifference
		// after the edit and after the deletion.
		name:    "saved_messages",
		account: true,
		// What the account holds besides the scenario (its unread messages
		// and secret chats, its username) and its presence, which its other
		// sessions decide.
		rules: Rules{
			Loose: []string{"unread_count", "state.unread_count", "qts", "state.qts",
				"users.[].username", "users.[].status"},
		},
		run: func(ctx context.Context, t *testing.T, _ *target, a *account, r *Recorder) {
			// A failed run does not leave the message in the account. The
			// cleanup is registered first, so it runs after the connections
			// of the scenario are closed.
			var undeleted int32
			t.Cleanup(func() {
				if undeleted == 0 {
					return
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				c, err := a.open(ctx, t)
				if err == nil {
					_, err = c.Invoke(ctx, deleteMessages(c, undeleted))
					_ = c.Close()
				}
				if err != nil {
					t.Logf("the message of the scenario is not deleted: %v", err)
				}
			})

			c := a.connect(ctx, t)
			o, err := r.Call(ctx, c, "updates.getState", &mtproto.TLUpdatesGetState{Constructor: c.Constructor(mtproto.Predicate_updates_getState)})
			if err != nil {
				t.Fatalf("updates.getState: %v", err)
			}
			state, ok := o.(*mtproto.TLUpdatesState)
			if !ok {
				t.Fatalf("updates.getState returned %T", o)
			}
			self := &mtproto.InputPeer{Constructor: c.Constructor(mtproto.Predicate_inputPeerSelf)}
			o, err = r.Call(ctx, c, "messages.sendMessage", &mtproto.TLMessagesSendMessage{
				Constructor: c.Constructor(mtproto.Predicate_messages_sendMessage),
				Peer:        self, Message: textPrefix + "saved message", RandomId: randomInt(t, math.MaxInt64),
			})
			if err != nil {
				t.Fatalf("messages.sendMessage: %v", err)
			}
			id := sentMessageID(t, o)
			undeleted = id
			if _, err = r.Call(ctx, c, "messages.editMessage", &mtproto.TLMessagesEditMessage{
				Constructor: c.Constructor(mtproto.Predicate_messages_editMessage),
				Peer:        self, Id: id, Message: wrapperspb.String(textPrefix + "edited message"),
			}); err != nil {
				t.Fatalf("messages.editMessage: %v", err)
			}
			_ = c.Close()

			// Another session of the account asks what it missed since the
			// state: a new connection, as an app that was offline.
			difference := func() {
				t.Helper()
				other := a.connect(ctx, t)
				defer func() { _ = other.Close() }()
				if _, err := r.Call(ctx, other, "updates.getDifference", &mtproto.TLUpdatesGetDifference{
					Constructor: other.Constructor(mtproto.Predicate_updates_getDifference),
					Pts:         state.GetPts(), Date: state.GetDate(), Qts: state.GetQts(),
				}); err != nil {
					t.Fatalf("updates.getDifference: %v", err)
				}
			}
			difference()
			c = a.connect(ctx, t)
			if _, err = r.Call(ctx, c, "messages.deleteMessages", deleteMessages(c, id)); err != nil {
				t.Fatalf("messages.deleteMessages: %v", err)
			}
			undeleted = 0
			_ = c.Close()
			difference()
		},
	},
}

func TestConformance(t *testing.T) {
	tg := pickTarget(t)
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			var a *account
			source := tg.name
			if sc.account {
				a = tg.login(ctx, t)
				source = a.source
			}
			r := NewRecorder(sc.name)
			if a != nil {
				if a.self == 0 {
					t.Fatal("the account has no user id: sign in again")
				}
				r.Scope(a.self)
			}
			sc.run(ctx, t, tg, a, r)
			// What a real account did meanwhile is not the scenario's; on the
			// stand the account has nothing else, so anything dropped is an
			// answer our server should not have given.
			if dropped := r.Dropped(); len(dropped) > 0 {
				if !tg.record {
					t.Errorf("answers hold what is not the account's own:\n  %s", strings.Join(dropped, "\n  "))
				} else {
					t.Logf("not the account's own, left out: %s", strings.Join(dropped, ", "))
				}
			}
			got, err := r.Fixture(fmt.Sprintf("%s, layer %d, %s", source, mtclient.DefaultLayer, time.Now().UTC().Format("2006-01-02")))
			if err != nil {
				t.Fatal(err)
			}
			if tg.record {
				if f := Foreign(got, textPrefix); sc.account && len(f) > 0 {
					t.Fatalf("not recorded: the answers hold what is not the account's own, record again:\n  %s", strings.Join(f, "\n  "))
				}
				if err = WriteFixture(FixturePath(sc.name), got); err != nil {
					t.Fatal(err)
				}
				t.Logf("recorded %s", FixturePath(sc.name))
				return
			}
			if out := os.Getenv("PLUMA_CONFORMANCE_OUT"); out != "" {
				if err = WriteFixture(filepath.Join(out, sc.name+".json"), got); err != nil {
					t.Fatal(err)
				}
			}
			want, err := ReadFixture(sc.name)
			switch {
			case errors.Is(err, fs.ErrNotExist) && sc.pending != "":
				t.Skipf("not compared, no fixture yet: %s", sc.pending)
			case err != nil:
				t.Fatalf("no fixture (record it from Telegram, docs/tasks/007-conformance-harness.md): %v", err)
			case sc.pending != "":
				t.Errorf("the fixture is recorded: remove pending of %s", sc.name)
			}
			if diffs := Compare(want, got, sc.rules); len(diffs) > 0 {
				t.Errorf("%s differs from Telegram (%s):\n  %s", sc.name, want.Source, strings.Join(diffs, "\n  "))
			}
		})
	}
}
