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
//	go run ./test/conformance/cmd/tglogin [-name main] [-test] [-qr]
//
// With -qr there is no code: the program shows a QR code, which is scanned
// in a Telegram app where the account is open (Settings, Devices, Link
// Desktop Device), as Telegram Desktop signs in.
package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"github.com/teamgram/teamgram-server/test/conformance"

	"golang.org/x/term"
	"rsc.io/qr"
)

func main() {
	name := flag.String("name", "main", "name of the saved session")
	test := flag.Bool("test", false, "the Telegram test environment instead of production")
	byQR := flag.Bool("qr", false, "sign in by scanning a QR code in an app where the account is open")
	flag.Parse()
	login := run
	if *byQR {
		login = runQR
	}
	if err := login(*name, *test); err != nil {
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
	signedIn := false
	defer func() {
		if !signedIn {
			cancelCode(c, phone, sent.GetPhoneCodeHash())
		}
	}()

	var o mtproto.TLObject
	for {
		describe(sent)
		code := secret("Код (не отображается; пустая строка — другой способ): ")
		if code == "" {
			next, err := resendCode(ctx, c, phone, sent)
			if err != nil {
				return err
			}
			sent = next
			continue
		}
		o, err = c.Invoke(ctx, &mtproto.TLAuthSignIn{
			Constructor: c.Constructor(mtproto.Predicate_auth_signIn), PhoneNumber: phone, PhoneCodeHash: sent.GetPhoneCodeHash(),
			PhoneCode_FLAGSTRING: mtproto.MakeFlagsString(code),
		})
		if mtclient.IsRPCError(err, "PHONE_CODE_INVALID") {
			fmt.Println("Неверный код.")
			continue
		}
		break
	}
	if mtclient.IsRPCError(err, "SESSION_PASSWORD_NEEDED") {
		o, err = checkPassword(ctx, c)
	}
	if err != nil {
		return fmt.Errorf("вход: %w", err)
	}
	signedIn = true
	if _, ok := o.(*mtproto.TLAuthAuthorizationSignUpRequired); ok {
		return errors.New("аккаунта с этим номером нет: создайте его в официальном приложении Telegram и запустите программу снова")
	}
	auth, ok := o.(*mtproto.TLAuthAuthorization)
	if !ok {
		return fmt.Errorf("вход: ответ %T", o)
	}
	return save(name, test, dc, c, auth.To_Auth_Authorization())
}

// describe says where Telegram sent the code and what else it offers.
func describe(sent *mtproto.TLAuthSentCode) {
	t := sent.GetType()
	switch t.GetPredicateName() {
	case mtproto.Predicate_auth_sentCodeTypeApp:
		fmt.Printf("Код из %d цифр отправлен сообщением от «Telegram» во все приложения, где открыт этот аккаунт.\n", t.GetLength())
		fmt.Println("Если за минуту он не пришёл, Telegram его не доставил: так бывает со сторонними приложениями с января 2026.")
		fmt.Println("Тогда войдите по QR (-qr) и не запрашивайте код снова много раз подряд.")
	case mtproto.Predicate_auth_sentCodeTypeSms:
		fmt.Printf("Код из %d цифр отправлен по SMS.\n", t.GetLength())
	case mtproto.Predicate_auth_sentCodeTypeCall:
		fmt.Printf("Код из %d цифр продиктуют в звонке.\n", t.GetLength())
	case mtproto.Predicate_auth_sentCodeTypeEmailCode:
		fmt.Printf("Код из %d цифр отправлен на почту %s.\n", t.GetLength(), t.GetEmailPattern())
	case mtproto.Predicate_auth_sentCodeTypeFragmentSms:
		fmt.Printf("Код из %d цифр отправлен SMS через Fragment: %s\n", t.GetLength(), t.GetUrl())
	default:
		fmt.Printf("Код отправлен (%s).\n", t.GetPredicateName())
	}
	next := sent.GetNextType()
	if next == nil {
		fmt.Println("Другого способа Telegram не предлагает.")
		return
	}
	how := map[string]string{
		mtproto.Predicate_auth_codeTypeSms:         "SMS",
		mtproto.Predicate_auth_codeTypeCall:        "звонок",
		mtproto.Predicate_auth_codeTypeFlashCall:   "сброшенный звонок",
		mtproto.Predicate_auth_codeTypeMissedCall:  "пропущенный звонок",
		mtproto.Predicate_auth_codeTypeFragmentSms: "SMS через Fragment",
	}[next.GetPredicateName()]
	if how == "" {
		how = next.GetPredicateName()
	}
	if timeout := sent.GetTimeout(); timeout != nil {
		fmt.Printf("Другой способ — %s, не раньше чем через %d с.\n", how, timeout.GetValue())
	} else {
		fmt.Printf("Другой способ — %s.\n", how)
	}
}

// resendCode asks Telegram for the code in the other way it offers.
func resendCode(ctx context.Context, c *mtclient.Client, phone string, sent *mtproto.TLAuthSentCode) (*mtproto.TLAuthSentCode, error) {
	if sent.GetNextType() == nil {
		return nil, errors.New("другого способа получить код нет: войдите по QR (-qr)")
	}
	o, err := c.Invoke(ctx, &mtproto.TLAuthResendCode{
		Constructor: c.Constructor(mtproto.Predicate_auth_resendCode), PhoneNumber: phone, PhoneCodeHash: sent.GetPhoneCodeHash(),
	})
	if err != nil {
		return nil, fmt.Errorf("повторная отправка кода: %w", err)
	}
	next, ok := o.(*mtproto.TLAuthSentCode)
	if !ok {
		return nil, fmt.Errorf("повторная отправка кода: ответ %T", o)
	}
	return next, nil
}

// cancelCode tells Telegram the code will not be used, as an official app
// does when the user goes back.
func cancelCode(c *mtclient.Client, phone, hash string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = c.Invoke(ctx, &mtproto.TLAuthCancelCode{
		Constructor: c.Constructor(mtproto.Predicate_auth_cancelCode), PhoneNumber: phone, PhoneCodeHash: hash,
	})
}

// save keeps the session of c, signed in as auth says.
func save(name string, test bool, dc int32, c *mtclient.Client, auth *mtproto.Auth_Authorization) error {
	u := auth.GetUser()
	path, err := conformance.SaveSession(name, &conformance.Session{
		Test: test, DC: dc, UserID: u.GetId(), AuthKey: c.AuthKey(), SavedAt: time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("сохранение сеанса: %w", err)
	}
	fmt.Printf("Готово: вошли как %s, сеанс сохранён в %s (DC %d).\n",
		strings.TrimSpace(u.GetFirstName().GetValue()+" "+u.GetLastName().GetValue()), path, dc)
	return nil
}

// runQR signs in by a login token (https://core.telegram.org/api/qr-login):
// the QR code of tg://login?token=..., renewed when it expires, until an
// app of the account accepts it.
func runQR(name string, test bool) error {
	tg, err := conformance.TelegramFromEnv(test)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	dc := int32(2)
	c, err := dial(ctx, tg, dc)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	fmt.Println("Откройте Telegram там, где открыт аккаунт запасного номера: Настройки → Устройства → «Подключить устройство» (Link Desktop Device), и отсканируйте код.")
	export := func() (mtproto.TLObject, error) {
		return c.Invoke(ctx, &mtproto.TLAuthExportLoginToken{
			Constructor: c.Constructor(mtproto.Predicate_auth_exportLoginToken),
			ApiId:       tg.APIID, ApiHash: tg.APIHash, ExceptIds: []int64{},
		})
	}
	o, err := export()
	for {
		if mtclient.IsRPCError(err, "SESSION_PASSWORD_NEEDED") {
			if o, err = checkPassword(ctx, c); err == nil {
				if auth, ok := o.(*mtproto.TLAuthAuthorization); ok {
					return save(name, test, dc, c, auth.To_Auth_Authorization())
				}
				return fmt.Errorf("вход: ответ %T", o)
			}
		}
		if err != nil {
			return fmt.Errorf("вход по QR: %w", err)
		}
		switch t := o.(type) {
		case *mtproto.TLAuthLoginToken:
			if err = showQR(t.GetToken()); err != nil {
				return err
			}
			// Until the app accepts the token, or it expires.
			wait := time.Until(time.Unix(int64(t.GetExpires()), 0))
			if wait < 5*time.Second || wait > time.Minute {
				wait = 30 * time.Second
			}
			wctx, wcancel := context.WithTimeout(ctx, wait)
			_, _ = c.WaitUpdate(wctx, func(u *mtproto.Updates) bool {
				return u.GetPredicateName() == mtproto.Predicate_updateShort &&
					u.GetUpdate().GetPredicateName() == mtproto.Predicate_updateLoginToken
			})
			wcancel()
			if ctx.Err() != nil {
				return errors.New("код не отсканирован за 10 минут")
			}
			o, err = export()
		case *mtproto.TLAuthLoginTokenMigrateTo:
			_ = c.Close()
			dc = t.GetDcId()
			if c, err = dial(ctx, tg, dc); err != nil {
				return err
			}
			o, err = c.Invoke(ctx, &mtproto.TLAuthImportLoginToken{
				Constructor: c.Constructor(mtproto.Predicate_auth_importLoginToken), Token: t.GetToken(),
			})
		case *mtproto.TLAuthLoginTokenSuccess:
			removeQR()
			return save(name, test, dc, c, t.GetAuthorization())
		default:
			return fmt.Errorf("вход по QR: ответ %T", o)
		}
	}
}

func qrFile() string { return filepath.Join(conformance.SessionDir(), "login-qr.png") }

// showQR prints the QR code of a login token in the terminal (light
// modules as blocks, so dark-on-light on a dark terminal) and writes it as
// a PNG beside the sessions, for a terminal that draws it badly.
func showQR(token []byte) error {
	code, err := qr.Encode("tg://login?token="+base64.RawURLEncoding.EncodeToString(token), qr.M)
	if err != nil {
		return err
	}
	const quiet = 2
	dark := func(x, y int) bool {
		return x >= 0 && y >= 0 && x < code.Size && y < code.Size && code.Black(x, y)
	}
	var b strings.Builder
	for y := -quiet; y < code.Size+quiet; y += 2 {
		for x := -quiet; x < code.Size+quiet; x++ {
			top, bottom := !dark(x, y), !dark(x, y+1)
			switch {
			case top && bottom:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bottom:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString("\n")
	}
	fmt.Print("\n" + b.String())
	if err = os.MkdirAll(conformance.SessionDir(), 0o700); err == nil {
		if err = os.WriteFile(qrFile(), code.PNG(), 0o600); err == nil {
			fmt.Printf("Тот же код картинкой: open %s\nКод обновляется примерно раз в 30 секунд.\n", qrFile())
		}
	}
	return nil
}

func removeQR() { _ = os.Remove(qrFile()) }

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
