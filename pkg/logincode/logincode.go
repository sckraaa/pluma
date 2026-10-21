// Package logincode issues and checks the login codes of auth.sendCode /
// auth.signIn and account.sendChangePhoneCode / account.changePhone for a
// closed deployment (task 001, ADR 0003):
//
//   - only phones on the allowlist (MySQL table phone_allowlist, managed with
//     tools/pluma-admin) may request or use a code; others get
//     PHONE_NUMBER_INVALID, whether or not an account exists;
//   - a code is Length random digits from crypto/rand; only its HMAC (see
//     Hasher) is stored, in Redis, next to the phone_code_hash;
//   - a phone_code_hash allows MaxAttempts code checks, then
//     PHONE_CODE_EXPIRED; code requests and checks are limited with
//     FLOOD_WAIT_X (see Rule);
//   - a code goes either to the user's other authorized sessions (the caller
//     sends it as a message from the service notifications account) or, when
//     there is none, nowhere: the administrator issues one for a chosen
//     pending request with `pluma-admin login-code <phone> [request]`
//     (Service.Pending, Service.Reissue). There is no fixed or test code.
package logincode

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/kv"
)

const (
	// CodeTTL is how long a code is valid after it is issued.
	CodeTTL = 10 * time.Minute
	// TransactionTTL is how long a login request (phone_code_hash) lives. It
	// leaves the administrator time to issue a code by hand.
	TransactionTTL = time.Hour
	// MaxAttempts is the number of code checks per phone_code_hash.
	MaxAttempts = 5
)

// Config is the LoginCode section of the bff config.
type Config struct {
	// HmacKey keys the HMAC under which codes are stored: ${LOGIN_CODE_HMAC_KEY},
	// at least MinHmacKeyLength bytes. Changing it invalidates pending codes only.
	HmacKey string
}

// Allowlist tells which phones may log in or sign up.
type Allowlist interface {
	// Contains reports whether phone (normalized digits, as users.phone) is allowed.
	Contains(ctx context.Context, phone string) (bool, error)
}

// ReissueMinTime is the least time a request must have left for Reissue to
// issue a code for it: the code is useless once the request ends.
const ReissueMinTime = 3 * time.Minute

// ErrNoPendingRequest is returned by Reissue when the phone has no code
// request waiting (none was made, it expired or it was used).
var ErrNoPendingRequest = errors.New("no pending login code request for this phone: ask the user to request a code in the app first")

// ErrRequestUsedUp is returned by Reissue for a request with no code checks
// left: a new code could never be accepted.
var ErrRequestUsedUp = errors.New("this login request has no code checks left: ask the user to request a new code in the app")

// ErrRequestEnding is returned by Reissue for a request that ends within
// ReissueMinTime.
var ErrRequestEnding = errors.New("this login request expires in less than 3 minutes: ask the user to request a new code in the app")

// ErrAmbiguousRequest is returned by Reissue when the phone has several
// pending requests and none (or no single one) was chosen.
var ErrAmbiguousRequest = errors.New("several pending login code requests for this phone: choose one by its id")

// ErrNotAllowed is returned by Reissue for a phone that is not on the allowlist.
var ErrNotAllowed = errors.New("phone is not on the allowlist")

// Service applies the login code policy. It is safe for concurrent use.
type Service struct {
	allowlist Allowlist
	store     *Store
	limiter   *Limiter
	hasher    *Hasher

	now    func() time.Time
	random io.Reader
}

// NewService returns a Service storing codes and flood counters in kv. It
// fails if the HMAC key is missing or too short.
func NewService(c Config, allowlist Allowlist, kv kv.Store) (*Service, error) {
	hasher, err := NewHasher(c.HmacKey)
	if err != nil {
		return nil, err
	}
	if allowlist == nil {
		return nil, errors.New("logincode: allowlist is required")
	}
	return &Service{
		allowlist: allowlist,
		store:     NewStore(kv),
		limiter:   NewLimiter(kv),
		hasher:    hasher,
		now:       time.Now,
		random:    rand.Reader,
	}, nil
}

// CheckAllowed returns PHONE_NUMBER_INVALID if phone is not on the allowlist.
func (s *Service) CheckAllowed(ctx context.Context, phone string) error {
	ok, err := s.allowlist.Contains(ctx, phone)
	if err != nil {
		logx.WithContext(ctx).Errorf("phone allowlist lookup: %v", err)
		return mtproto.ErrInternalServerError
	}
	if !ok {
		logx.WithContext(ctx).Infof("login code: phone not on the allowlist")
		return mtproto.ErrPhoneNumberInvalid
	}
	return nil
}

