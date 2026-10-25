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
	"crypto/rsa"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"github.com/teamgram/teamgram-server/pkg/serverkey"
)

// The scenarios run against our stand (test/e2e/run.sh test: PLUMA_E2E_*)
// and compare with the fixtures, or, with PLUMA_CONFORMANCE_RECORD=telegram
// and TELEGRAM_API_ID/TELEGRAM_API_HASH, against the Telegram test
// environment and write the fixtures (docs/tasks/007-conformance-harness.md).

// The Telegram test environment: DC 2 of Telegram Desktop's built-in test
// options (Telegram/SourceFiles/mtproto/mtproto_dc_options.cpp) and its
// key. A test DC is 10000 + its number in the key exchange.
const (
	telegramTestAddr = "149.154.167.40:443"
	telegramTestDC   = 10002
	telegramTestKey  = `-----BEGIN RSA PUBLIC KEY-----
MIIBCgKCAQEAyMEdY1aR+sCR3ZSJrtztKTKqigvO/vBfqACJLZtS7QMgCGXJ6XIR
yy7mx66W0/sOFa7/1mAZtEoIokDP3ShoqF4fVNb6XeqgQfaUHd8wJpDWHcR2OFwv
plUUI1PLTktZ9uW2WE23b+ixNwJjJGwBDJPQEQFBE+vfmH0JP503wr5INS1poWg/
j25sIWeYPHYeOrFp/eXaqhISP6G+q2IeTaWTXpwZj4LzXq5YOpk4bYEQ6mvRq7D1
aHWfYmlEGepfaYR8Q0YqvvhYtMte3ITnuSJs171+GDqpdKcSwHnd6FudwGO4pcCO
j4WcDuXc2CTHgH8gFTNhp/Y8/SpDOhvn9QIDAQAB
-----END RSA PUBLIC KEY-----`
)

// target is a server the scenarios run against.
type target struct {
	name    string
	record  bool // write the fixtures instead of comparing
	addr    string
	key     *rsa.PublicKey
	dc      int32
	apiID   int32
	apiHash string
	// phone returns a number that may get a login code: a test number of
	// Telegram, an allowlisted one of the stand.
	phone func(t *testing.T) string
}

func (tg *target) dial(ctx context.Context, t *testing.T) *mtclient.Client {
	t.Helper()
	c, err := mtclient.Dial(ctx, mtclient.Config{
		Addr: tg.addr, PublicKey: tg.key, DC: tg.dc, APIID: tg.apiID,
		DeviceModel: "Pluma conformance", AppVersion: "1.0", Logf: t.Logf,
	})
	if err != nil {
		t.Fatalf("%s: connect: %v", tg.name, err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if _, err = c.InitConnection(ctx); err != nil {
		t.Fatalf("%s: initConnection: %v", tg.name, err)
	}
	return c
}

func pickTarget(t *testing.T) *target {
	t.Helper()
	if os.Getenv("PLUMA_CONFORMANCE_RECORD") == "telegram" {
		apiID, err := strconv.Atoi(os.Getenv("TELEGRAM_API_ID"))
		if err != nil || os.Getenv("TELEGRAM_API_HASH") == "" {
			t.Fatal("recording needs TELEGRAM_API_ID and TELEGRAM_API_HASH (~/pluma-secrets/telegram-api.env)")
		}
		key, err := serverkey.ParsePublicKeyPEM([]byte(telegramTestKey))
		if err != nil {
			t.Fatal(err)
		}
		return &target{
			name: "telegram test DC 2", record: true, addr: telegramTestAddr, key: key, dc: telegramTestDC,
			apiID: int32(apiID), apiHash: os.Getenv("TELEGRAM_API_HASH"),
			phone: func(t *testing.T) string { return fmt.Sprintf("999662%04d", randomInt(t, 10000)) },
		}
	}
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
	return &target{
		name: "stand", addr: addr, key: key, dc: 2, apiID: 1, apiHash: "conformance",
		phone: func(t *testing.T) string {
			p := fmt.Sprintf("+1201556%04d", randomInt(t, 10000))
			if out, err := exec.Command(run, "admin", "allowlist", "add", p, "conformance").CombinedOutput(); err != nil {
				t.Fatalf("allowlist %s: %v\n%s", p, err, out)
			}
			return p
		},
	}
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
	name  string
	rules Rules
	run   func(ctx context.Context, t *testing.T, tg *target, r *Recorder)
}

// sendCode asks for a login code for phone.
func sendCode(ctx context.Context, t *testing.T, tg *target, r *Recorder, c *mtclient.Client, phone string) (*mtproto.TLAuthSentCode, error) {
	t.Helper()
	o, err := r.Call(ctx, c, "auth.sendCode", &mtproto.TLAuthSendCode{
		Constructor: c.Constructor(mtproto.Predicate_auth_sendCode),
		PhoneNumber: phone, ApiId: tg.apiID, ApiHash: tg.apiHash,
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
		run: func(ctx context.Context, t *testing.T, tg *target, r *Recorder) {
			c := tg.dial(ctx, t)
			_, _ = r.Call(ctx, c, "help.getConfig", &mtproto.TLHelpGetConfig{Constructor: c.Constructor(mtproto.Predicate_help_getConfig)})
		},
	},
	{
		name:  "help_getNearestDc",
		rules: Rules{Loose: []string{"country", "this_dc", "nearest_dc"}},
		run: func(ctx context.Context, t *testing.T, tg *target, r *Recorder) {
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
		run: func(ctx context.Context, t *testing.T, tg *target, r *Recorder) {
			c := tg.dial(ctx, t)
			if _, err := sendCode(ctx, t, tg, r, c, tg.phone(t)); err != nil {
				t.Fatalf("auth.sendCode: %v", err)
			}
		},
	},
	{
		name: "auth_sendCode_invalid_phone",
		run: func(ctx context.Context, t *testing.T, tg *target, r *Recorder) {
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
		run: func(ctx context.Context, t *testing.T, tg *target, r *Recorder) {
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
}

func TestConformance(t *testing.T) {
	tg := pickTarget(t)
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			r := NewRecorder(sc.name)
			sc.run(ctx, t, tg, r)
			got, err := r.Fixture(fmt.Sprintf("%s, layer %d, %s", tg.name, mtclient.DefaultLayer, time.Now().UTC().Format("2006-01-02")))
			if err != nil {
				t.Fatal(err)
			}
			if tg.record {
				if err = WriteFixture(got); err != nil {
					t.Fatal(err)
				}
				t.Logf("recorded %s", FixturePath(sc.name))
				return
			}
			want, err := ReadFixture(sc.name)
			if err != nil {
				t.Fatalf("no fixture (record it from Telegram, docs/tasks/007-conformance-harness.md): %v", err)
			}
			if diffs := Compare(want, got, sc.rules); len(diffs) > 0 {
				t.Errorf("%s differs from Telegram (%s):\n  %s", sc.name, want.Source, strings.Join(diffs, "\n  "))
			}
		})
	}
}
