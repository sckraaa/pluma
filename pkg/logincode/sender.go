package logincode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/metric"
)

// SendResult is what the code sender (app/ops/codes, task 073, ADR 0042) did
// with a code. The values are its wire format.
type SendResult string

const (
	SendSent             SendResult = "sent"
	SendQueued           SendResult = "queued"
	SendNoTelegram       SendResult = "no_telegram"
	SendUndeliverable    SendResult = "undeliverable"
	SendRecipientLimited SendResult = "recipient_limited"
	SendAccountLimited   SendResult = "account_limited"
	SendBusy             SendResult = "busy"
	SendUnavailable      SendResult = "unavailable"
	SendExpired          SendResult = "expired"
)

// Accepted reports whether the code is on its way: sent, or waiting its turn.
func (r SendResult) Accepted() bool { return r == SendSent || r == SendQueued }

// Sender writes a login code to the person's own Telegram account. The
// failure of a Sender is never the login's failure: the code then comes from
// the administrator, as without a sender.
type Sender interface {
	Send(ctx context.Context, phone, code string) (SendResult, error)
}

// senderWait is how long the sender may hold auth.sendCode for the real
// result, in the usual case that the queue is empty. Longer and the app
// shows a spinner for no reason; the sender keeps its code queued.
const senderWait = 6 * time.Second

// MinSenderTokenLength is the shortest accepted Config.SenderToken, in bytes.
const MinSenderTokenLength = 32

var metricSender = metric.NewCounterVec(&metric.CounterVecOpts{
	Namespace: "login_code",
	Name:      "sender_total",
	Help:      "Codes handed to the Telegram account service, by result.",
	Labels:    []string{"result"},
})

// httpSender is the client of POST /v1/send of app/ops/codes.
type httpSender struct {
	url, token string
	client     *http.Client
}

// newHTTPSender returns a Sender at url, the base of the service (http://codes:9378).
func newHTTPSender(url, token string) (*httpSender, error) {
	if len(token) < MinSenderTokenLength {
		return nil, fmt.Errorf("logincode: LOGIN_CODES_SENDER_TOKEN must be at least %d characters (docs/ops/login-codes.md)", MinSenderTokenLength)
	}
	return &httpSender{
		url:    strings.TrimRight(url, "/") + "/v1/send",
		token:  token,
		client: &http.Client{Timeout: senderWait + 3*time.Second},
	}, nil
}

func (s *httpSender) Send(ctx context.Context, phone, code string) (SendResult, error) {
	body, err := json.Marshal(struct {
		Phone  string `json:"phone"`
		Code   string `json:"code"`
		WaitMs int64  `json:"wait_ms"`
	}{phone, code, senderWait.Milliseconds()})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.token)
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("sender answered %d", resp.StatusCode)
	}
	var out struct {
		Result SendResult `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1024)).Decode(&out); err != nil {
		return "", fmt.Errorf("sender answer: %w", err)
	}
	if out.Result == "" {
		return "", errors.New("sender answer without a result")
	}
	return out.Result, nil
}

// DeliverByTelegram tries to write a new code for the request phoneCodeHash
// of phone to the person's Telegram account, and reports whether the code is
// on its way.
//
// The code is stored only after the sender took it, so a request that the
// sender refuses stays exactly as it was: an earlier code still works, and
// the administrator can issue one (Reissue). An undelivered code never
// waits to be guessed. Failures are logged and counted, never returned: the
// administrator path remains.
func (s *Service) DeliverByTelegram(ctx context.Context, phone, phoneCodeHash, clientIp string) bool {
	if s.sender == nil || (s.reviewPhone != "" && phone == s.reviewPhone) {
		return false
	}
	// Bounds what one address can make the account do when registration is
	// open: the account's own limits are per phone and in total.
	if ip := ipSubject(clientIp); ip != "" {
		if over, _ := s.limiter.Over(ctx, TelegramCodePerIP, ip); over {
			metricSender.Inc("limited_ip")
			logx.WithContext(ctx).Infof("login code: telegram delivery over the per-address limit")
			return false
		}
	}
	code, err := generate(s.random)
	if err != nil {
		logx.WithContext(ctx).Errorf("login code: %v", err)
		return false
	}
	result, err := s.sender.Send(ctx, phone, code)
	if err != nil {
		metricSender.Inc("error")
		logx.WithContext(ctx).Errorf("login code: sender: %v", err)
		return false
	}
	metricSender.Inc(string(result))
	if !result.Accepted() {
		logx.WithContext(ctx).Infof("login code: telegram delivery refused: %s", result)
		return false
	}
	ok, err := s.store.replace(ctx, phoneCodeHash, s.hasher.Sum(phoneCodeHash, phone, code), s.now().Add(CodeTTL).Unix())
	if err != nil || !ok {
		logx.WithContext(ctx).Errorf("login code store: replace after delivery: ok=%v err=%v", ok, err)
		return false
	}
	return true
}

// ErrSenderMisconfigured is returned for a URL without a token or a token without a URL.
var ErrSenderMisconfigured = errors.New("logincode: LOGIN_CODES_SENDER_URL and LOGIN_CODES_SENDER_TOKEN must be set together (docs/ops/login-codes.md)")

// senderFromConfig returns the Sender of the config, nil if none is set.
func senderFromConfig(c Config) (Sender, error) {
	if c.SenderURL == "" && c.SenderToken == "" {
		return nil, nil
	}
	if c.SenderURL == "" || c.SenderToken == "" {
		return nil, ErrSenderMisconfigured
	}
	return newHTTPSender(c.SenderURL, c.SenderToken)
}
