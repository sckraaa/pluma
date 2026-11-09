package account

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/app/ops/codes/internal/sender"
)

// fakeLib plays the TDLib side: handle answers each request with events.
type fakeLib struct {
	mu       sync.Mutex
	nextID   int
	requests []map[string]any
	events   chan string
	handle   func(req map[string]any) []string
	executed []string
}

func (f *fakeLib) CreateClient() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	return f.nextID
}

func (f *fakeLib) Execute(request string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.executed = append(f.executed, request)
	return ""
}

func (f *fakeLib) Send(client int, request string) {
	var req map[string]any
	if err := json.Unmarshal([]byte(request), &req); err != nil {
		panic(err)
	}
	f.mu.Lock()
	f.requests = append(f.requests, req)
	f.mu.Unlock()
	for _, e := range f.handle(req) {
		f.events <- stamp(e, client)
	}
}

func (f *fakeLib) Receive(timeout time.Duration) string {
	select {
	case e := <-f.events:
		return e
	case <-time.After(5 * time.Millisecond):
		return ""
	}
}

func stamp(event string, client int) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(event), &m); err != nil {
		panic(err)
	}
	m["@client_id"] = client
	data, _ := json.Marshal(m)
	return string(data)
}

func (f *fakeLib) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, r := range f.requests {
		out = append(out, r["@type"].(string))
	}
	return out
}

func answer(req map[string]any, body string) string {
	extra, _ := json.Marshal(req["@extra"])
	return strings.Replace(body, "{", `{"@extra":`+string(extra)+`,`, 1)
}

func authState(state string) string {
	return `{"@type":"updateAuthorizationState","authorization_state":` + state + `}`
}

func stateOf(typ string) string { return authState(`{"@type":"` + typ + `"}`) }

// readyLib is a client that authorizes at once; the rest of its behaviour is rest.
func readyLib(rest func(req map[string]any) []string) *fakeLib {
	return &fakeLib{
		events: make(chan string, 64),
		handle: func(req map[string]any) []string {
			switch req["@type"] {
			case "getOption":
				return []string{stateOf("authorizationStateWaitTdlibParameters")}
			case "setTdlibParameters":
				return []string{answer(req, `{"@type":"ok"}`), stateOf("authorizationStateReady")}
			case "close":
				return []string{stateOf("authorizationStateClosed")}
			}
			return rest(req)
		},
	}
}

func run(t *testing.T, f *fakeLib, login *Login) (*Account, func()) {
	t.Helper()
	a := New(f, Params{APIID: 7, APIHash: "testhash", DatabaseDir: "/data/td", EncryptionKey: []byte("key-material")}, login)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.Run(ctx)
	}()
	stop := func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Run did not stop")
		}
	}
	t.Cleanup(stop)
	return a, stop
}

func waitSession(t *testing.T, a *Account, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if a.Session() == want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("session %s, want %s", a.Session(), want)
}

func delivery(rest func(req map[string]any) []string) (*Account, *fakeLib, func(*testing.T) sender.Delivery) {
	f := readyLib(rest)
	var a *Account
	return a, f, func(t *testing.T) sender.Delivery {
		acc, _ := run(t, f, nil)
		waitSession(t, acc, sender.SessionReady)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return acc.Deliver(ctx, "79991234567", "the text")
	}
}

func TestDeliverSendsThroughContactAndChat(t *testing.T) {
	_, f, deliver := delivery(func(req map[string]any) []string {
		switch req["@type"] {
		case "importContacts":
			c := req["contacts"].([]any)[0].(map[string]any)
			if c["phone_number"] != "+79991234567" || c["first_name"] != "Pluma" {
				t.Errorf("contact %v", c)
			}
			return []string{answer(req, `{"@type":"importedContacts","user_ids":[77],"importer_count":[0]}`)}
		case "createPrivateChat":
			if req["user_id"] != float64(77) {
				t.Errorf("user %v", req["user_id"])
			}
			return []string{answer(req, `{"@type":"chat","id":770}`)}
		case "sendMessage":
			if req["chat_id"] != float64(770) {
				t.Errorf("chat %v", req["chat_id"])
			}
			content := req["input_message_content"].(map[string]any)
			text := content["text"].(map[string]any)["text"]
			if text != "the text" {
				t.Errorf("text %v", text)
			}
			return []string{
				answer(req, `{"@type":"message","id":100,"sending_state":{"@type":"messageSendingStatePending"}}`),
				`{"@type":"updateMessageSendSucceeded","old_message_id":100,"message":{"@type":"message","id":101}}`,
			}
		}
		return nil
	})
	got := deliver(t)
	if got.Result != sender.Sent || got.UserID != 77 {
		t.Fatalf("delivery %+v", got)
	}
	if want := []string{"getOption", "setTdlibParameters", "importContacts", "createPrivateChat", "sendMessage"}; strings.Join(f.methods(), ",") != strings.Join(want, ",") {
		t.Fatalf("methods %v", f.methods())
	}
}

