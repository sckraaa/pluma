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

package conformance

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"github.com/teamgram/teamgram-server/pkg/serverkey"
)

// The data centers of Telegram as Telegram Desktop has them built in
// (Telegram/SourceFiles/mtproto/mtproto_dc_options.cpp): addresses and the
// public keys. In the key exchange a test DC is 10000 + its number.
var (
	productionDCs = map[int32]string{
		1: "149.154.175.50:443",
		2: "149.154.167.51:443",
		3: "149.154.175.100:443",
		4: "149.154.167.91:443",
		5: "149.154.171.5:443",
	}
	testDCs = map[int32]string{
		1: "149.154.175.10:443",
		2: "149.154.167.40:443",
		3: "149.154.175.117:443",
	}
)

const productionKey = `-----BEGIN RSA PUBLIC KEY-----
MIIBCgKCAQEA6LszBcC1LGzyr992NzE0ieY+BSaOW622Aa9Bd4ZHLl+TuFQ4lo4g
5nKaMBwK/BIb9xUfg0Q29/2mgIR6Zr9krM7HjuIcCzFvDtr+L0GQjae9H0pRB2OO
62cECs5HKhT5DZ98K33vmWiLowc621dQuwKWSQKjWf50XYFw42h21P2KXUGyp2y/
+aEyZ+uVgLLQbRA1dEjSDZ2iGRy12Mk5gpYc397aYp438fsJoHIgJ2lgMv5h7WY9
t6N/byY9Nw9p21Og3AoXSL2q/2IJ1WRUhebgAdGVMlV1fkuOQoEzR7EdpqtQD9Cs
5+bfo3Nhmcyvk5ftB0WkJ9z6bNZ7yxrP8wIDAQAB
-----END RSA PUBLIC KEY-----`

const testKey = `-----BEGIN RSA PUBLIC KEY-----
MIIBCgKCAQEAyMEdY1aR+sCR3ZSJrtztKTKqigvO/vBfqACJLZtS7QMgCGXJ6XIR
yy7mx66W0/sOFa7/1mAZtEoIokDP3ShoqF4fVNb6XeqgQfaUHd8wJpDWHcR2OFwv
plUUI1PLTktZ9uW2WE23b+ixNwJjJGwBDJPQEQFBE+vfmH0JP503wr5INS1poWg/
j25sIWeYPHYeOrFp/eXaqhISP6G+q2IeTaWTXpwZj4LzXq5YOpk4bYEQ6mvRq7D1
aHWfYmlEGepfaYR8Q0YqvvhYtMte3ITnuSJs171+GDqpdKcSwHnd6FudwGO4pcCO
j4WcDuXc2CTHgH8gFTNhp/Y8/SpDOhvn9QIDAQAB
-----END RSA PUBLIC KEY-----`

// Telegram is one environment of Telegram: production or test.
type Telegram struct {
	Test    bool
	APIID   int32
	APIHash string
}

// TelegramFromEnv reads the api_id of the project (~/pluma-secrets/
// telegram-api.env: TELEGRAM_API_ID, TELEGRAM_API_HASH).
func TelegramFromEnv(test bool) (*Telegram, error) {
	id, err := strconv.ParseInt(os.Getenv("TELEGRAM_API_ID"), 10, 32)
	if err != nil || os.Getenv("TELEGRAM_API_HASH") == "" {
		return nil, errors.New("TELEGRAM_API_ID and TELEGRAM_API_HASH are not set (~/pluma-secrets/telegram-api.env)")
	}
	return &Telegram{Test: test, APIID: int32(id), APIHash: os.Getenv("TELEGRAM_API_HASH")}, nil
}

func (tg *Telegram) key() (*rsa.PublicKey, error) {
	if tg.Test {
		return serverkey.ParsePublicKeyPEM([]byte(testKey))
	}
	return serverkey.ParsePublicKeyPEM([]byte(productionKey))
}

// Config is the client config for DC dc; the address is the built-in one
// unless addr is set.
func (tg *Telegram) Config(dc int32, addr string, logf func(string, ...any)) (mtclient.Config, error) {
	key, err := tg.key()
	if err != nil {
		return mtclient.Config{}, err
	}
	dcs, exchangeDC := productionDCs, dc
	if tg.Test {
		dcs, exchangeDC = testDCs, 10000+dc
	}
	if addr == "" {
		if addr = dcs[dc]; addr == "" {
			return mtclient.Config{}, fmt.Errorf("no address of DC %d", dc)
		}
	}
	return mtclient.Config{
		Addr: addr, PublicKey: key, DC: exchangeDC, APIID: tg.APIID,
		DeviceModel: "Pluma conformance", SystemVersion: "go", AppVersion: "1.0", Logf: logf,
	}, nil
}

var migrateError = regexp.MustCompile(`^(PHONE|USER|NETWORK)_MIGRATE_(\d+)$`)

// MigrateDC returns the DC a *_MIGRATE_X error sends the client to.
func MigrateDC(err error) (int32, bool) {
	var e *mtclient.RPCError
	if !errors.As(err, &e) || e.Code != 303 {
		return 0, false
	}
	m := migrateError.FindStringSubmatch(e.Message)
	if m == nil {
		return 0, false
	}
	n, err := strconv.ParseInt(m[2], 10, 32)
	return int32(n), err == nil
}

// Session is a signed-in authorization of Telegram, saved by the login
// program of the owner (cmd/tglogin) and used by the recordings.
type Session struct {
	Test    bool              `json:"test"`
	DC      int32             `json:"dc"`
	Addr    string            `json:"addr"`
	UserID  int64             `json:"user_id"`
	AuthKey *mtclient.AuthKey `json:"auth_key"`
	SavedAt time.Time         `json:"saved_at"`
}

// SessionDir is where the sessions are kept, outside the repository.
func SessionDir() string {
	return filepath.Join(os.Getenv("HOME"), "pluma-secrets", "telegram-prod")
}

// SaveSession writes s as name, readable by the owner only.
func SaveSession(name string, s *Session) (string, error) {
	if err := os.MkdirAll(SessionDir(), 0o700); err != nil {
		return "", err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	path := filepath.Join(SessionDir(), name+".json")
	return path, os.WriteFile(path, b, 0o600)
}

// LoadSession reads the session name.
func LoadSession(name string) (*Session, error) {
	b, err := os.ReadFile(filepath.Join(SessionDir(), name+".json"))
	if err != nil {
		return nil, err
	}
	var s Session
	if err = json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// DialSession connects with a saved session, its key and DC.
func (tg *Telegram) DialSession(ctx context.Context, s *Session, logf func(string, ...any)) (*mtclient.Client, error) {
	cfg, err := tg.Config(s.DC, s.Addr, logf)
	if err != nil {
		return nil, err
	}
	c, err := mtclient.DialKey(ctx, cfg, s.AuthKey)
	if err != nil {
		return nil, err
	}
	if _, err = c.InitConnection(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}
