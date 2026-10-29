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
// saves the session in ~/pluma-secrets/telegram-prod, or telegram-test for
// the test environment (task 007). The owner runs it and types the number,
// the code and the 2FA password; nothing of them is printed or kept, only
// the session (DC and auth key).
//
//	set -a; . ~/pluma-secrets/telegram-api.env; set +a
//	go run ./test/conformance/cmd/tglogin [-name main] [-test] [-qr]
//
// With -qr there is no code: the program shows a QR code, which is scanned
// in a Telegram app where the account is open (Settings, Devices, Link
// Desktop Device), as Telegram Desktop signs in.
//
// After the login it stays online for a minute, as an official app does,
// prints what Telegram tells the account meanwhile and checks that the
// session still works. The types of the messages it sent and received (no
// content) go to <name>.log beside the session: if Telegram ends the
// session, the log says when.
//
// With -service-texts FILE it writes, right after the login, the messages of
// Telegram's service account to the account as templates without what
// belongs to it (conformance.ServiceTexts, task 017), and again 25 seconds
// later, when the message about this login has come: Telegram ends a
// third-party session within a minute.
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
	"sync"
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
	flag.StringVar(&serviceTextsOut, "service-texts", "", "after the login, write the messages of the service account as templates to this file")
	flag.Parse()
	login := run
	if *byQR {
		login = runQR
	}
	err := withLog(*name, *test, func(logf func(string, ...any)) error { return login(*name, *test, logf) })
	if err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка:", err)
		os.Exit(1)
	}
}

// withLog runs login with a log of the connections in <name>.log beside
// the session: TL types and message ids with the time, never content.
func withLog(name string, test bool, login func(logf func(string, ...any)) error) error {
	path, err := conformance.SessionLog(test, name)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(conformance.SessionDir(test), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	var mu sync.Mutex
	logf := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		_, _ = fmt.Fprintf(f, "%s %s\n", time.Now().UTC().Format(time.RFC3339Nano), fmt.Sprintf(format, args...))
	}
	logf("tglogin start")
	err = login(logf)
	logf("tglogin end: %v", err)
	return err
}

func run(name string, test bool, logf func(string, ...any)) error {
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
		if c, err = dial(ctx, tg, dc, logf); err != nil {
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
		describe(sent, test)
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
	if err = save(name, test, dc, c, auth.To_Auth_Authorization()); err != nil {
		return err
	}
	return stayOnline(tg, dc, c, logf)
}

// describe says where Telegram sent the code and what else it offers.
func describe(sent *mtproto.TLAuthSentCode, test bool) {
	t := sent.GetType()
	switch t.GetPredicateName() {
	case mtproto.Predicate_auth_sentCodeTypeApp:
		fmt.Printf("Код из %d цифр отправлен сообщением от «Telegram» во все приложения, где открыт этот аккаунт.\n", t.GetLength())
		if !test {
			fmt.Println("Если за минуту он не пришёл, Telegram его не доставил: так бывает со сторонними приложениями с января 2026.")
			fmt.Println("Тогда войдите по QR (-qr) и не запрашивайте код снова много раз подряд.")
		}
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
	serviceTextsSelf = u
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
func runQR(name string, test bool, logf func(string, ...any)) error {
	tg, err := conformance.TelegramFromEnv(test)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	dc := int32(2)
	c, err := dial(ctx, tg, dc, logf)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	where := "аккаунт запасного номера"
	if test {
		where = "аккаунт тестовой среды"
	}
	fmt.Printf("Откройте Telegram там, где открыт %s: Настройки → Устройства → «Подключить устройство» (Link Desktop Device), и отсканируйте код.\n", where)
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
					if err = save(name, test, dc, c, auth.To_Auth_Authorization()); err != nil {
						return err
					}
					return stayOnline(tg, dc, c, logf)
				}
				return fmt.Errorf("вход: ответ %T", o)
			}
		}
		if err != nil {
			return fmt.Errorf("вход по QR: %w", err)
		}
		switch t := o.(type) {
		case *mtproto.TLAuthLoginToken:
			if err = showQR(t.GetToken(), test); err != nil {
				return err
			}
			// Until the app accepts the token, or it expires.
			wait := time.Until(time.Unix(int64(t.GetExpires()), 0))
			if wait < 5*time.Second || wait > time.Minute {
				wait = 30 * time.Second
			}
			wctx, wcancel := context.WithTimeout(ctx, wait)
			_, _ = c.WaitUpdate(wctx, func(u *mtproto.Updates) bool {
				return len(updatesOf(u, mtproto.Predicate_updateLoginToken)) > 0
			})
			wcancel()
			if ctx.Err() != nil {
				return errors.New("код не отсканирован за 10 минут")
			}
			o, err = export()
		case *mtproto.TLAuthLoginTokenMigrateTo:
			_ = c.Close()
			dc = t.GetDcId()
			if c, err = dial(ctx, tg, dc, logf); err != nil {
				return err
			}
			o, err = c.Invoke(ctx, &mtproto.TLAuthImportLoginToken{
				Constructor: c.Constructor(mtproto.Predicate_auth_importLoginToken), Token: t.GetToken(),
			})
		case *mtproto.TLAuthLoginTokenSuccess:
			removeQR(test)
			if err = save(name, test, dc, c, t.GetAuthorization()); err != nil {
				return err
			}
			return stayOnline(tg, dc, c, logf)
		default:
			return fmt.Errorf("вход по QR: ответ %T", o)
		}
	}
}

