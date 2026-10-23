package logincode

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/kv"
)

// Rule limits how many times an action may happen per subject (a phone, an
// auth key, an IP) in a fixed window.
type Rule struct {
	Name   string
	Limit  int64
	Window time.Duration
}

// Login flood limits. Counters are shared by the login (auth.*) and the
// change-phone (account.*) flows.
//
// Per-IP limits must leave room for the whole group behind one address (an
// office or family NAT, a reverse proxy without the PROXY protocol): they
// either count only failures that indicate probing (a phone not on the
// allowlist, a wrong code, an unknown phone_code_hash) or are far above what
// a group of 100 needs. Successful requests are refunded (see Service.reserve).
var (
	// Code requests: auth.sendCode, auth.resendCode, account.sendChangePhoneCode.
	//
	// Per phone, two budgets: one per requesting address (the owner's own
	// devices) and a total. A stranger who knows the phone can exhaust only
	// the total, and needs SendCodePerPhone.Limit / SendCodePerPhoneIP.Limit
	// addresses an hour to do it; pluma-admin login-unblock clears it.
	SendCodePerPhoneIP = Rule{Name: "send_code_phone_ip", Limit: 5, Window: time.Hour}
	SendCodePerPhone   = Rule{Name: "send_code_phone", Limit: 20, Window: time.Hour}
	SendCodePerAuthKey = Rule{Name: "send_code_key", Limit: 10, Window: time.Hour}
	// All code requests from one address: bounds the codes (and messages
	// from 777000) one address can trigger across many phones.
	SendCodePerIP = Rule{Name: "send_code_ip", Limit: 200, Window: time.Hour}

	// Phones not on the allowlist, asked about from one address by any
	// method that takes a phone. Only misses count: the allowlist cannot be
	// enumerated quickly, and members behind one NAT never hit it.
	PhoneProbePerIP = Rule{Name: "phone_probe_ip", Limit: 30, Window: time.Hour}
	// Allowlist lookups by the methods that take a phone without requesting a
	// code (auth.signIn, auth.signUp, auth.cancelCode, account.changePhone),
	// per auth key (one device).
	PhoneLookupPerAuthKey = Rule{Name: "phone_lookup_key", Limit: 30, Window: time.Hour}

	// Code checks: auth.signIn, account.changePhone. The per-phone limits
	// count real comparisons only (a request that has a live code), before the
	// comparison, so parallel guesses cannot slip past them; a correct code is
	// refunded. With Length digits and these limits a guesser gets at most
	// CheckCodePerPhoneMonth.Limit guesses per phone a month: 365 a year, a
	// 0.04% chance per phone and ~3.6% per year for an allowlist of 100 phones
	// all under attack, every new code showing up in the owner's app. Codes
	// of the administrator path exist only once the administrator issues
	// them. The real defence is a 2FA password (task 008).
	CheckCodePerPhone      = Rule{Name: "check_code_phone", Limit: 5, Window: time.Hour}
	CheckCodePerPhoneDay   = Rule{Name: "check_code_phone_day", Limit: 10, Window: 24 * time.Hour}
	CheckCodePerPhoneMonth = Rule{Name: "check_code_phone_month", Limit: 30, Window: 30 * 24 * time.Hour}
	// Failed checks (wrong code, unknown or expired request) from one address.
	CheckCodePerIP = Rule{Name: "check_code_ip", Limit: 30, Window: time.Hour}

	// Wrong codes across all phones. Not a limit (it would let anyone lock
	// everyone out): past it every failed check is logged as an error, the
	// signal of a brute force spread over many phones.
	CheckCodeFailuresAlert = Rule{Name: "check_code_failures", Limit: 50, Window: 24 * time.Hour}
)

const floodKeyPrefix = "login_flood_"

// alertSubject is the subject of rules counted across all phones.
const alertSubject = "all"

// KEYS[1] counter; ARGV window seconds. Fixed window: the first hit starts
// it. Returns {count, seconds left}.
const hitScript = `
local n = redis.call('INCR', KEYS[1])
local ttl = redis.call('TTL', KEYS[1])
if ttl < 0 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
  ttl = tonumber(ARGV[1])
end
return {n, ttl}`

// KEYS[1] counter. Takes back one hit; never creates the counter (it may have
// expired meanwhile) and drops it at zero.
const refundScript = `
if redis.call('EXISTS', KEYS[1]) == 0 then return 0 end
local n = redis.call('DECR', KEYS[1])
if n <= 0 then redis.call('DEL', KEYS[1]) end
return n`

