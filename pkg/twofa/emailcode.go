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

package twofa

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/langpack"
	"github.com/teamgram/teamgram-server/pkg/logincode"
	"github.com/teamgram/teamgram-server/pkg/mail"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/metric"
)

// The codes of the recovery email (ADR 0058, p. 2-3).
const (
	// EmailCodeLength is the number of digits of a code: Android expects 6
	// for both the confirmation and the recovery
	// (TwoStepVerificationSetupActivity.java:122, LoginActivity.java:7387),
	// the other clients take it from EMAIL_UNCONFIRMED_6.
	EmailCodeLength = logincode.Length
	// EmailCodeTTL is how long a code can be used. The emails say "30
	// minutes" in every language (pkg/langpack/mail.json).
	EmailCodeTTL = 30 * time.Minute
	// EmailCodeMaxAttempts is how many wrong codes burn one.
	EmailCodeMaxAttempts = 5

	// emailSendDeadline bounds the attempts to hand one email over.
	emailSendDeadline = 8 * time.Second
)

// emailSendPauses are the pauses before the second and the third attempt to
// hand an email over, taken only after a retryable error.
var emailSendPauses = []time.Duration{500 * time.Millisecond, 1500 * time.Millisecond}

// Purposes of a code, the first field of its HMAC and a label of the
// metrics.
const (
	PurposeSetup    = "setup"    // confirms a new recovery email
	PurposeRecovery = "recovery" // recovers the password
)

// Limits of the emails and of the codes typed in (ADR 0058, p. 3). Sends
// are counted per user and purpose (the minute also per address), per auth key and for the whole server
// in a UTC day; a send the provider did not accept gives back only the
// user's count of the minute, so that it can be sent again at once, while
// the other counts stay: a failing provider must not let one account use up
// the server's day. Codes typed in are counted before the comparison and a
// right one is refunded.
var (
	EmailSendPerUser     = logincode.Rule{Name: "password_email_send", Limit: 1, Window: time.Minute}
	EmailSendPerUserHour = logincode.Rule{Name: "password_email_send_hour", Limit: 5, Window: time.Hour}
	EmailSendPerUserDay  = logincode.Rule{Name: "password_email_send_day", Limit: 10, Window: 24 * time.Hour}
	EmailSendPerAuthKey  = logincode.Rule{Name: "password_email_send_key", Limit: 10, Window: time.Hour}

	EmailCodePerUser    = logincode.Rule{Name: "password_email_code", Limit: 10, Window: time.Hour}
	EmailCodePerUserDay = logincode.Rule{Name: "password_email_code_day", Limit: 30, Window: 24 * time.Hour}
	EmailCodePerAuthKey = logincode.Rule{Name: "password_email_code_key", Limit: 10, Window: time.Hour}

	// Wrong codes of one user in a day. Not a limit: past it each wrong code
	// is logged as an error, the sign of a brute force.
	EmailCodeFailuresAlert = logincode.Rule{Name: "password_email_failures", Limit: 20, Window: 24 * time.Hour}
)

// emailServerDay is the rule of the server's emails in a UTC day; its
// limit is Mail.DailyLimit and its subject the day, so the window (longer
// than the day) only lets the counter expire.
func emailServerDay(limit int64) logincode.Rule {
	return logincode.Rule{Name: "password_email_server_day", Limit: limit, Window: 25 * time.Hour}
}

// Metrics of the recovery email (ADR 0058, p. 12); the alerts on them are in
// teamgramd/deploy/prometheus/server/rules/codes.yml.
var (
	metricEmailSend = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "password_email",
		Name:      "send_total",
		Help:      "Emails with a code of the recovery email, by purpose and result.",
		Labels:    []string{"purpose", "result"},
	})
	metricEmailCode = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "password_email",
		Name:      "code_total",
		Help:      "Codes of the recovery email typed in, by purpose and result.",
		Labels:    []string{"purpose", "result"},
	})
)

// Results of the metrics.
const (
	resultOk        = "ok"
	resultRetryable = "retryable" // the provider failed, and repeats failed
	resultPermanent = "permanent" // the provider refused
	resultLimited   = "limited"   // a limit of ours
	resultInvalid   = "invalid"
	resultExpired   = "expired"
)