// CheckPhone applies the allowlist to a request that names phone without
// asking for a code (auth.signIn, auth.signUp, auth.cancelCode,
// account.changePhone), after the lookup limits of the auth key and IP.
func (s *Service) CheckPhone(ctx context.Context, phone string, authKeyId int64, clientIp string) error {
	if err := s.limiter.Hit(ctx, PhoneLookupPerAuthKey, formatKeyId(authKeyId)); err != nil {
		return err
	}
	return s.checkAllowedFrom(ctx, phone, ipSubject(clientIp))
}

// CheckSendCode applies the allowlist and the code request limits to a
// request for a code for phone. The per-IP and per-auth-key limits come
// first, so they also throttle probing of the allowlist.
func (s *Service) CheckSendCode(ctx context.Context, phone string, authKeyId int64, clientIp string) error {
	ip := ipSubject(clientIp)
	if err := s.limiter.Hit(ctx, SendCodePerIP, ip); err != nil {
		return err
	}
	if err := s.limiter.Hit(ctx, SendCodePerAuthKey, formatKeyId(authKeyId)); err != nil {
		return err
	}
	if err := s.checkAllowedFrom(ctx, phone, ip); err != nil {
		return err
	}
	if err := s.limiter.Hit(ctx, SendCodePerPhoneIP, phoneIpSubject(phone, ip)); err != nil {
		return err
	}
	return s.limiter.Hit(ctx, SendCodePerPhone, phone)
}

// checkAllowedFrom applies the allowlist to phone asked about from ip,
// counting a miss under PhoneProbePerIP (reserved before the lookup, so
// parallel probes are counted too; refunded when the phone is allowed).
func (s *Service) checkAllowedFrom(ctx context.Context, phone, ip string) error {
	if err := s.limiter.Hit(ctx, PhoneProbePerIP, ip); err != nil {
		return err
	}
	if err := s.CheckAllowed(ctx, phone); err != nil {
		if !errors.Is(err, mtproto.ErrPhoneNumberInvalid) {
			s.limiter.Refund(ctx, PhoneProbePerIP, ip)
		}
		return err
	}
	s.limiter.Refund(ctx, PhoneProbePerIP, ip)
	return nil
}

// Issue registers the login request phoneCodeHash of phone as one of the
// phone's pending requests (the ones Reissue can issue a code for).
//
// With withCode it also generates a code and returns it, for delivery to the
// user's other sessions; the plaintext is not kept. Without it the request
// has no code until the administrator issues one (Reissue), and no check
// can succeed before that.
func (s *Service) Issue(ctx context.Context, phone, phoneCodeHash, clientIp string, withCode bool) (string, error) {
	var (
		now  = s.now()
		code string
		v    = secret{Phone: phone, CreatedAt: now.Unix(), ClientIp: clientIp}
	)
	if withCode {
		var err error
		if code, err = generate(s.random); err != nil {
			logx.WithContext(ctx).Errorf("login code: %v", err)
			return "", mtproto.ErrInternalServerError
		}
		v.Hmac = s.hasher.Sum(phoneCodeHash, phone, code)
		v.ExpiresAt = now.Add(CodeTTL).Unix()
	}
	if err := s.store.put(ctx, phoneCodeHash, v, TransactionTTL); err != nil {
		logx.WithContext(ctx).Errorf("login code store: %v", err)
		return "", mtproto.ErrInternalServerError
	}
	if err := s.store.addRequest(ctx, phone, phoneCodeHash, v.CreatedAt, TransactionTTL); err != nil {
		logx.WithContext(ctx).Errorf("login code store: %v", err)
		return "", mtproto.ErrInternalServerError
	}
	return code, nil
}

// limitHit is one counter of a reservation (see reserve).
type limitHit struct {
	rule    Rule
	subject string
}

// reserve counts one action under each of hits, in order, and returns a
// function that refunds them all. If a rule is exceeded the hits made so far
// are refunded and its FLOOD_WAIT_X is returned.
func (s *Service) reserve(ctx context.Context, hits ...limitHit) (refund func(), err error) {
	done := make([]limitHit, 0, len(hits))
	refund = func() {
		for _, h := range done {
			s.limiter.Refund(ctx, h.rule, h.subject)
		}
	}
	for _, h := range hits {
		if err = s.limiter.Hit(ctx, h.rule, h.subject); err != nil {
			refund()
			return func() {}, err
		}
		done = append(done, h)
	}
	return refund, nil
}

