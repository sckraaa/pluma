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

// Package account is the Telegram account that writes the login codes, on
// TDLib (ADR 0042). The methods and states are those of td_api.tl of the
// commit pinned in teamgramd/deploy/codes/Dockerfile. Nothing here logs a
// phone, a text or a TDLib object.
package account

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/teamgram/teamgram-server/app/ops/codes/internal/sender"
	"github.com/teamgram/teamgram-server/app/ops/codes/internal/tdjson"
)

// Params are the TDLib parameters of the account.
type Params struct {
	APIID       int32
	APIHash     string
	DatabaseDir string
	// EncryptionKey encrypts the TDLib database, session included.
	EncryptionKey []byte
}

// Error is an error object of TDLib.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("tdlib error %d: %s", e.Code, e.Message) }

// retryAfterPrefix is the text of TDLib's flood waits, the way TDLib itself
// parses it (Global::get_retry_after, td/telegram/Global.cpp).
const retryAfterPrefix = "Too Many Requests: retry after "

// RetryAfter returns the wait of a 429 error, 0 for any other.
func (e *Error) RetryAfter() time.Duration {
	if e.Code != 429 || !strings.HasPrefix(e.Message, retryAfterPrefix) {
		return 0
	}
	n, err := strconv.Atoi(e.Message[len(retryAfterPrefix):])
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

// Login is what the operator's login needs from the terminal (the tool
// command `codes login`).
type Login struct {
	// ShowQR shows a tg:// link as a QR code. TDLib updates the link
	// frequently, so it is called again with each new one.
	ShowQR func(link string)
	// Password asks for the two-step verification password. hint may be empty.
	Password func(hint string) (string, error)
	// Done is called once the account is authorized, or the login failed.
	Done func(err error)
}

type result struct {
	data json.RawMessage
	err  error
}

// Account drives one TDLib client. Run reads TDLib on a single goroutine;
// the other methods can be called from any.
type Account struct {
	lib    tdjson.Library
	params Params
	login  *Login

	mu      sync.Mutex
	client  int
	session string
	seq     int64
	pending map[string]chan result
	// The confirmation of a sent message arrives as an update, keyed by the
	// temporary message id; early holds those that arrived before the
	// answer to sendMessage was read.
	sends map[int64]chan error
	early map[int64]error

	closing  bool
	closedCh chan struct{}
	asked    bool
}

// New returns an Account that serves codes (login nil) or walks the operator
// through a QR login.
func New(lib tdjson.Library, params Params, login *Login) *Account {
	return &Account{
		lib:      lib,
		params:   params,
		login:    login,
		session:  sender.SessionStarting,
		pending:  map[string]chan result{},
		sends:    map[int64]chan error{},
		early:    map[int64]error{},
		closedCh: make(chan struct{}),
	}
}

// Session returns the state of the session (sender.Session*).
func (a *Account) Session() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.session
}

func (a *Account) setSession(s string) {
	a.mu.Lock()
	changed := a.session != s
	a.session = s
	a.mu.Unlock()
	if changed {
		log.Printf("account: session state %s", s)
	}
}

// quiet stops TDLib's own log: its verbose levels print requests. The
// synchronous calls are allowed before any client exists.
func (a *Account) quiet() {
	a.lib.Execute(`{"@type":"setLogStream","log_stream":{"@type":"logStreamEmpty"}}`)
	a.lib.Execute(`{"@type":"setLogVerbosityLevel","new_verbosity_level":0}`)
}

func (a *Account) start() {
	client := a.lib.CreateClient()
	a.mu.Lock()
	a.client = client
	a.asked = false
	a.mu.Unlock()
	// A client starts on its first request (td_json_client.h).
	a.lib.Send(client, `{"@type":"getOption","name":"version"}`)
}

