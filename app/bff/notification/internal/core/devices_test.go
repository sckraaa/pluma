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
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/notification/internal/svc"
	"github.com/teamgram/teamgram-server/pkg/pushdevices"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/status"
)

const (
	devUser    = 42
	devPermKey = 1001
)

func devCore(store pushdevices.Store, md *metadata.RpcMetadata) *NotificationCore {
	ctx := context.Background()
	return &NotificationCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{PushDevices: store},
		Logger: logx.WithContext(ctx),
		MD:     md,
	}
}

func signedIn() *metadata.RpcMetadata {
	return &metadata.RpcMetadata{UserId: devUser, PermAuthKeyId: devPermKey, LangCode: "ru"}
}

func registerReq(tokenType int32, token string, secret []byte) *mtproto.TLAccountRegisterDevice {
	return &mtproto.TLAccountRegisterDevice{
		TokenType:  tokenType,
		Token:      token,
		AppSandbox: mtproto.BoolFalse,
		Secret:     secret,
		OtherUids:  []int64{43},
	}
}

func wantRpcError(t *testing.T, what string, err error, want string) {
	t.Helper()
	if s, ok := status.FromError(err); !ok || s.Message() != want {
		t.Fatalf("%s: %v, want %s", what, err, want)
	}
}

func TestRegisterDevice(t *testing.T) {
	store := pushdevices.NewMemStore()
	c := devCore(store, signedIn())
	secret := bytes.Repeat([]byte{7}, pushdevices.SecretLength)

	req := registerReq(pushdevices.TokenFCM, "fcm-1", secret)
	req.NoMuted = true
	if r, err := c.AccountRegisterDevice(req); err != nil || !mtproto.FromBool(r) {
		t.Fatalf("registerDevice: %v %v", r, err)
	}
	ds, _ := store.List(context.Background(), devUser)
	if len(ds) != 1 {
		t.Fatalf("devices: %+v", ds)
	}
	d := ds[0]
	if d.AuthKeyId != devPermKey || d.TokenType != pushdevices.TokenFCM || d.Token != "fcm-1" || !d.NoMuted || d.AppSandbox ||
		!bytes.Equal(d.Secret, secret) || len(d.OtherUids) != 1 || d.LangCode != "ru" {
		t.Fatalf("device: %+v", d)
	}

	// A new token of the authorization replaces the old one.
	if _, err := c.AccountRegisterDevice(registerReq(pushdevices.TokenFCM, "fcm-2", secret)); err != nil {
		t.Fatal(err)
	}
	if ds, _ = store.List(context.Background(), devUser); len(ds) != 1 || ds[0].Token != "fcm-2" {
		t.Fatalf("after a new token: %+v", ds)
	}

	// Web push may come without a secret.
	if _, err := c.AccountRegisterDevice(registerReq(pushdevices.TokenWebPush, `{"endpoint":"https://push.example/1"}`, nil)); err != nil {
		t.Fatalf("web push without a secret: %v", err)
	}

	for name, tc := range map[string]struct {
		req  *mtproto.TLAccountRegisterDevice
		want string
	}{
		"type 4":           {registerReq(4, "x", secret), "TOKEN_TYPE_INVALID"},
		"type 13":          {registerReq(13, "x", secret), "TOKEN_TYPE_INVALID"},
		"empty token":      {registerReq(pushdevices.TokenAPNs, "", secret), "TOKEN_EMPTY"},
		"long token":       {registerReq(pushdevices.TokenAPNs, strings.Repeat("a", pushdevices.MaxTokenLength+1), secret), "TOKEN_INVALID"},
		"no secret":        {registerReq(pushdevices.TokenAPNs, "ab", nil), "TOKEN_INVALID"},
		"short secret":     {registerReq(pushdevices.TokenVoIP, "ab", secret[:255]), "TOKEN_INVALID"},
		"web short secret": {registerReq(pushdevices.TokenWebPush, "{}", secret[:32]), "TOKEN_INVALID"},
		"invalid utf-8":    {registerReq(pushdevices.TokenFCM, "a\xffb", secret), "TOKEN_INVALID"},
	} {
		_, err := c.AccountRegisterDevice(tc.req)
		wantRpcError(t, name, err, tc.want)
	}

	if _, err := devCore(store, &metadata.RpcMetadata{}).AccountRegisterDevice(registerReq(pushdevices.TokenFCM, "x", secret)); !errors.Is(err, mtproto.ErrAuthKeyUnregistered) {
		t.Fatalf("registerDevice without a user: %v", err)
	}
}

