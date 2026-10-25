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

// tglogin signs the account for the conformance fixtures into Telegram and
// saves the session in ~/pluma-secrets/telegram-prod (task 007). The owner
// runs it and types the number, the code and the 2FA password; nothing of
// them is printed or kept, only the session (DC and auth key).
//
//	set -a; . ~/pluma-secrets/telegram-api.env; set +a
//	go run ./test/conformance/cmd/tglogin [-name main] [-test]
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"github.com/teamgram/teamgram-server/test/conformance"

	"golang.org/x/term"
)

func main() {
	name := flag.String("name", "main", "name of the saved session")
	test := flag.Bool("test", false, "the Telegram test environment instead of production")
	flag.Parse()
	if err := run(*name, *test); err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка:", err)
		os.Exit(1)
	}
}

func run(name string, test bool) error {
	tg, err := conformance.TelegramFromEnv(test)
	if err != nil {
		return err
	}
	in := bufio.NewReader(os.Stdin)
	phone := strings.TrimSpace(strings.TrimPrefix(ask(in, "Номер телефона (с кодом страны, например +79991234567): "), "+"))
	if phone == "" {
		return errors.New("номер не введён")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	dc := int32(2)
	var (
		c    *mtclient.Client
		sent *mtproto.TLAuthSentCode
	)
	for attempt := 0; attempt < 3; attempt++ {
		if c, err = dial(ctx, tg, dc); err != nil {
			return err
		}
		o, err := c.Invoke(ctx, &mtproto.TLAuthSendCode{
			Constructor: c.Constructor(mtproto.Predicate_auth_sendCode),
			PhoneNumber: phone, ApiId: tg.APIID, ApiHash: tg.APIHash,
			Settings: &mtproto.CodeSettings{Constructor: c.Constructor(mtproto.Predicate_codeSettings)},
		})
		if to, ok := conformance.MigrateDC(err); ok {
			_ = c.Close()
			dc = to
			continue
		}
		if err != nil {
			_ = c.Close()
			return fmt.Errorf("запрос кода: %w", err)
		}
		s, ok := o.(*mtproto.TLAuthSentCode)
		if !ok {
			_ = c.Close()
			return fmt.Errorf("запрос кода: ответ %T", o)
		}
		sent = s
		break
	}
	if sent == nil {
		return errors.New("сервер переадресовал слишком много раз")
	}
	defer c.Close()

	switch sent.GetType().GetPredicateName() {
	case mtproto.Predicate_auth_sentCodeTypeApp:
		fmt.Println("Код отправлен в приложение Telegram, где этот аккаунт уже открыт.")
	case mtproto.Predicate_auth_sentCodeTypeSms:
		fmt.Println("Код отправлен по SMS.")
	default:
		fmt.Printf("Код отправлен (%s).\n", sent.GetType().GetPredicateName())
	}
	code := secret("Код (не отображается): ")
	o, err := c.Invoke(ctx, &mtproto.TLAuthSignIn{
		Constructor: c.Constructor(mtproto.Predicate_auth_signIn), PhoneNumber: phone, PhoneCodeHash: sent.GetPhoneCodeHash(),
		PhoneCode_FLAGSTRING: mtproto.MakeFlagsString(code),
	})
	if mtclient.IsRPCError(err, "SESSION_PASSWORD_NEEDED") {
		o, err = checkPassword(ctx, c)
	}
	if err != nil {
		return fmt.Errorf("вход: %w", err)
	}
	if _, ok := o.(*mtproto.TLAuthAuthorizationSignUpRequired); ok {
		return errors.New("аккаунта с этим номером нет: создайте его в официальном приложении Telegram и запустите программу снова")
	}
	auth, ok := o.(*mtproto.TLAuthAuthorization)
	if !ok {
		return fmt.Errorf("вход: ответ %T", o)
	}
	path, err := conformance.SaveSession(name, &conformance.Session{
		Test: test, DC: dc, UserID: auth.GetUser().GetId(), AuthKey: c.AuthKey(), SavedAt: time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("сохранение сеанса: %w", err)
	}
	fmt.Printf("Готово: вошли как %s, сеанс сохранён в %s (DC %d).\n",
		strings.TrimSpace(auth.GetUser().GetFirstName().GetValue()+" "+auth.GetUser().GetLastName().GetValue()), path, dc)
	return nil
}

func dial(ctx context.Context, tg *conformance.Telegram, dc int32) (*mtclient.Client, error) {
	cfg, err := tg.Config(dc, "", nil)
	if err != nil {
		return nil, err
	}
	c, err := mtclient.Dial(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("подключение к DC %d: %w", dc, err)
	}
	if _, err = c.InitConnection(ctx); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("подключение к DC %d: %w", dc, err)
	}
	return c, nil
}

// checkPassword asks for the 2FA password and checks it by SRP.
func checkPassword(ctx context.Context, c *mtclient.Client) (mtproto.TLObject, error) {
	o, err := c.Invoke(ctx, &mtproto.TLAccountGetPassword{Constructor: c.Constructor(mtproto.Predicate_account_getPassword)})
	if err != nil {
		return nil, err
	}
	pw, ok := o.(interface {
		To_Account_Password() *mtproto.Account_Password
	})
	if !ok {
		return nil, fmt.Errorf("account.getPassword: ответ %T", o)
	}
	hint := pw.To_Account_Password().GetHint().GetValue()
	prompt := "Пароль двухэтапной проверки (не отображается): "
	if hint != "" {
		prompt = fmt.Sprintf("Пароль двухэтапной проверки, подсказка «%s» (не отображается): ", hint)
	}
	srp, err := mtclient.CheckPassword(pw.To_Account_Password(), secret(prompt))
	if err != nil {
		return nil, err
	}
	return c.Invoke(ctx, &mtproto.TLAuthCheckPassword{Constructor: c.Constructor(mtproto.Predicate_auth_checkPassword), Password: srp})
}

func ask(in *bufio.Reader, prompt string) string {
	fmt.Print(prompt)
	s, _ := in.ReadString('\n')
	return strings.TrimSpace(s)
}

// secret reads a line without echo.
func secret(prompt string) string {
	fmt.Print(prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка чтения:", err)
		os.Exit(1)
	}
	return strings.TrimSpace(string(b))
}