func TestDeliverNoAccount(t *testing.T) {
	_, _, deliver := delivery(func(req map[string]any) []string {
		return []string{answer(req, `{"@type":"importedContacts","user_ids":[0],"importer_count":[0]}`)}
	})
	if got := deliver(t); got.Result != sender.NoTelegram || got.UserID != 0 {
		t.Fatalf("delivery %+v", got)
	}
}

func TestDeliverFloodWaitOfImport(t *testing.T) {
	_, _, deliver := delivery(func(req map[string]any) []string {
		return []string{answer(req, `{"@type":"error","code":429,"message":"Too Many Requests: retry after 3600"}`)}
	})
	got := deliver(t)
	if got.Result != sender.AccountLimited || got.RetryAfter != time.Hour || got.UserID != 0 {
		t.Fatalf("delivery %+v", got)
	}
}

func TestDeliverFailedUpdate(t *testing.T) {
	_, _, deliver := delivery(func(req map[string]any) []string {
		switch req["@type"] {
		case "importContacts":
			return []string{answer(req, `{"@type":"importedContacts","user_ids":[77],"importer_count":[0]}`)}
		case "createPrivateChat":
			return []string{answer(req, `{"@type":"chat","id":770}`)}
		case "sendMessage":
			return []string{
				answer(req, `{"@type":"message","id":100,"sending_state":{"@type":"messageSendingStatePending"}}`),
				`{"@type":"updateMessageSendFailed","old_message_id":100,"error":{"@type":"error","code":400,"message":"PEER_FLOOD"}}`,
			}
		}
		return nil
	})
	got := deliver(t)
	if got.Result != sender.AccountLimited || !got.Banned || got.UserID != 77 {
		t.Fatalf("delivery %+v", got)
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		err    Error
		result sender.Result
		banned bool
		retry  time.Duration
	}{
		{Error{429, "Too Many Requests: retry after 30"}, sender.AccountLimited, false, 30 * time.Second},
		{Error{429, "something else"}, sender.Unavailable, false, 0},
		{Error{400, "PEER_FLOOD"}, sender.AccountLimited, true, 0},
		{Error{403, "CHAT_WRITE_FORBIDDEN"}, sender.Undeliverable, false, 0},
		{Error{400, "USER_IS_BLOCKED"}, sender.Undeliverable, false, 0},
		{Error{401, "Unauthorized"}, sender.Unavailable, false, 0},
		{Error{500, "internal"}, sender.Unavailable, false, 0},
	}
	for _, tt := range tests {
		e := tt.err
		got := classify(&e, 7)
		if got.Result != tt.result || got.Banned != tt.banned || got.RetryAfter != tt.retry || got.UserID != 7 {
			t.Errorf("%+v: %+v", tt.err, got)
		}
	}
}

func TestTdlibParameters(t *testing.T) {
	f := readyLib(func(map[string]any) []string { return nil })
	a, _ := run(t, f, nil)
	waitSession(t, a, sender.SessionReady)
	f.mu.Lock()
	defer f.mu.Unlock()
	var p map[string]any
	for _, r := range f.requests {
		if r["@type"] == "setTdlibParameters" {
			p = r
		}
	}
	if p == nil {
		t.Fatal("no setTdlibParameters")
	}
	if p["api_id"] != float64(7) || p["api_hash"] != "testhash" || p["database_directory"] != "/data/td" {
		t.Fatalf("parameters %v", p)
	}
	for _, name := range []string{"use_message_database", "use_chat_info_database", "use_file_database"} {
		if p[name] != false {
			t.Errorf("%s = %v", name, p[name])
		}
	}
	if p["database_encryption_key"] != "a2V5LW1hdGVyaWFs" {
		t.Errorf("key %v", p["database_encryption_key"])
	}
	if !strings.Contains(strings.Join(f.executed, " "), "logStreamEmpty") {
		t.Errorf("TDLib's own log is not switched off: %v", f.executed)
	}
}