// mailState is the mail of a Service; nil when mail is off.
type mailState struct {
	sender     mail.Sender
	hasher     *emailHasher
	dailyLimit int64
}

// EnableMail turns the recovery email on with c (the Mail section of
// bff.yaml), the codes keyed by hmacKey (LOGIN_CODE_HMAC_KEY). A config
// without a key leaves it off (ADR 0058, p. 7); a key without a usable
// From is mail.ErrMailMisconfigured.
func (s *Service) EnableMail(c mail.Config, hmacKey string) error {
	sender, err := mail.New(c)
	if err != nil || sender == nil {
		return err
	}
	return s.SetMailSender(sender, hmacKey, c.DailyLimit)
}

// SetMailSender turns the recovery email on with sender, the codes keyed by
// hmacKey; dailyLimit is the server's emails in a UTC day
// (mail.DefaultDailyLimit if 0).
func (s *Service) SetMailSender(sender mail.Sender, hmacKey string, dailyLimit int64) error {
	if sender == nil {
		return errors.New("twofa: no mail sender")
	}
	if len(hmacKey) < logincode.MinHmacKeyLength {
		return fmt.Errorf("twofa: the HMAC key of the email codes must be at least %d bytes (LOGIN_CODE_HMAC_KEY)", logincode.MinHmacKeyLength)
	}
	if dailyLimit <= 0 {
		dailyLimit = mail.DefaultDailyLimit
	}
	s.mail = &mailState{sender: sender, hasher: &emailHasher{key: []byte(hmacKey)}, dailyLimit: dailyLimit}
	return nil
}

// MailEnabled reports whether the recovery email is on.
func (s *Service) MailEnabled() bool { return s.mail != nil }

// emailHasher computes what Redis keeps instead of a code: HMAC-SHA256
// under LOGIN_CODE_HMAC_KEY over the length-prefixed fields (domain label,
// purpose, user, address, the password's set_at for a recovery, code). A
// code issued before a change of the address or of the password stops
// matching by itself.
type emailHasher struct {
	key []byte
}

func (h *emailHasher) sum(purpose string, userId int64, address string, setAt int64, code string) string {
	m := hmac.New(sha256.New, h.key)
	var n [binary.MaxVarintLen64]byte
	at := ""
	if purpose == PurposeRecovery {
		at = strconv.FormatInt(setAt, 10)
	}
	for _, field := range []string{"pluma-email-code-v1", purpose, strconv.FormatInt(userId, 10), address, at, code} {
		m.Write(n[:binary.PutUvarint(n[:], uint64(len(field)))])
		m.Write([]byte(field))
	}
	return hex.EncodeToString(m.Sum(nil))
}

// Redis layout of the codes (one key per purpose and user, so a new code
// replaces the old one):
//
//	twofa_email_code_<purpose>_<user_id>   hash {hash, code_id, attempts, issued_at}, EmailCodeTTL
const emailCodeKeyPrefix = "twofa_email_code_"

func emailCodeKey(purpose string, userId int64) string {
	return emailCodeKeyPrefix + purpose + "_" + strconv.FormatInt(userId, 10)
}

const (
	// KEYS[1] code; ARGV hash, code_id, issued_at, ttl seconds.
	putEmailCodeScript = `
redis.call('DEL', KEYS[1])
redis.call('HSET', KEYS[1], 'hash', ARGV[1], 'code_id', ARGV[2], 'attempts', 0, 'issued_at', ARGV[3])
redis.call('EXPIRE', KEYS[1], ARGV[4])
return 1`

	// KEYS[1] code; ARGV hash, max attempts, take ('1': delete a matching
	// code). Returns {0, 0} without a code, {1, 0} for a match, {2, n} for
	// the n-th wrong one; the wrong one that reaches the limit deletes the
	// code. One script, so parallel requests cannot both take one code or
	// get past the limit.
	checkEmailCodeScript = `
local h = redis.call('HGET', KEYS[1], 'hash')
if not h then return {0, 0} end
if h == ARGV[1] then
  if ARGV[3] == '1' then redis.call('DEL', KEYS[1]) end
  return {1, 0}
end
local n = redis.call('HINCRBY', KEYS[1], 'attempts', 1)
if n >= tonumber(ARGV[2]) then redis.call('DEL', KEYS[1]) end
return {2, n}`
)

