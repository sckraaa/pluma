package logincode

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeSender struct {
	result  SendResult
	account string
	err     error
	phones  []string
	codes   []string
}

func (f *fakeSender) Send(_ context.Context, phone, code string) (SendReply, error) {
	f.phones = append(f.phones, phone)
	f.codes = append(f.codes, code)
	return SendReply{Result: f.result, Account: f.account}, f.err
}

// delivered is DeliverByTelegram's ok.
func delivered(ctx context.Context, s *Service) bool {
	_, ok := s.DeliverByTelegram(ctx, sendPhone, sendHash, sendIP)
	return ok
}

const (
	sendPhone = "79991234567"
	sendHash  = "hash-of-the-request"
	sendIP    = "203.0.113.9"
)

func issueRequest(t *testing.T, s *Service, withCode bool) string {
	t.Helper()
	code, err := s.Issue(context.Background(), sendPhone, sendHash, sendIP, withCode)
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func TestDeliverByTelegramStoresTheCodeItWrote(t *testing.T) {
	ctx := context.Background()
	for _, result := range []SendResult{SendSent, SendQueued} {
		t.Run(string(result), func(t *testing.T) {
			s, _, _ := testService(t, sendPhone)
			f := &fakeSender{result: result}
			s.SetSender(f)
			issueRequest(t, s, false)
			if !delivered(ctx, s) {
				t.Fatal("not delivered")
			}
			if len(f.codes) != 1 || !wellFormed(f.codes[0]) || f.phones[0] != sendPhone {
				t.Fatalf("sent %v to %v", f.codes, f.phones)
			}
			if err := s.Verify(ctx, sendPhone, sendHash, f.codes[0], sendIP); err != nil {
				t.Fatalf("the code written does not work: %v", err)
			}
		})
	}
}

func TestDeliverByTelegramRefusedLeavesNoCode(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name string
		f    *fakeSender
	}{
		{"no account", &fakeSender{result: SendNoTelegram}},
		{"recipient limit", &fakeSender{result: SendRecipientLimited}},
		{"account limited", &fakeSender{result: SendAccountLimited}},
		{"unavailable", &fakeSender{result: SendUnavailable}},
		{"transport error", &fakeSender{err: errors.New("connection refused")}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, _, _ := testService(t, sendPhone)
			s.SetSender(tt.f)
			issueRequest(t, s, false)
			if delivered(ctx, s) {
				t.Fatal("reported as delivered")
			}
			list, err := s.Pending(ctx, sendPhone)
			if err != nil {
				t.Fatal(err)
			}
			if len(list) != 1 || list[0].HasCode {
				t.Fatalf("pending %+v: an undelivered code was stored", list)
			}
			if _, err := s.Reissue(ctx, sendPhone, ""); err != nil {
				t.Fatalf("the administrator path is gone: %v", err)
			}
		})
	}
}

func TestDeliverByTelegramSkipsARequestOutOfChecks(t *testing.T) {
	ctx := context.Background()
	s, _, _ := testService(t, sendPhone)
	f := &fakeSender{result: SendSent}
	s.SetSender(f)
	code := issueRequest(t, s, true)
	wrong := "111111"
	if code == wrong {
		wrong = "222222"
	}
	for range MaxAttempts {
		if err := s.Verify(ctx, sendPhone, sendHash, wrong, sendIP); err == nil {
			t.Fatal("a wrong code was accepted")
		}
	}
	if delivered(ctx, s) {
		t.Fatal("reported as delivered")
	}
	if len(f.codes) != 0 {
		t.Fatalf("the account wrote %d codes", len(f.codes))
	}
}

func TestDeliverByTelegramRefusedKeepsTheEarlierCode(t *testing.T) {
	ctx := context.Background()
	s, _, _ := testService(t, sendPhone)
	s.SetSender(&fakeSender{result: SendUnavailable})
	code := issueRequest(t, s, true)
	if delivered(ctx, s) {
		t.Fatal("reported as delivered")
	}
	if err := s.Verify(ctx, sendPhone, sendHash, code, sendIP); err != nil {
		t.Fatalf("the in-app code stopped working: %v", err)
	}
}