func qrFile(test bool) string { return filepath.Join(conformance.SessionDir(test), "login-qr.png") }

// showQR prints the QR code of a login token in the terminal (light
// modules as blocks, so dark-on-light on a dark terminal) and writes it as
// a PNG beside the sessions, for a terminal that draws it badly.
func showQR(token []byte, test bool) error {
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
	if err = os.MkdirAll(conformance.SessionDir(test), 0o700); err == nil {
		if err = os.WriteFile(qrFile(test), code.PNG(), 0o600); err == nil {
			fmt.Printf("Тот же код картинкой: open %s\nКод обновляется примерно раз в 30 секунд.\n", qrFile(test))
		}
	}
	return nil
}

func removeQR(test bool) { _ = os.Remove(qrFile(test)) }

// dial connects to DC dc with a new key; mtclient.Dial sends initConnection
// once.
func dial(ctx context.Context, tg *conformance.Telegram, dc int32, logf func(string, ...any)) (*mtclient.Client, error) {
	cfg, err := tg.Config(dc, "", logf)
	if err != nil {
		return nil, err
	}
	c, err := mtclient.Dial(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("подключение к DC %d: %w", dc, err)
	}
	return c, nil
}

// online is how long the program stays connected after the login.
const online = time.Minute

// serviceTextsOut is the file of -service-texts. It holds the history of
// the service account (serviceTextsDoc) and what comes while the session
// lives (serviceTextsLive): a new login, a code, as Telegram sends them to
// this session, in its language.
var (
	serviceTextsOut  string
	serviceTextsSelf *mtproto.User
	serviceTextsDoc  string
	serviceTextsLive []string
)

func flushServiceTexts() error {
	return os.WriteFile(serviceTextsOut, []byte(serviceTextsDoc+strings.Join(serviceTextsLive, "")), 0o600)
}

// liveServiceText adds a text that came to the session to serviceTextsOut.
func liveServiceText(kind, text string, entities []*mtproto.MessageEntity, logf func(string, ...any)) {
	if serviceTextsOut == "" || serviceTextsSelf == nil {
		return
	}
	t := conformance.ScrubServiceText(serviceTextsSelf, text, entities)
	serviceTextsLive = append(serviceTextsLive, fmt.Sprintf("\n## Came to this session: %s, %s\n\n```\n%s\n```\n",
		kind, time.Now().UTC().Format("2006-01-02 15:04 UTC"), t))
	if err := flushServiceTexts(); err != nil {
		logf("service texts: %v", err)
	}
}

// writeServiceTexts writes the templates of the service account's messages
// to serviceTextsOut; it tells how it went and never stops the login.
func writeServiceTexts(ctx context.Context, c *mtclient.Client, logf func(string, ...any)) {
	tctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	doc, messages, texts, err := conformance.ServiceTexts(tctx, c, 200)
	if err == nil {
		serviceTextsDoc = doc
		err = flushServiceTexts()
	}
	if err != nil {
		logf("service texts: %v", err)
		fmt.Println("Тексты служебных сообщений не записаны:", err)
		return
	}
	logf("service texts: %d messages, %d texts", messages, texts)
	fmt.Printf("Тексты служебных сообщений: %d сообщений, %d разных, в %s\n", messages, texts, serviceTextsOut)
}

