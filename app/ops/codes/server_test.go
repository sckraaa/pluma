package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/app/ops/codes/internal/sender"
	"github.com/teamgram/teamgram-server/pkg/logincode"
)

const apiToken = "0123456789abcdef0123456789abcdef"

type fakeQueue struct {
	phone, code string
	wait        time.Duration
	result      sender.Result
}

func (f *fakeQueue) Submit(_ context.Context, phone, code string, wait time.Duration) sender.Result {
	f.phone, f.code, f.wait = phone, code, wait
	return f.result
}

func (f *fakeQueue) Account() string { return "pluma_codes" }

func (f *fakeQueue) State() sender.State {
	return sender.State{Session: sender.SessionNoSession, QueueLength: 2}
}

func request(h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestSendRequiresToken(t *testing.T) {
	h := newHandler(&fakeQueue{}, apiToken)
	if w := request(h, "POST", "/v1/send", "", `{}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", w.Code)
	}
	if w := request(h, "POST", "/v1/send", "wrong", `{}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", w.Code)
	}
	if w := request(h, "GET", "/v1/state", "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("state without a token: %d", w.Code)
	}
}

func TestSendValidates(t *testing.T) {
	h := newHandler(&fakeQueue{}, apiToken)
	for _, body := range []string{
		`not json`,
		`{"phone":"+7999","code":"123456"}`,
		`{"phone":"79991234567","code":"abc"}`,
		`{"phone":"79991234567","code":"123456","extra":1}`,
	} {
		if w := request(h, "POST", "/v1/send", apiToken, body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", body, w.Code)
		}
	}
}

func TestSendAndState(t *testing.T) {
	q := &fakeQueue{result: sender.Sent}
	h := newHandler(q, apiToken)
	w := request(h, "POST", "/v1/send", apiToken, `{"phone":"79991234567","code":"123456","wait_ms":6000}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var got sendResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.Result != sender.Sent {
		t.Fatalf("response %q, %v", w.Body.String(), err)
	}
	if q.phone != "79991234567" || q.code != "123456" || q.wait != 6*time.Second {
		t.Fatalf("queued %+v", q)
	}

	if w := request(h, "POST", "/v1/send", apiToken, `{"phone":"79991234567","code":"123456","wait_ms":99999}`); w.Code != 200 || q.wait != maxWait {
		t.Fatalf("wait not capped: %d, %s", w.Code, q.wait)
	}

	w = request(h, "GET", "/v1/state", apiToken, "")
	var st sender.State
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil || st.Session != sender.SessionNoSession || st.QueueLength != 2 {
		t.Fatalf("state %q, %v", w.Body.String(), err)
	}
}

// The username of the account goes with a code on its way only: the app
// opens that chat to read the code.
func TestSendNamesTheAccountOfADeliveredCode(t *testing.T) {
	for _, tt := range []struct {
		result sender.Result
		want   string
	}{
		{sender.Sent, "pluma_codes"},
		{sender.Queued, "pluma_codes"},
		{sender.NoTelegram, ""},
		{sender.Unavailable, ""},
	} {
		h := newHandler(&fakeQueue{result: tt.result}, apiToken)
		w := request(h, "POST", "/v1/send", apiToken, `{"phone":"79991234567","code":"123456"}`)
		var got sendResponse
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.Account != tt.want {
			t.Errorf("%s: %q, %v", tt.result, w.Body.String(), err)
		}
	}
}

func TestHealthAndMetricsNeedNoToken(t *testing.T) {
	h := newHandler(&fakeQueue{}, apiToken)
	if w := request(h, "GET", "/healthz", "", ""); w.Code != http.StatusOK {
		t.Fatalf("healthz %d", w.Code)
	}
	if w := request(h, "GET", "/metrics", "", ""); w.Code != http.StatusOK {
		t.Fatalf("metrics %d", w.Code)
	}
}

// TestWireResultsMatchBff keeps the results of this service and what bff
// reads from it the same strings.
func TestWireResultsMatchBff(t *testing.T) {
	pairs := map[sender.Result]logincode.SendResult{
		sender.Sent:             logincode.SendSent,
		sender.Queued:           logincode.SendQueued,
		sender.NoTelegram:       logincode.SendNoTelegram,
		sender.Undeliverable:    logincode.SendUndeliverable,
		sender.RecipientLimited: logincode.SendRecipientLimited,
		sender.AccountLimited:   logincode.SendAccountLimited,
		sender.Busy:             logincode.SendBusy,
		sender.Unavailable:      logincode.SendUnavailable,
		sender.Expired:          logincode.SendExpired,
	}
	for s, b := range pairs {
		if string(s) != string(b) {
			t.Errorf("%q != %q", s, b)
		}
	}
	if !logincode.SendSent.Accepted() || !logincode.SendQueued.Accepted() || logincode.SendNoTelegram.Accepted() {
		t.Error("wrong Accepted")
	}
}

func TestConfigFromEnv(t *testing.T) {
	good := map[string]string{
		"TELEGRAM_API_ID":   "12345",
		"TELEGRAM_API_HASH": "abcd",
		"CODES_API_TOKEN":   apiToken,
		"CODES_DB_KEY":      apiToken,
	}
	get := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if c, err := configFromEnv(get(good)); err != nil || c.apiID != 12345 || c.dataDir != "/data" || c.listen != ":9378" {
		t.Fatalf("%+v, %v", c, err)
	}
	for _, missing := range []string{"TELEGRAM_API_ID", "TELEGRAM_API_HASH", "CODES_API_TOKEN", "CODES_DB_KEY"} {
		bad := map[string]string{}
		for k, v := range good {
			if k != missing {
				bad[k] = v
			}
		}
		if _, err := configFromEnv(get(bad)); err == nil {
			t.Errorf("no error without %s", missing)
		}
	}
}

func TestRenderQR(t *testing.T) {
	out, err := renderQR("tg://login?token=abcdef")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 15 {
		t.Fatalf("%d lines", len(lines))
	}
	width := len([]rune(lines[0]))
	for _, l := range lines {
		if len([]rune(l)) != width {
			t.Fatalf("uneven lines: %d and %d", len([]rune(l)), width)
		}
	}
}