// Limiter counts actions in Redis and answers FLOOD_WAIT_X (420) once a Rule
// is exceeded. Redis keeps the counters because the login state (codes,
// phone_code_hash transactions) already lives there and is shared by every
// bff instance; a counter is a single key with a TTL, so nothing has to be
// cleaned up.
type Limiter struct {
	kv kv.Store
}

// NewLimiter returns a Limiter on kv.
func NewLimiter(kv kv.Store) *Limiter {
	return &Limiter{kv: kv}
}

func floodKey(rule Rule, subject string) string {
	return floodKeyPrefix + rule.Name + "_" + subject
}

// Hit counts one action of subject under rule. It returns FLOOD_WAIT_X with
// the seconds left in the window when the rule is exceeded, and
// INTERNAL_SERVER_ERROR if Redis fails (the limits fail closed). An empty
// subject (e.g. an unknown client IP) is not counted.
func (l *Limiter) Hit(ctx context.Context, rule Rule, subject string) error {
	if subject == "" {
		return nil
	}
	n, ttl, err := l.count(ctx, rule, subject)
	if err != nil {
		logx.WithContext(ctx).Errorf("login flood limit %s: %v", rule.Name, err)
		return mtproto.ErrInternalServerError
	}
	if n <= rule.Limit {
		return nil
	}
	logx.WithContext(ctx).Infof("login flood limit %s exceeded (%d/%d), wait %ds", rule.Name, n, rule.Limit, ttl)
	return mtproto.NewErrFloodWaitX(int32(max(ttl, 1)))
}

// Over counts one action of subject under rule and reports whether the rule
// is now exceeded, for rules that only raise an alert. Redis errors count as
// not exceeded.
func (l *Limiter) Over(ctx context.Context, rule Rule, subject string) (bool, int64) {
	n, _, err := l.count(ctx, rule, subject)
	if err != nil {
		logx.WithContext(ctx).Errorf("login flood counter %s: %v", rule.Name, err)
		return false, 0
	}
	return n > rule.Limit, n
}

func (l *Limiter) count(ctx context.Context, rule Rule, subject string) (n, ttl int64, err error) {
	r, err := l.kv.EvalCtx(ctx, hitScript, floodKey(rule, subject), int64(rule.Window/time.Second))
	if err != nil {
		return 0, 0, err
	}
	return parseHit(r)
}

// Refund takes back one Hit of subject under rule, for an action that turned
// out not to be what the rule limits (e.g. a correct code). Best effort: a
// failed refund only leaves the counter one higher.
func (l *Limiter) Refund(ctx context.Context, rule Rule, subject string) {
	if subject == "" {
		return
	}
	if _, err := l.kv.EvalCtx(ctx, refundScript, floodKey(rule, subject)); err != nil {
		logx.WithContext(ctx).Errorf("login flood limit %s refund: %v", rule.Name, err)
	}
}

// Reset clears the counter of subject under rule (pluma-admin login-unblock).
func (l *Limiter) Reset(ctx context.Context, rule Rule, subject string) error {
	if subject == "" {
		return nil
	}
	_, err := l.kv.DelCtx(ctx, floodKey(rule, subject))
	return err
}

func parseHit(r any) (n, ttl int64, err error) {
	a, ok := r.([]any)
	if !ok || len(a) != 2 {
		return 0, 0, fmt.Errorf("unexpected reply %T", r)
	}
	if n, err = toInt64(a[0]); err != nil {
		return
	}
	ttl, err = toInt64(a[1])
	return
}

// ipSubject returns the limiter subject of a client address: an IPv4 address
// (IPv4-mapped IPv6 included) as is, an IPv6 address as its /64, which one
// subscriber usually gets whole and can rotate addresses in. An address it
// cannot parse is used as given; "" stays "" (not counted).
func ipSubject(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		host, _, splitErr := net.SplitHostPort(ip)
		if splitErr != nil {
			return ip
		}
		if addr, err = netip.ParseAddr(host); err != nil {
			return ip
		}
	}
	addr = addr.Unmap().WithZone("")
	if addr.Is4() {
		return addr.String()
	}
	p, err := addr.Prefix(64)
	if err != nil {
		return addr.String()
	}
	return p.String()
}

// phoneIpSubject is the subject of per-phone limits of one requesting address.
func phoneIpSubject(phone, ip string) string {
	if phone == "" || ip == "" {
		return ""
	}
	return phone + "_" + ip
}