// Run reads TDLib until ctx is cancelled and the client has closed. When the
// session ends unexpectedly TDLib closes the client, which has to be created
// anew (authorizationStateClosed); the new one then reports "no session".
func (a *Account) Run(ctx context.Context) {
	a.quiet()
	a.start()
	stop := ctx.Done()
	for {
		select {
		case <-stop:
			stop = nil
			a.mu.Lock()
			a.closing = true
			client := a.client
			a.mu.Unlock()
			a.lib.Send(client, `{"@type":"close"}`)
		case <-a.closedCh:
			return
		default:
		}
		if event := a.lib.Receive(time.Second); event != "" {
			a.handle(event)
		}
	}
}

type envelope struct {
	Type     string `json:"@type"`
	Extra    string `json:"@extra"`
	ClientID int    `json:"@client_id"`
}

func (a *Account) handle(event string) {
	var e envelope
	if err := json.Unmarshal([]byte(event), &e); err != nil {
		log.Printf("account: an unreadable event of TDLib (%d bytes)", len(event))
		return
	}
	a.mu.Lock()
	current := a.client
	a.mu.Unlock()
	if e.ClientID != current {
		return
	}
	if e.Extra != "" {
		a.resolve(e.Extra, e.Type, []byte(event))
		return
	}
	switch e.Type {
	case "updateAuthorizationState":
		var u struct {
			State json.RawMessage `json:"authorization_state"`
		}
		if json.Unmarshal([]byte(event), &u) == nil {
			a.onAuthorizationState(u.State)
		}
	case "updateMessageSendSucceeded":
		var u struct {
			Old int64 `json:"old_message_id"`
		}
		if json.Unmarshal([]byte(event), &u) == nil {
			a.onSendResult(u.Old, nil)
		}
	case "updateMessageSendFailed":
		var u struct {
			Old   int64 `json:"old_message_id"`
			Error Error `json:"error"`
		}
		if json.Unmarshal([]byte(event), &u) == nil {
			err := u.Error
			a.onSendResult(u.Old, &err)
		}
	}
}

func (a *Account) resolve(extra, typ string, data []byte) {
	a.mu.Lock()
	ch := a.pending[extra]
	delete(a.pending, extra)
	a.mu.Unlock()
	if ch == nil {
		return
	}
	if typ == "error" {
		var e Error
		if err := json.Unmarshal(data, &e); err != nil {
			ch <- result{err: err}
			return
		}
		ch <- result{err: &e}
		return
	}
	ch <- result{data: data}
}

func (a *Account) onSendResult(oldID int64, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if ch := a.sends[oldID]; ch != nil {
		delete(a.sends, oldID)
		ch <- err
		return
	}
	a.early[oldID] = err
	if len(a.early) > 128 {
		a.early = map[int64]error{oldID: err}
	}
}

// call sends a request and waits for its answer; not for the Run goroutine.
func (a *Account) call(ctx context.Context, request map[string]any) (json.RawMessage, error) {
	a.mu.Lock()
	a.seq++
	extra := strconv.FormatInt(a.seq, 10)
	ch := make(chan result, 1)
	a.pending[extra] = ch
	client := a.client
	a.mu.Unlock()

	request["@extra"] = extra
	data, err := json.Marshal(request)
	if err != nil {
		a.forget(extra)
		return nil, err
	}
	a.lib.Send(client, string(data))
	select {
	case r := <-ch:
		return r.data, r.err
	case <-ctx.Done():
		a.forget(extra)
		return nil, ctx.Err()
	}
}

func (a *Account) forget(extra string) {
	a.mu.Lock()
	delete(a.pending, extra)
	a.mu.Unlock()
}

// callAsync runs a request from the Run goroutine, which cannot wait.
func (a *Account) callAsync(request map[string]any, onError func(error)) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := a.call(ctx, request); err != nil && onError != nil {
			onError(err)
		}
	}()
}

