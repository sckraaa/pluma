package logincode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/metric"
)

// SendResult is what the code sender (app/ops/codes, task 073, ADR 0044) did
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

// SendReply is the sender's answer about one code.
type SendReply struct {
	Result SendResult
	// Account is the username of the Telegram account that writes the
	// code, given with an accepted code; "" if it has none.
	Account string
}

// Sender writes a login code to the person's own Telegram account. The
// failure of a Sender is never the login's failure: the code then comes from
// the administrator, as without a sender.
type Sender interface {
	Send(ctx context.Context, phone, code string) (SendReply, error)
}

// telegramUsername is the form of a Telegram username (5 to 32 letters,
// digits and underscores; 4 for collectible ones) that the login screens
// may put into tg://resolve?domain=.
var telegramUsername = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{3,31}$`)

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

func (s *httpSender) Send(ctx context.Context, phone, code string) (SendReply, error) {
	body, err := json.Marshal(struct {
		Phone  string `json:"phone"`
		Code   string `json:"code"`
		WaitMs int64  `json:"wait_ms"`
	}{phone, code, senderWait.Milliseconds()})
	if err != nil {
		return SendReply{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return SendReply{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.token)
	resp, err := s.client.Do(req)
	if err != nil {
		return SendReply{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return SendReply{}, fmt.Errorf("sender answered %d", resp.StatusCode)
	}
	var out struct {
		Result  SendResult `json:"result"`
		Account string     `json:"account"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1024)).Decode(&out); err != nil {
		return SendReply{}, fmt.Errorf("sender answer: %w", err)
	}
	if out.Result == "" {
		return SendReply{}, errors.New("sender answer without a result")
	}
	return SendReply{Result: out.Result, Account: out.Account}, nil
}

// DeliverByTelegram tries to write a new code for the request phoneCodeHash
// of phone to the person's Telegram account, and reports whether the code is
// on its way. account is the username of the account that writes it, ""
// if it has none (or not of a form a tg:// link can carry).
//
// The code is stored only after the sender took it, so a request that the
// sender refuses stays exactly as it was: an earlier code still works, and
// the administrator can issue one (Reissue). An undelivered code never
// waits to be guessed. Failures are logged and counted, never returned: the
// administrator path remains.
func (s *Service) DeliverByTelegram(ctx context.Context, phone, phoneCodeHash, clientIp string) (account string, ok bool) {
	if !s.TelegramAvailable(phone) {
		return "", false
	}
	// A request already over (out of checks, cancelled) takes no code, and
	// the account's budget is not spent on one.
	if _, ok, err := s.store.get(ctx, phoneCodeHash); err != nil || !ok {
		if err != nil {
			logx.WithContext(ctx).Errorf("login code store: %v", err)
		}
		return "", false
	}
	// Bounds what one address can make the account do when registration is
	// open: the account's own limits are per phone and in total.
	if ip := ipSubject(clientIp); ip != "" {
		if over, _ := s.limiter.Over(ctx, TelegramCodePerIP, ip); over {
			metricSender.Inc("limited_ip")
			logx.WithContext(ctx).Infof("login code: telegram delivery over the per-address limit")
			return "", false
		}
	}
	code, err := generate(s.random)
	if err != nil {
		logx.WithContext(ctx).Errorf("login code: %v", err)
		return "", false
	}
	reply, err := s.sender.Send(ctx, phone, code)
	if err != nil {
		metricSender.Inc("error")
		logx.WithContext(ctx).Errorf("login code: sender: %v", err)
		return "", false
	}
	metricSender.Inc(string(reply.Result))
	if !reply.Result.Accepted() {
		logx.WithContext(ctx).Infof("login code: telegram delivery refused: %s", reply.Result)
		return "", false
	}
	stored, err := s.store.replace(ctx, phoneCodeHash, s.hasher.Sum(phoneCodeHash, phone, code), s.now().Add(CodeTTL).Unix())
	if err != nil || !stored {
		logx.WithContext(ctx).Errorf("login code store: replace after delivery: ok=%v err=%v", stored, err)
		return "", false
	}
	if telegramUsername.MatchString(reply.Account) {
		account = reply.Account
	}
	return account, true
}

// TelegramAvailable reports whether codes of phone can go to the person's
// Telegram at all: a sender is deployed, and the phone is not the review
// phone, whose code is fixed.
func (s *Service) TelegramAvailable(phone string) bool {
	return s.sender != nil && (s.reviewPhone == "" || phone != s.reviewPhone)
}

// ErrSenderMisconfigured is returned for a URL without a token.
var ErrSenderMisconfigured = errors.New("logincode: LOGIN_CODES_SENDER_URL needs LOGIN_CODES_SENDER_TOKEN (docs/ops/login-codes.md)")

// senderFromConfig returns the Sender of the config, nil if no URL is set. A
// token without a URL is normal: the service reads the same variable, and
// bff is pointed at it only after its first login (docs/ops/login-codes.md).
func senderFromConfig(c Config) (Sender, error) {
	if c.SenderURL == "" {
		return nil, nil
	}
	if c.SenderToken == "" {
		return nil, ErrSenderMisconfigured
	}
	return newHTTPSender(c.SenderURL, c.SenderToken)
}