// Verify checks code for the request phoneCodeHash of phone, applying the
// check limits first. On success the code is used up. Errors:
// FLOOD_WAIT_X, PHONE_CODE_EXPIRED (no request, code expired or out of
// attempts), PHONE_CODE_INVALID.
//
// Every check is counted against the client address and refunded if the code
// is right. The per-phone limits count only checks that compare a live code:
// a request without one (waiting for the administrator, expired, unknown)
// cannot be guessed, so a stranger's checks of it do not lock the owner out.
func (s *Service) Verify(ctx context.Context, phone, phoneCodeHash, code, clientIp string) error {
	refundIp, err := s.reserve(ctx, limitHit{CheckCodePerIP, ipSubject(clientIp)})
	if err != nil {
		return err
	}

	now := s.now().Unix()
	v, ok, err := s.store.attempt(ctx, phoneCodeHash, now)
	switch {
	case err != nil:
		refundIp()
		logx.WithContext(ctx).Errorf("login code store: %v", err)
		return mtproto.ErrInternalServerError
	case !ok:
		return mtproto.ErrPhoneCodeExpired
	case v.Hmac == "":
		// Waiting for the administrator: there is no code to match yet.
		return mtproto.ErrPhoneCodeInvalid
	case now > v.ExpiresAt:
		// Kept, not counted: the administrator can still issue a new code
		// for this request.
		return mtproto.ErrPhoneCodeExpired
	case v.Attempts > MaxAttempts:
		s.drop(ctx, v.Phone, phoneCodeHash)
		return mtproto.ErrPhoneCodeExpired
	}

	// A real comparison: it counts against the phone of the request.
	refundPhone, err := s.reserve(ctx,
		limitHit{CheckCodePerPhone, v.Phone},
		limitHit{CheckCodePerPhoneDay, v.Phone},
		limitHit{CheckCodePerPhoneMonth, v.Phone})
	if err != nil {
		return err
	}

	if v.Phone != phone || !wellFormed(code) || !s.hasher.Verify(phoneCodeHash, phone, code, v.Hmac) {
		if v.Attempts >= MaxAttempts {
			// That was the last check: the request is over.
			s.drop(ctx, v.Phone, phoneCodeHash)
		}
		logx.WithContext(ctx).Infof("login code: wrong code (attempt %d/%d)", v.Attempts, MaxAttempts)
		if over, n := s.limiter.Over(ctx, CheckCodeFailuresAlert, alertSubject); over {
			logx.WithContext(ctx).Errorf("login code: %d wrong codes in the last %s across all phones: possible brute force (check pluma-admin login-code requests and the client IPs)",
				n, CheckCodeFailuresAlert.Window)
		}
		return mtproto.ErrPhoneCodeInvalid
	}

	refundPhone()
	refundIp()
	s.drop(ctx, phone, phoneCodeHash)
	return nil
}

// Cancel drops the request phoneCodeHash of phone (auth.cancelCode, or a
// newer request from the same auth key).
func (s *Service) Cancel(ctx context.Context, phone, phoneCodeHash string) {
	s.drop(ctx, phone, phoneCodeHash)
}

func (s *Service) drop(ctx context.Context, phone, phoneCodeHash string) {
	if err := s.store.delete(ctx, phoneCodeHash); err != nil {
		logx.WithContext(ctx).Errorf("login code store: %v", err)
	}
	if err := s.store.removeRequest(ctx, phone, phoneCodeHash); err != nil {
		logx.WithContext(ctx).Errorf("login code store: %v", err)
	}
}

// PendingRequest is a code request that waits for a code (or for the code it
// has to be checked).
type PendingRequest struct {
	// Id identifies the request to the administrator: a prefix of its
	// phone_code_hash, unique among the phone's pending requests.
	Id          string
	RequestedAt time.Time // when the user requested a code
	EndsAt      time.Time // when the request (and any code of it) expires
	ClientIp    string    // from where the user requested it
	Attempts    int64     // checks already used out of MaxAttempts
	HasCode     bool      // a code was issued (in the app or by the administrator)

	phoneCodeHash string
}

// requestIdLength is the shortest Id of a PendingRequest.
const requestIdLength = 8