// codeResult is the outcome of a code typed in.
type codeResult int

const (
	codeNone  codeResult = iota // no live code: never sent, expired, burnt or taken
	codeRight                   // it matches
	codeWrong                   // it does not
)

// errSendFailed is a send the provider did not accept: each caller answers
// it in its own way (ADR 0058, p. 4).
var errSendFailed = errors.New("twofa: the email was not sent")

// emailRequest is one email with a code to send.
type emailRequest struct {
	purpose   string
	userId    int64
	authKeyId int64
	address   string
	setAt     int64 // the password's, for a recovery code
	lang      string
}

// sendCode sends a new code of r.purpose to r.address under the limits and
// keeps its HMAC, replacing the previous code, once the provider accepted
// the email. It returns FLOOD_WAIT_X past a limit, errSendFailed if the
// provider did not accept the email, INTERNAL_SERVER_ERROR if Redis fails.
func (s *Service) sendCode(ctx context.Context, r emailRequest) error {
	counts, err := s.countSend(ctx, r)
	if err != nil {
		if !errors.Is(err, mtproto.ErrInternalServerError) {
			metricEmailSend.Inc(r.purpose, resultLimited)
			logx.WithContext(ctx).Infof("twofa: user %d: a %s email is over a limit", r.userId, r.purpose)
		}
		return err
	}

	code, err := logincode.Generate()
	if err != nil {
		s.refund(ctx, counts)
		return errInternal(ctx, "generate an email code", err)
	}
	rnd, err := s.randomBytes(16)
	if err != nil {
		s.refund(ctx, counts)
		return errInternal(ctx, "random", err)
	}
	codeId := hex.EncodeToString(rnd)

	m := composeEmail(r.purpose, r.lang, code)
	m.To = r.address
	m.IdempotencyKey = "pluma/" + r.purpose + "/" + strconv.FormatInt(r.userId, 10) + "/" + codeId
	start := s.now()
	id, err := s.deliver(ctx, m)
	took := s.now().Sub(start)
	if err != nil {
		// Only the minute's count is given back: the provider may have
		// failed for everyone, or taken the email after all.
		s.refund(ctx, counts[:1])
		result, reason := resultRetryable, "unknown"
		var e *mail.Error
		if errors.As(err, &e) {
			reason = e.Code
			if !e.Retryable {
				result = resultPermanent
			}
		}
		metricEmailSend.Inc(r.purpose, result)
		logx.WithContext(ctx).Errorf("twofa: user %d: the %s email %s was not sent (%s, %s) in %v", r.userId, r.purpose, codeId, result, reason, took)
		return errSendFailed
	}
	metricEmailSend.Inc(r.purpose, resultOk)
	logx.WithContext(ctx).Infof("twofa: user %d: the %s email %s was sent as %s in %v", r.userId, r.purpose, codeId, id, took)

	hash := s.mail.hasher.sum(r.purpose, r.userId, r.address, r.setAt, code)
	if _, err = s.state.kv.EvalCtx(ctx, putEmailCodeScript, emailCodeKey(r.purpose, r.userId),
		hash, codeId, s.now().Unix(), int64(EmailCodeTTL/time.Second)); err != nil {
		// The email is out, its code is not usable: "send again".
		return errInternal(ctx, "store the email code of user "+formatKeyId(r.userId), err)
	}
	return nil
}

// addressKey names an address in the keys of the limits without the
// address itself: the first 8 bytes of its SHA-256, lower-cased.
func addressKey(address string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(address)))
	return hex.EncodeToString(sum[:8])
}