func TestRegisterInvalidatedToken(t *testing.T) {
	store := pushdevices.NewMemStore()
	c := devCore(store, signedIn())
	secret := bytes.Repeat([]byte{7}, pushdevices.SecretLength)
	if _, err := c.AccountRegisterDevice(registerReq(pushdevices.TokenAPNs, "ab12", secret)); err != nil {
		t.Fatal(err)
	}
	ds, _ := store.List(context.Background(), devUser)
	if err := store.Invalidate(context.Background(), ds[0].Id); err != nil {
		t.Fatal(err)
	}
	_, err := c.AccountRegisterDevice(registerReq(pushdevices.TokenAPNs, "ab12", secret))
	wantRpcError(t, "an invalidated token", err, "TOKEN_WAS_INVALIDATED")
	if _, err = c.AccountRegisterDevice(registerReq(pushdevices.TokenAPNs, "cd34", secret)); err != nil {
		t.Fatalf("a new token after TOKEN_WAS_INVALIDATED: %v", err)
	}

	store.SetError(errors.New("db down"))
	_, err = c.AccountRegisterDevice(registerReq(pushdevices.TokenAPNs, "cd34", secret))
	wantRpcError(t, "a store error", err, "INTERNAL_SERVER_ERROR")
}

func TestUnregisterAndLockDevice(t *testing.T) {
	ctx := context.Background()
	store := pushdevices.NewMemStore()
	c := devCore(store, signedIn())
	secret := bytes.Repeat([]byte{7}, pushdevices.SecretLength)
	for _, tt := range []int32{pushdevices.TokenAPNs, pushdevices.TokenVoIP} {
		if _, err := c.AccountRegisterDevice(registerReq(tt, "ab12", secret)); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := c.AccountUpdateDeviceLocked(&mtproto.TLAccountUpdateDeviceLocked{Period: 300}); err != nil {
		t.Fatal(err)
	}
	ds, _ := store.List(ctx, devUser)
	if len(ds) != 2 || ds[0].LockedPeriod != 300 || ds[1].LockedPeriod != 300 {
		t.Fatalf("after updateDeviceLocked: %+v", ds)
	}
	if _, err := c.AccountUpdateDeviceLocked(&mtproto.TLAccountUpdateDeviceLocked{Period: -1}); err != nil {
		t.Fatal(err)
	}
	if ds, _ = store.List(ctx, devUser); ds[0].LockedPeriod != 0 {
		t.Fatalf("no lock: period %d", ds[0].LockedPeriod)
	}

	if _, err := c.AccountUnregisterDevice(&mtproto.TLAccountUnregisterDevice{TokenType: pushdevices.TokenVoIP, Token: "ab12"}); err != nil {
		t.Fatal(err)
	}
	if ds, _ = store.List(ctx, devUser); len(ds) != 1 || ds[0].TokenType != pushdevices.TokenAPNs {
		t.Fatalf("after unregisterDevice: %+v", ds)
	}
	// Unregistering an unknown token is fine, as for Telegram.
	if r, err := c.AccountUnregisterDevice(&mtproto.TLAccountUnregisterDevice{TokenType: pushdevices.TokenFCM, Token: "zz"}); err != nil || !mtproto.FromBool(r) {
		t.Fatalf("unregisterDevice of an unknown token: %v %v", r, err)
	}
	_, err := c.AccountUnregisterDevice(&mtproto.TLAccountUnregisterDevice{TokenType: pushdevices.TokenFCM})
	wantRpcError(t, "unregisterDevice without a token", err, "TOKEN_EMPTY")

	if r, err := c.MessagesReportMessagesDelivery(&mtproto.TLMessagesReportMessagesDelivery{Push: true, Id: []int32{1, 2}}); err != nil || !mtproto.FromBool(r) {
		t.Fatalf("reportMessagesDelivery: %v %v", r, err)
	}
}
