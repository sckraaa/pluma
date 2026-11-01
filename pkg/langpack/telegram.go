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

package langpack

import (
	"context"
	"crypto/rsa"
	"errors"
	"math/big"
	"runtime"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// TelegramAddr is DC 2 of Telegram's production network, the first address
// the Android client knows for it (DrKLO/Telegram ConnectionsManager.cpp,
// the built-in datacenters: "149.154.167.51", 443).
const TelegramAddr = "149.154.167.51:443"

// TelegramDC is the number of the datacenter at TelegramAddr.
const TelegramDC = 2

// telegramModulus is the modulus of Telegram's production RSA key (exponent
// 65537), as Web K has it (tweb src/lib/mtproto/rsaKeysManager.ts at
// 346844bf, "obtained from here: https://core.telegram.org/api/obtaining_api_id").
const telegramModulus = "e8bb3305c0b52c6cf2afdf7637313489e63e05268e5badb601af417786472e5f93b85438968e20e6729a301c0afc121bf7151f834436f7fda680847a66bf64accec78ee21c0b316f0edafe2f41908da7bd1f4a5107638eeb67040ace472a14f90d9f7c2b7def99688ba3073adb5750bb02964902a359fe745d8170e36876d4fd8a5d41b2a76cbff9a13267eb9580b2d06d10357448d20d9da2191cb5d8c93982961cdfdeda629e37f1fb09a0722027696032fe61ed663db7a37f6f263d370f69db53a0dc0a1748bdaaff6209d5645485e6e001d1953255757e4b8e42813347b11da6ab500fd0ace7e6dfa3736199ccaf9397ed0745a427dcfa6cd67bcb1acff3"

// TelegramPublicKey returns Telegram's production RSA public key.
func TelegramPublicKey() *rsa.PublicKey {
	n, ok := new(big.Int).SetString(telegramModulus, 16)
	if !ok {
		panic("langpack: bad Telegram modulus")
	}
	return &rsa.PublicKey{N: n, E: 65537}
}

// TelegramConfig is how DialTelegram connects.
type TelegramConfig struct {
	Addr  string // default TelegramAddr
	DC    int32  // default TelegramDC
	APIID int32  // the api_id of the application (my.telegram.org); required
	Layer int32  // default mtclient.DefaultLayer
	// AppVersion is sent in initConnection.
	AppVersion string
}

// TelegramClient is a connection to Telegram without login that sends every
// request inside invokeWithLayer(initConnection(...)).
type TelegramClient struct {
	c *mtclient.Client
}

// DialTelegram connects to Telegram, creates a throwaway auth key (the key
// exchange checks that the server holds Telegram's key) and returns the
// client. No phone, code or api_hash is involved.
func DialTelegram(ctx context.Context, cfg TelegramConfig) (*TelegramClient, error) {
	if cfg.APIID <= 0 {
		return nil, errors.New("langpack: an api_id is required to talk to Telegram")
	}
	if cfg.Addr == "" {
		cfg.Addr = TelegramAddr
	}
	if cfg.DC == 0 {
		cfg.DC = TelegramDC
	}
	if cfg.AppVersion == "" {
		cfg.AppVersion = "1.0"
	}
	c, err := mtclient.DialNewKey(ctx, mtclient.Config{
		Addr:           cfg.Addr,
		PublicKey:      TelegramPublicKey(),
		Layer:          cfg.Layer,
		DC:             cfg.DC,
		APIID:          cfg.APIID,
		DeviceModel:    "pluma-admin",
		SystemVersion:  runtime.GOOS,
		AppVersion:     cfg.AppVersion,
		SystemLangCode: "en",
		LangCode:       "en",
	}, mtclient.KeyOptions{DC: cfg.DC})
	if err != nil {
		return nil, err
	}
	return &TelegramClient{c: c}, nil
}

// InvokeRaw sends req wrapped in invokeWithLayer(initConnection(req)), as a
// client does with the first request of a connection.
func (t *TelegramClient) InvokeRaw(ctx context.Context, req mtproto.TLObject) ([]byte, error) {
	x := mtproto.NewEncodeBuf(512)
	if err := req.Encode(x, t.c.Layer()); err != nil {
		return nil, err
	}
	return t.c.InvokeRaw(ctx, t.c.WrapInit(x.GetBuf()))
}

// Close closes the connection.
func (t *TelegramClient) Close() error {
	return t.c.Close()
}