func (a *Account) onAuthorizationState(raw json.RawMessage) {
	var state struct {
		Type string `json:"@type"`
		Link string `json:"link"`
		Hint string `json:"password_hint"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return
	}
	switch state.Type {
	case "authorizationStateWaitTdlibParameters":
		a.callAsync(a.tdlibParameters(), func(err error) {
			// 401: the database encryption key is wrong (td_api.tl).
			log.Printf("account: TDLib rejects the parameters or the database key")
			a.setSession(sender.SessionBroken)
			a.finishLogin(fmt.Errorf("open the TDLib database: %w", err))
		})
	case "authorizationStateWaitPhoneNumber":
		a.setSession(sender.SessionNoSession)
		if a.login != nil {
			a.callAsync(map[string]any{"@type": "requestQrCodeAuthentication", "other_user_ids": []int64{}}, func(err error) {
				a.finishLogin(fmt.Errorf("request a QR code: %w", err))
			})
		}
	case "authorizationStateWaitOtherDeviceConfirmation":
		if a.login != nil {
			a.login.ShowQR(state.Link)
		}
	case "authorizationStateWaitPassword":
		a.setSession(sender.SessionNoSession)
		if a.login != nil {
			go a.askPassword(state.Hint)
		}
	case "authorizationStateReady":
		a.setSession(sender.SessionReady)
		if a.login != nil {
			a.finishLogin(nil)
		}
	case "authorizationStateLoggingOut", "authorizationStateClosing":
		a.setSession(sender.SessionNoSession)
	case "authorizationStateClosed":
		a.onClosed()
	default:
		// The phone-number login states are never asked for: only QR.
		if a.login == nil {
			a.setSession(sender.SessionNoSession)
		}
	}
}

func (a *Account) tdlibParameters() map[string]any {
	key := ""
	if len(a.params.EncryptionKey) > 0 {
		key = base64.StdEncoding.EncodeToString(a.params.EncryptionKey)
	}
	return map[string]any{
		"@type":                   "setTdlibParameters",
		"use_test_dc":             false,
		"database_directory":      a.params.DatabaseDir,
		"files_directory":         "",
		"database_encryption_key": key,
		// The session is all that has to survive: no message or file cache.
		"use_file_database":      false,
		"use_chat_info_database": false,
		"use_message_database":   false,
		"use_secret_chats":       false,
		"api_id":                 a.params.APIID,
		"api_hash":               a.params.APIHash,
		"system_language_code":   "ru",
		"device_model":           "Server",
		"system_version":         "",
		"application_version":    "1.0",
	}
}

func (a *Account) askPassword(hint string) {
	password, err := a.login.Password(hint)
	if err != nil {
		a.finishLogin(fmt.Errorf("read the password: %w", err))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := a.call(ctx, map[string]any{"@type": "checkAuthenticationPassword", "password": password}); err != nil {
		var te *Error
		if errors.As(err, &te) && te.Code == 400 {
			// A wrong password: ask again, the state stays.
			fmt.Println("Неверный пароль.")
			a.askPassword(hint)
			return
		}
		a.finishLogin(fmt.Errorf("check the password: %w", err))
	}
}

func (a *Account) finishLogin(err error) {
	if a.login != nil && a.login.Done != nil {
		a.login.Done(err)
	}
}

func (a *Account) onClosed() {
	a.mu.Lock()
	closing := a.closing
	for extra, ch := range a.pending {
		ch <- result{err: errors.New("the TDLib client closed")}
		delete(a.pending, extra)
	}
	for id, ch := range a.sends {
		ch <- errors.New("the TDLib client closed")
		delete(a.sends, id)
	}
	a.early = map[int64]error{}
	a.mu.Unlock()
	if closing {
		close(a.closedCh)
		return
	}
	// The session ended by itself (revoked, or logged out on Telegram's
	// side). TDLib has removed it; the new client finds no session.
	a.setSession(sender.SessionNoSession)
	log.Printf("account: the TDLib client closed unexpectedly, the session is gone")
	time.Sleep(time.Second)
	a.start()
}

// Deliver imports the phone, sends text and reports the outcome. The contact
// stays imported when it returns: the caller removes it (Delivery.UserID).
func (a *Account) Deliver(ctx context.Context, phone, text string) sender.Delivery {
	raw, err := a.call(ctx, map[string]any{
		"@type": "importContacts",
		"contacts": []map[string]any{{
			"@type":        "importedContact",
			"phone_number": "+" + phone,
			"first_name":   "Pluma",
			"last_name":    "",
		}},
	})
	if err != nil {
		return classify(err, 0)
	}
	var imported struct {
		UserIDs []int64 `json:"user_ids"`
	}
	if err := json.Unmarshal(raw, &imported); err != nil || len(imported.UserIDs) != 1 {
		return sender.Delivery{Result: sender.Unavailable}
	}
	userID := imported.UserIDs[0]
	if userID == 0 {
		// 0 if the contact is not yet a registered user (td_api.tl, importedContacts).
		return sender.Delivery{Result: sender.NoTelegram}
	}

	raw, err = a.call(ctx, map[string]any{"@type": "createPrivateChat", "user_id": userID, "force": false})
	if err != nil {
		return classify(err, userID)
	}
	var chat struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &chat); err != nil {
		return sender.Delivery{Result: sender.Unavailable, UserID: userID}
	}

	raw, err = a.call(ctx, map[string]any{
		"@type":   "sendMessage",
		"chat_id": chat.ID,
		"input_message_content": map[string]any{
			"@type": "inputMessageText",
			"text":  map[string]any{"@type": "formattedText", "text": text},
		},
	})
	if err != nil {
		return classify(err, userID)
	}
	var sent struct {
		ID           int64           `json:"id"`
		SendingState json.RawMessage `json:"sending_state"`
	}
	if err := json.Unmarshal(raw, &sent); err != nil {
		return sender.Delivery{Result: sender.Unavailable, UserID: userID}
	}
	if len(sent.SendingState) == 0 || string(sent.SendingState) == "null" {
		return sender.Delivery{Result: sender.Sent, UserID: userID}
	}
	if err := a.waitSent(ctx, sent.ID); err != nil {
		return classify(err, userID)
	}
	return sender.Delivery{Result: sender.Sent, UserID: userID}
}

// waitSent waits for updateMessageSendSucceeded or Failed of a message.
func (a *Account) waitSent(ctx context.Context, id int64) error {
	a.mu.Lock()
	if err, ok := a.early[id]; ok {
		delete(a.early, id)
		a.mu.Unlock()
		return err
	}
	ch := make(chan error, 1)
	a.sends[id] = ch
	a.mu.Unlock()
	select {
	case err := <-ch:
		return err
	case <-ctx.Done():
		a.mu.Lock()
		delete(a.sends, id)
		a.mu.Unlock()
		return ctx.Err()
	}
}

// classify maps a failure of the delivery to its result. Error texts are
// TDLib's and Telegram's and may change, so only the codes and these
// well-known names decide, and whatever is unknown is Unavailable: the user
// gets the code from the administrator.
func classify(err error, userID int64) sender.Delivery {
	d := sender.Delivery{Result: sender.Unavailable, UserID: userID}
	var te *Error
	if !errors.As(err, &te) {
		return d
	}
	if wait := te.RetryAfter(); wait > 0 {
		d.Result = sender.AccountLimited
		d.RetryAfter = wait
		return d
	}
	switch {
	case te.Code == 401:
		// The session is gone; Session() says so once TDLib reports it.
	case strings.Contains(te.Message, "PEER_FLOOD"):
		d.Result = sender.AccountLimited
		d.Banned = true
	case te.Code == 403, strings.Contains(te.Message, "USER_IS_BLOCKED"),
		strings.Contains(te.Message, "USER_PRIVACY_RESTRICTED"),
		strings.Contains(te.Message, "YOU_BLOCKED_USER"):
		d.Result = sender.Undeliverable
	}
	return d
}

// RemoveContacts removes imported contacts.
func (a *Account) RemoveContacts(ctx context.Context, userIDs []int64) error {
	_, err := a.call(ctx, map[string]any{"@type": "removeContacts", "user_ids": userIDs})
	return err
}