// countSend counts one email of r under every limit; past one, the counts
// made are taken back and FLOOD_WAIT_X returned. It returns the counts of
// the user and of the key, the user's count of the minute first; the
// server's count is never given back.
func (s *Service) countSend(ctx context.Context, r emailRequest) ([]counted, error) {
	user := r.purpose + "_" + formatKeyId(r.userId)
	all := []counted{
		// The minute is counted per address: a typo fixed on the code
		// screen gets its email at once, the hour and the day still bound
		// the user.
		{EmailSendPerUser, user + "_" + addressKey(r.address)},
		{EmailSendPerUserHour, user},
		{EmailSendPerUserDay, user},
		{EmailSendPerAuthKey, formatKeyId(r.authKeyId)},
	}
	for i, c := range all {
		if err := s.limiter.Hit(ctx, c.rule, c.subject); err != nil {
			s.refund(ctx, all[:i])
			return nil, err
		}
	}
	now := s.now().UTC()
	if err := s.limiter.Hit(ctx, emailServerDay(s.mail.dailyLimit), now.Format("20060102")); err != nil {
		s.refund(ctx, all)
		if errors.Is(err, mtproto.ErrInternalServerError) {
			return nil, err
		}
		midnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
		logx.WithContext(ctx).Errorf("twofa: the server's emails of the day are used up (%d)", s.mail.dailyLimit)
		return nil, mtproto.NewErrFloodWaitX(int32(max(midnight.Sub(now)/time.Second, 1)))
	}
	return all, nil
}