// Pending returns the pending code requests of phone, oldest first. Anyone
// who knows the phone can create one, so the administrator picks the one
// whose time and address match what the owner says.
func (s *Service) Pending(ctx context.Context, phone string) ([]PendingRequest, error) {
	if ok, err := s.allowlist.Contains(ctx, phone); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrNotAllowed
	}
	hashes, err := s.store.requests(ctx, phone)
	if err != nil {
		return nil, err
	}
	var list []PendingRequest
	for _, h := range hashes {
		v, ok, err := s.store.get(ctx, h)
		if err != nil {
			return nil, err
		}
		if !ok || v.Phone != phone {
			// Used, cancelled or expired.
			_ = s.store.removeRequest(ctx, phone, h)
			continue
		}
		list = append(list, PendingRequest{
			RequestedAt:   time.Unix(v.CreatedAt, 0),
			EndsAt:        time.Unix(v.CreatedAt, 0).Add(TransactionTTL),
			ClientIp:      v.ClientIp,
			Attempts:      v.Attempts,
			HasCode:       v.Hmac != "",
			phoneCodeHash: h,
		})
	}
	sort.Slice(list, func(i, j int) bool {
		if !list[i].RequestedAt.Equal(list[j].RequestedAt) {
			return list[i].RequestedAt.Before(list[j].RequestedAt)
		}
		return list[i].phoneCodeHash < list[j].phoneCodeHash
	})
	assignRequestIds(list)
	return list, nil
}

// assignRequestIds gives every request the shortest prefix of its
// phone_code_hash (at least requestIdLength) that no other request shares.
func assignRequestIds(list []PendingRequest) {
	for i := range list {
		h := list[i].phoneCodeHash
		n := min(requestIdLength, len(h))
		for ; n < len(h); n++ {
			unique := true
			for j := range list {
				if j != i && strings.HasPrefix(list[j].phoneCodeHash, h[:n]) {
					unique = false
					break
				}
			}
			if unique {
				break
			}
		}
		list[i].Id = h[:n]
	}
}

// Reissued is a code issued by the administrator.
type Reissued struct {
	Code      string
	ExpiresAt time.Time
	PendingRequest
}

// Reissue generates a fresh code for the pending request requestId of phone
// (a PendingRequest.Id or a longer prefix of its phone_code_hash) and stores
// its HMAC in place of the previous code; the attempt counter is kept. With
// requestId "" the phone must have exactly one pending request. It is the
// administrator's delivery path (tools/pluma-admin).
//
// The code expires CodeTTL from now, but never after its request: a request
// with less than ReissueMinTime left (ErrRequestEnding) or without checks
// left (ErrRequestUsedUp) gets no code.
func (s *Service) Reissue(ctx context.Context, phone, requestId string) (*Reissued, error) {
	list, err := s.Pending(ctx, phone)
	if err != nil {
		return nil, err
	}
	var chosen []PendingRequest
	for _, r := range list {
		if requestId == "" || strings.HasPrefix(r.phoneCodeHash, strings.ToLower(requestId)) {
			chosen = append(chosen, r)
		}
	}
	switch {
	case len(chosen) == 0:
		return nil, ErrNoPendingRequest
	case len(chosen) > 1:
		return nil, ErrAmbiguousRequest
	}
	r := chosen[0]
	now := s.now()
	switch {
	case r.Attempts >= MaxAttempts:
		return nil, ErrRequestUsedUp
	case r.EndsAt.Sub(now) < ReissueMinTime:
		return nil, ErrRequestEnding
	}

	code, err := generate(s.random)
	if err != nil {
		return nil, err
	}
	expiresAt := now.Add(CodeTTL)
	if expiresAt.After(r.EndsAt) {
		expiresAt = r.EndsAt
	}
	ok, err := s.store.replace(ctx, r.phoneCodeHash, s.hasher.Sum(r.phoneCodeHash, phone, code), expiresAt.Unix())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNoPendingRequest
	}
	r.HasCode = true
	return &Reissued{Code: code, ExpiresAt: expiresAt, PendingRequest: r}, nil
}

// Unblock clears the flood counters of phone: the per-phone code request and
// check limits (pluma-admin login-unblock). The per-address request limit of
// the phone expires within an hour by itself.
func (s *Service) Unblock(ctx context.Context, phone string) error {
	for _, rule := range []Rule{SendCodePerPhone, CheckCodePerPhone, CheckCodePerPhoneDay, CheckCodePerPhoneMonth} {
		if err := s.limiter.Reset(ctx, rule, phone); err != nil {
			return err
		}
	}
	return nil
}

// UnblockIP clears the flood counters of a client address (an IPv6 address
// clears its /64).
func (s *Service) UnblockIP(ctx context.Context, ip string) error {
	if _, err := netip.ParseAddr(ip); err != nil {
		return err
	}
	subject := ipSubject(ip)
	for _, rule := range []Rule{SendCodePerIP, PhoneProbePerIP, CheckCodePerIP} {
		if err := s.limiter.Reset(ctx, rule, subject); err != nil {
			return err
		}
	}
	return nil
}

func formatKeyId(authKeyId int64) string {
	if authKeyId == 0 {
		return ""
	}
	return strconv.FormatInt(authKeyId, 10)
}