func TestDeliverByTelegramNeverForTheReviewPhone(t *testing.T) {
	mr, store := newTestKV(t)
	_ = mr
	s, err := NewService(Config{HmacKey: testKey, ReviewPhone: sendPhone, ReviewCode: "123456"}, fakeAllowlist{sendPhone: true}, store)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSender{result: SendSent}
	s.SetSender(f)
	if delivered(context.Background(), s) {
		t.Fatal("delivered")
	}
	if len(f.codes) != 0 {
		t.Fatal("the fixed code went to the sender")
	}
}

func TestDeliverByTelegramWithoutSender(t *testing.T) {
	s, _, _ := testService(t, sendPhone)
	if delivered(context.Background(), s) {
		t.Fatal("delivered with no sender")
	}
}

func TestDeliverByTelegramPerAddressLimit(t *testing.T) {
	ctx := context.Background()
	s, _, _ := testService(t, sendPhone)
	f := &fakeSender{result: SendSent}
	s.SetSender(f)
	issueRequest(t, s, false)
	for range TelegramCodePerIP.Limit {
		if !delivered(ctx, s) {
			t.Fatal("refused under the limit")
		}
	}
	before := len(f.codes)
	if delivered(ctx, s) {
		t.Fatal("delivered over the limit")
	}
	if len(f.codes) != before {
		t.Fatal("the sender was asked over the limit")
	}
}

func TestHTTPSenderContract(t *testing.T) {
	var got struct {
		Phone  string `json:"phone"`
		Code   string `json:"code"`
		WaitMs int64  `json:"wait_ms"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/send" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+testKey {
			http.Error(w, "bad", http.StatusUnauthorized)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"result":"sent","account":"pluma_codes"}`))
	}))
	defer srv.Close()

	s, err := newHTTPSender(srv.URL+"/", testKey)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := s.Send(context.Background(), sendPhone, "123456")
	if err != nil || reply.Result != SendSent || reply.Account != "pluma_codes" {
		t.Fatalf("%+v, %v", reply, err)
	}
	if got.Phone != sendPhone || got.Code != "123456" || got.WaitMs != senderWait.Milliseconds() {
		t.Fatalf("request %+v", got)
	}
	if senderWait >= 10*time.Second {
		t.Fatalf("senderWait %s holds auth.sendCode too long", senderWait)
	}
}