// deliver hands m to the provider: up to 1+len(emailSendPauses) attempts,
// repeated only after a retryable error and all within emailSendDeadline,
// with one idempotency key, so the provider sends one email at most.
func (s *Service) deliver(ctx context.Context, m mail.Message) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, emailSendDeadline)
	defer cancel()
	for attempt := 0; ; attempt++ {
		id, err := s.mail.sender.Send(ctx, m)
		if err == nil {
			return id, nil
		}
		if !mail.Retryable(err) || attempt == len(emailSendPauses) {
			return "", err
		}
		if err = s.sleep(ctx, emailSendPauses[attempt]); err != nil {
			return "", &mail.Error{Code: "deadline", Retryable: true}
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// checkCode compares code with the live code of purpose of p's user for
// address under the limits of codes typed in; take deletes a matching code.
func (s *Service) checkCode(ctx context.Context, purpose string, p *Password, authKeyId int64, address, code string, take bool) (codeResult, error) {
	user, key := formatKeyId(p.UserId), formatKeyId(authKeyId)
	all := []counted{
		{EmailCodePerUser, user},
		{EmailCodePerUserDay, user},
		{EmailCodePerAuthKey, key},
	}
	for i, c := range all {
		if err := s.limiter.Hit(ctx, c.rule, c.subject); err != nil {
			s.refund(ctx, all[:i])
			if !errors.Is(err, mtproto.ErrInternalServerError) {
				metricEmailCode.Inc(purpose, resultLimited)
			}
			return codeNone, err
		}
	}
	takeArg := "0"
	if take {
		takeArg = "1"
	}
	hash := s.mail.hasher.sum(purpose, p.UserId, address, p.SetAt, code)
	reply, err := s.state.kv.EvalCtx(ctx, checkEmailCodeScript, emailCodeKey(purpose, p.UserId), hash, EmailCodeMaxAttempts, takeArg)
	if err != nil {
		s.refund(ctx, all)
		return codeNone, errInternal(ctx, "the email code of user "+user, err)
	}
	result, attempts, err := parseCheck(reply)
	if err != nil {
		s.refund(ctx, all)
		return codeNone, errInternal(ctx, "the email code of user "+user, err)
	}
	switch result {
	case codeRight:
		s.refund(ctx, all)
		metricEmailCode.Inc(purpose, resultOk)
	case codeWrong:
		metricEmailCode.Inc(purpose, resultInvalid)
		logx.WithContext(ctx).Infof("twofa: user %d: wrong %s email code (%d of %d)", p.UserId, purpose, attempts, EmailCodeMaxAttempts)
		if over, n := s.limiter.Over(ctx, EmailCodeFailuresAlert, user); over {
			logx.WithContext(ctx).Errorf("twofa: user %d: %d wrong email codes in a day, a possible brute force", p.UserId, n)
		}
	default:
		// Not a guess: nothing could match.
		s.refund(ctx, all)
		metricEmailCode.Inc(purpose, resultExpired)
	}
	return result, nil
}

func parseCheck(reply any) (codeResult, int64, error) {
	a, ok := reply.([]any)
	if !ok || len(a) != 2 {
		return codeNone, 0, fmt.Errorf("unexpected reply %T", reply)
	}
	r, ok1 := a[0].(int64)
	n, ok2 := a[1].(int64)
	if !ok1 || !ok2 || r < 0 || r > 2 {
		return codeNone, 0, fmt.Errorf("unexpected reply %v", a)
	}
	return codeResult(r), n, nil
}

// dropCode forgets the code of purpose of userId. Best effort: it expires.
func (s *Service) dropCode(ctx context.Context, purpose string, userId int64) {
	if _, err := s.state.kv.DelCtx(ctx, emailCodeKey(purpose, userId)); err != nil {
		logx.WithContext(ctx).Errorf("twofa: drop the %s email code of user %d: %v", purpose, userId, err)
	}
}

// composeEmail is the email with code of purpose in lang (ADR 0058, p. 9):
// the texts of pkg/langpack/mail.json, the code on a line of its own, plain
// text and a small HTML part with one table and inline CSS. No phone,
// account name or device.
func composeEmail(purpose, lang, code string) mail.Message {
	subjectKey, bodyKey, tag := "PlumaMailRecoverySubject", "PlumaMailRecoveryBody", "recovery_code"
	if purpose == PurposeSetup {
		subjectKey, bodyKey, tag = "PlumaMailConfirmSubject", "PlumaMailConfirmBody", "email_confirm"
	}
	subject := langpack.MailText(subjectKey, lang)
	body := langpack.MailText(bodyKey, lang)
	validFor := langpack.MailText("PlumaMailValidFor", lang)
	ignore := langpack.MailText("PlumaMailIgnore", lang)

	text := body + "\n\n" + code + "\n\n" + validFor + "\n" + ignore + "\n"

	dir, align := "ltr", "left"
	if langpack.MailRTL(lang) {
		dir, align = "rtl", "right"
	}
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html lang="` + htmlLang(lang) + `" dir="` + dir + `"><head><meta charset="utf-8">`)
	b.WriteString(`<meta name="viewport" content="width=device-width,initial-scale=1"><title>` + html.EscapeString(subject) + `</title></head>`)
	b.WriteString(`<body style="margin:0;padding:16px;background:#f2f3f5">`)
	b.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" dir="` + dir + `" style="max-width:480px;margin:0 auto;background:#ffffff;border:1px solid #e1e3e6;border-radius:12px;` +
		`font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:16px;line-height:1.5;color:#1d2125;text-align:` + align + `">`)
	b.WriteString(`<tr><td style="padding:28px 28px 8px">` + html.EscapeString(body) + `</td></tr>`)
	b.WriteString(`<tr><td dir="ltr" style="padding:8px 28px;font-family:Menlo,Consolas,'Courier New',monospace;font-size:32px;font-weight:700;letter-spacing:6px;text-align:` + align + `">` + html.EscapeString(code) + `</td></tr>`)
	b.WriteString(`<tr><td style="padding:8px 28px 28px;font-size:14px;color:#5b636b">` + html.EscapeString(validFor) + `<br>` + html.EscapeString(ignore) + `</td></tr>`)
	b.WriteString(`</table></body></html>`)

	return mail.Message{Subject: subject, Text: text, HTML: b.String(), Tag: tag}
}

// htmlLang is lang for the lang attribute: a language tag, "en" for
// anything else a client may send.
func htmlLang(lang string) string {
	lang = langpack.CanonicalLangCode(lang)
	if lang == "" || len(lang) > 16 {
		return "en"
	}
	for i := 0; i < len(lang); i++ {
		if c := lang[i]; (c < 'a' || c > 'z') && c != '-' && (c < '0' || c > '9') {
			return "en"
		}
	}
	return lang
}