// stayOnline keeps the new session connected for a while, as an official
// app does after a login: it asks for the state of updates (so that the
// session gets them), prints what Telegram tells the account and checks at
// the end that the session still works, over a new connection if this one
// was lost. It has its own deadline: the login may have taken its time.
func stayOnline(tg *conformance.Telegram, dc int32, c *mtclient.Client, logf func(string, ...any)) error {
	ctx, cancel := context.WithTimeout(context.Background(), online+time.Minute)
	defer cancel()
	state := func(c *mtclient.Client) error {
		sctx, scancel := context.WithTimeout(ctx, 20*time.Second)
		defer scancel()
		_, err := c.Invoke(sctx, &mtproto.TLUpdatesGetState{Constructor: c.Constructor(mtproto.Predicate_updates_getState)})
		return err
	}
	if serviceTextsOut != "" {
		writeServiceTexts(ctx, c, logf)
	}
	if err := state(c); err != nil {
		return sessionEnded(err, logf)
	}
	fmt.Printf("Остаёмся на связи %v, как официальное приложение после входа. Прервать можно Ctrl+C: сеанс уже сохранён.\n", online)
	wctx, wcancel := context.WithTimeout(ctx, online)
	defer wcancel()
	// The second reading of the service texts, once the message about
	// this login has come.
	again, read := time.Now().Add(25*time.Second), serviceTextsOut == ""
	for {
		uctx, ucancel := wctx, context.CancelFunc(func() {})
		if !read {
			uctx, ucancel = context.WithDeadline(wctx, again)
		}
		u, err := c.WaitUpdate(uctx, func(*mtproto.Updates) bool { return true })
		ucancel()
		if !read && wctx.Err() == nil && !time.Now().Before(again) {
			writeServiceTexts(ctx, c, logf)
			read = true
		}
		if err != nil {
			if wctx.Err() == nil && c.Err() == nil {
				continue
			}
			break
		}
		report(u, logf)
	}
	if err := c.Err(); err != nil {
		// The connection was lost: check the session over a new one, as an
		// app reconnects (one connection at a time).
		logf("the connection stopped: %v", err)
		_ = c.Close()
		cfg, err := tg.Config(dc, "", logf)
		if err != nil {
			return err
		}
		if c, err = mtclient.DialKey(ctx, cfg, c.AuthKey()); err != nil {
			return sessionEnded(err, logf)
		}
		defer func() { _ = c.Close() }()
		if _, err = c.InitConnection(ctx); err != nil {
			return sessionEnded(err, logf)
		}
	}
	if err := state(c); err != nil {
		return sessionEnded(err, logf)
	}
	logf("the session works after %v", online)
	fmt.Println("Сеанс работает.")
	return nil
}

// telegramUserID is the account Telegram sends its service messages from.
const telegramUserID = 777000

// report prints what Telegram tells the account in u: service
// notifications, messages of its service account, a new login.
func report(u *mtproto.Updates, logf func(string, ...any)) {
	for _, n := range updatesOf(u, mtproto.Predicate_updateServiceNotification) {
		logf("updateServiceNotification type=%s popup=%v", n.GetType(), n.GetPopup())
		fmt.Printf("Уведомление Telegram для аккаунта (%s): %s\n", n.GetType(), n.GetMessage_STRING())
		liveServiceText("updateServiceNotification "+n.GetType(), n.GetMessage_STRING(), n.GetEntities(), logf)
	}
	if u.GetPredicateName() == mtproto.Predicate_updateShortMessage && u.GetUserId() == telegramUserID {
		logf("updateShortMessage from %d", telegramUserID)
		fmt.Printf("Сообщение от «Telegram»: %s\n", u.GetMessage())
		liveServiceText("updateShortMessage", u.GetMessage(), u.GetEntities(), logf)
	}
	for _, m := range updatesOf(u, mtproto.Predicate_updateNewMessage) {
		if msg := m.GetMessage_MESSAGE(); msg.GetPeerId().GetUserId() == telegramUserID {
			logf("updateNewMessage from %d", telegramUserID)
			fmt.Printf("Сообщение от «Telegram»: %s\n", msg.GetMessage())
			liveServiceText("updateNewMessage", msg.GetMessage(), msg.GetEntities(), logf)
		}
	}
	if len(updatesOf(u, mtproto.Predicate_updateNewAuthorization)) > 0 {
		logf("updateNewAuthorization")
		fmt.Println("Telegram сообщил аккаунту о новом входе.")
	}
}

// sessionEnded explains an error of the new session: Telegram no longer
// knows the key (transport error -404, or 401), or the check failed.
func sessionEnded(err error, logf func(string, ...any)) error {
	logf("the session failed: %v", err)
	var rpc *mtclient.RPCError
	if mtclient.IsTransportError(err, -404) || (errors.As(err, &rpc) && rpc.Code == 401) {
		return fmt.Errorf("сеанс завершён Telegram (%w); время — в журнале рядом с сеансом", err)
	}
	return fmt.Errorf("сеанс сохранён, но проверить его не удалось: %w", err)
}

// updatesOf returns the updates of u with the predicate, in a short update
// or in a list.
func updatesOf(u *mtproto.Updates, predicate string) []*mtproto.Update {
	var list []*mtproto.Update
	if u.GetPredicateName() == mtproto.Predicate_updateShort {
		list = []*mtproto.Update{u.GetUpdate()}
	} else {
		list = u.GetUpdates()
	}
	var out []*mtproto.Update
	for _, x := range list {
		if x.GetPredicateName() == predicate {
			out = append(out, x)
		}
	}
	return out
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
