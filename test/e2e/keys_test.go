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

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// Removal of auth keys (task 008): destroy_auth_key, auth.dropTempAuthKeys,
// and who may terminate the other sessions. A removed key stops working at
// once: session reports it unknown and the gateway answers -404 (and reads
// every key again within a minute anyway).

// keyGoneWithin is how long a removed key may keep working at most: the
// gateway's recheck interval and some slack.
const keyGoneWithin = 90 * time.Second

// waitKeyGone waits until requests over c fail: the server forgot its key.
func waitKeyGone(ctx context.Context, t *testing.T, name string, c *mtclient.Client) {
	t.Helper()
	deadline := time.Now().Add(keyGoneWithin)
	for time.Now().Before(deadline) {
		rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, err := c.Invoke(rctx, &mtproto.TLHelpGetConfig{Constructor: c.Constructor(mtproto.Predicate_help_getConfig)})
		cancel()
		if err != nil && (c.Err() != nil || mtclient.IsRPCError(err, "AUTH_KEY_UNREGISTERED") || mtclient.IsRPCError(err, "AUTH_KEY_INVALID")) {
			t.Logf("%s: the key is gone: %v", name, err)
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("%s: %v", name, ctx.Err())
		case <-time.After(5 * time.Second):
		}
	}
	t.Fatalf("%s: the removed key still works after %v", name, keyGoneWithin)
}

func TestDestroyAuthKey(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	u := login(ctx, t, testPhones(t, 1)[0], "Dora")

	o, err := u.c.Invoke(ctx, &mtproto.TLDestroyAuthKey{Constructor: mtproto.CRC32_destroy_auth_key})
	if err != nil {
		t.Fatalf("destroy_auth_key: %v", err)
	}
	if _, ok := o.(*mtproto.TLDestroyAuthKeyOk); !ok {
		t.Fatalf("destroy_auth_key answered %T, want destroy_auth_key_ok", o)
	}
	waitKeyGone(ctx, t, "destroyed key", u.c)
}

func TestDropTempAuthKeys(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	u := login(ctx, t, testPhones(t, 1)[0], "Emil")

	// A temporary key that asks for a month gets at most a week, and works.
	temp, err := mtclient.DialNewKey(ctx, config(t, "e2e temp"), mtclient.KeyOptions{Temp: true, ExpiresIn: 30 * 24 * 3600})
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

	o, err := u.c.Invoke(ctx, &mtproto.TLAuthDropTempAuthKeys{
		Constructor: u.c.Constructor(mtproto.Predicate_auth_dropTempAuthKeys),
	})
	if _, ok := o.(*mtproto.TLBoolTrue); err != nil || !ok {
		t.Fatalf("auth.dropTempAuthKeys: %T, %v; want boolTrue", o, err)
	}
	waitKeyGone(ctx, t, "dropped temporary key", temp)
	if _, err = u.c.Invoke(ctx, &mtproto.TLHelpGetConfig{Constructor: u.c.Constructor(mtproto.Predicate_help_getConfig)}); err != nil {
		t.Fatalf("the permanent key stopped working with its temporary keys: %v", err)
	}
}

// A session younger than a day may not terminate the others.
func TestResetAuthorizationsFromFreshSession(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	old := login(ctx, t, phone, "Fred")
	fresh := login(ctx, t, phone, "Fred")

	_, err := fresh.c.Invoke(ctx, &mtproto.TLAuthResetAuthorizations{
		Constructor: fresh.c.Constructor(mtproto.Predicate_auth_resetAuthorizations),
	})
	if !mtclient.IsRPCError(err, "FRESH_RESET_AUTHORISATION_FORBIDDEN") {
		t.Fatalf("auth.resetAuthorizations from a fresh session: %v, want FRESH_RESET_AUTHORISATION_FORBIDDEN", err)
	}
	if _, err = old.c.Invoke(ctx, &mtproto.TLHelpGetConfig{Constructor: old.c.Constructor(mtproto.Predicate_help_getConfig)}); err != nil {
		t.Fatalf("the other session was terminated: %v", err)
	}
}