func TestHTTPSenderErrors(t *testing.T) {
	status := http.StatusInternalServerError
	body := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	s, err := newHTTPSender(srv.URL, testKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Send(context.Background(), sendPhone, "123456"); err == nil || strings.Contains(err.Error(), sendPhone) {
		t.Fatalf("500: %v", err)
	}
	status, body = http.StatusOK, `{}`
	if _, err := s.Send(context.Background(), sendPhone, "123456"); err == nil {
		t.Fatal("no result: no error")
	}
}

func TestSenderConfig(t *testing.T) {
	if s, err := senderFromConfig(Config{}); s != nil || err != nil {
		t.Fatalf("empty: %v, %v", s, err)
	}
	if _, err := senderFromConfig(Config{SenderURL: "http://codes:9378"}); !errors.Is(err, ErrSenderMisconfigured) {
		t.Fatalf("no token: %v", err)
	}
	if s, err := senderFromConfig(Config{SenderToken: testKey}); s != nil || err != nil {
		t.Fatalf("token before the url: %v, %v", s, err)
	}
	if _, err := senderFromConfig(Config{SenderURL: "http://codes:9378", SenderToken: "short"}); err == nil {
		t.Fatal("a short token was accepted")
	}
	if s, err := senderFromConfig(Config{SenderURL: "http://codes:9378", SenderToken: testKey}); s == nil || err != nil {
		t.Fatalf("set: %v, %v", s, err)
	}
}

type fakeSwitch struct {
	open bool
	err  error
}

func (f fakeSwitch) RegistrationOpen(context.Context) (bool, error) { return f.open, f.err }

func TestRegistrationOpenAdmitsAnyPhone(t *testing.T) {
	ctx := context.Background()
	s, _, _ := testService(t)
	if err := s.CheckAllowed(ctx, sendPhone); rpcMessage(err) != "PHONE_NUMBER_INVALID" {
		t.Fatalf("closed, off the list: %v", err)
	}

	s.SetRegistrationSwitch(fakeSwitch{open: true})
	if open, err := s.RegistrationOpen(ctx); !open || err != nil {
		t.Fatalf("switch: %v, %v", open, err)
	}
	if err := s.CheckAllowed(ctx, sendPhone); err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := s.Pending(ctx, sendPhone); err != nil {
		t.Fatalf("Pending when open: %v", err)
	}

	s.SetRegistrationSwitch(fakeSwitch{open: false})
	if err := s.CheckAllowed(ctx, sendPhone); rpcMessage(err) != "PHONE_NUMBER_INVALID" {
		t.Fatalf("back to the list: %v", err)
	}

	s.SetRegistrationSwitch(fakeSwitch{err: errors.New("mysql down")})
	if err := s.CheckAllowed(ctx, sendPhone); rpcMessage(err) != "INTERNAL_SERVER_ERROR" {
		t.Fatalf("switch fails: %v", err)
	}
}

func TestListedPhoneDoesNotAskTheSwitch(t *testing.T) {
	s, _, _ := testService(t, sendPhone)
	s.SetRegistrationSwitch(fakeSwitch{err: errors.New("mysql down")})
	if err := s.CheckAllowed(context.Background(), sendPhone); err != nil {
		t.Fatalf("a listed phone depends on the switch: %v", err)
	}
}

// The username of the account comes back for the login screen's button to
// its chat, only in a form a tg:// link can carry.
func TestDeliverByTelegramNamesTheAccount(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct{ account, want string }{
		{"pluma_codes", "pluma_codes"},
		{"", ""},
		{"bad&name=1", ""},
		{"1digitfirst", ""},
	} {
		s, _, _ := testService(t, sendPhone)
		s.SetSender(&fakeSender{result: SendSent, account: tt.account})
		issueRequest(t, s, false)
		got, ok := s.DeliverByTelegram(ctx, sendPhone, sendHash, sendIP)
		if !ok || got != tt.want {
			t.Errorf("%q: %q, %v", tt.account, got, ok)
		}
	}
}

// The administrator's code and a later code in the person's Telegram live in
// slots of their own: the second does not cancel the first, nor the other
// way round, and the checks are counted once for both.
func TestAdministratorCodeSurvivesTelegramDelivery(t *testing.T) {
	ctx := context.Background()
	s, _, _ := testService(t, sendPhone)
	f := &fakeSender{result: SendSent}
	s.SetSender(f)
	issueRequest(t, s, false)
	admin, err := s.Reissue(ctx, sendPhone, "")
	if err != nil {
		t.Fatal(err)
	}
	if !delivered(ctx, s) {
		t.Fatal("not delivered")
	}
	if err := s.Verify(ctx, sendPhone, sendHash, admin.Code, sendIP); err != nil {
		t.Fatalf("the administrator's code stopped working: %v", err)
	}
}

func TestTelegramCodeSurvivesAdministratorCode(t *testing.T) {
	ctx := context.Background()
	s, _, _ := testService(t, sendPhone)
	f := &fakeSender{result: SendSent}
	s.SetSender(f)
	issueRequest(t, s, false)
	if !delivered(ctx, s) {
		t.Fatal("not delivered")
	}
	if _, err := s.Reissue(ctx, sendPhone, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Verify(ctx, sendPhone, sendHash, f.codes[0], sendIP); err != nil {
		t.Fatalf("the code in Telegram stopped working: %v", err)
	}
}

func TestTwoSlotsShareTheChecks(t *testing.T) {
	ctx := context.Background()
	s, _, _ := testService(t, sendPhone)
	f := &fakeSender{result: SendSent}
	s.SetSender(f)
	issueRequest(t, s, false)
	admin, err := s.Reissue(ctx, sendPhone, "")
	if err != nil {
		t.Fatal(err)
	}
	if !delivered(ctx, s) {
		t.Fatal("not delivered")
	}
	wrong := "000000"
	for wrong == admin.Code || wrong == f.codes[0] {
		wrong = "999999"
	}
	for range MaxAttempts {
		if err := s.Verify(ctx, sendPhone, sendHash, wrong, sendIP); err == nil {
			t.Fatal("a wrong code was accepted")
		}
	}
	if err := s.Verify(ctx, sendPhone, sendHash, admin.Code, sendIP); err == nil {
		t.Fatal("a check beyond MaxAttempts passed")
	}
}