func TestSessionGoneIsReported(t *testing.T) {
	var clients int
	var f *fakeLib
	f = &fakeLib{events: make(chan string, 64), handle: func(req map[string]any) []string {
		switch req["@type"] {
		case "getOption":
			clients++
			if clients == 1 {
				return []string{stateOf("authorizationStateWaitTdlibParameters")}
			}
			return []string{stateOf("authorizationStateWaitTdlibParameters")}
		case "setTdlibParameters":
			if clients == 1 {
				return []string{answer(req, `{"@type":"ok"}`), stateOf("authorizationStateReady")}
			}
			return []string{answer(req, `{"@type":"ok"}`), stateOf("authorizationStateWaitPhoneNumber")}
		case "close":
			return []string{stateOf("authorizationStateClosed")}
		}
		return nil
	}}
	a, _ := run(t, f, nil)
	waitSession(t, a, sender.SessionReady)

	// Telegram ended the session: TDLib logs out, closes and is created anew.
	f.events <- stamp(stateOf("authorizationStateLoggingOut"), 1)
	waitSession(t, a, sender.SessionNoSession)
	f.events <- stamp(stateOf("authorizationStateClosing"), 1)
	f.events <- stamp(stateOf("authorizationStateClosed"), 1)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		n := f.nextID
		f.mu.Unlock()
		if n == 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if f.nextID != 2 {
		t.Fatalf("client not recreated: %d", f.nextID)
	}
	waitSession(t, a, sender.SessionNoSession)

	// The server does not ask for a QR code by itself: that is the operator's.
	for _, m := range f.methods() {
		if m == "requestQrCodeAuthentication" {
			t.Fatal("the service asked for a QR code")
		}
	}
}

func TestQRLoginWithPassword(t *testing.T) {
	var links []string
	var mu sync.Mutex
	done := make(chan error, 1)
	f := &fakeLib{events: make(chan string, 64)}
	f.handle = func(req map[string]any) []string {
		switch req["@type"] {
		case "getOption":
			return []string{stateOf("authorizationStateWaitTdlibParameters")}
		case "setTdlibParameters":
			return []string{answer(req, `{"@type":"ok"}`), stateOf("authorizationStateWaitPhoneNumber")}
		case "requestQrCodeAuthentication":
			return []string{answer(req, `{"@type":"ok"}`), authState(`{"@type":"authorizationStateWaitOtherDeviceConfirmation","link":"tg://login?token=one"}`)}
		case "checkAuthenticationPassword":
			if req["password"] != "right" {
				return []string{answer(req, `{"@type":"error","code":400,"message":"PASSWORD_HASH_INVALID"}`)}
			}
			return []string{answer(req, `{"@type":"ok"}`), stateOf("authorizationStateReady")}
		case "close":
			return []string{stateOf("authorizationStateClosed")}
		}
		return nil
	}
	tries := 0
	a, stop := run(t, f, &Login{
		ShowQR: func(link string) {
			mu.Lock()
			defer mu.Unlock()
			links = append(links, link)
			if len(links) == 1 {
				go func() {
					f.events <- stamp(authState(`{"@type":"authorizationStateWaitPassword","password_hint":"h"}`), 1)
				}()
			}
		},
		Password: func(hint string) (string, error) {
			tries++
			if tries == 1 {
				return "wrong", nil
			}
			return "right", nil
		},
		Done: func(err error) { done <- err },
	})
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("login did not finish")
	}
	waitSession(t, a, sender.SessionReady)
	stop()
	mu.Lock()
	defer mu.Unlock()
	if len(links) != 1 || links[0] != "tg://login?token=one" || tries != 2 {
		t.Fatalf("links %q, tries %d", links, tries)
	}
}
