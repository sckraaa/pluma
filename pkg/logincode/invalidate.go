package logincode

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/stores/kv"
)

// MaxInvalidateCodes is the most codes one invalidation looks at: the limit
// of account.invalidateSignInCodes (LIMIT_INVALID above it), and the most
// candidates a message gives (Candidates).
const MaxInvalidateCodes = 100

// Invalidator drops the pending login requests whose code the person has
// shown to someone: core.telegram.org/api/auth, "Invalidating login codes".
// The servers invalidate a code the person sent or forwarded to a chat, and
// the clients call account.invalidateSignInCodes with the codes of the chat
// with the service notifications account when the person takes a
// screenshot of it (iOS TelegramUI/Sources/ChatController.swift:7945-7958,
// TelegramCore/Sources/Authorization.swift:1428-1434). It is safe for
// concurrent use.
type Invalidator struct {
	store  *Store
	hasher *Hasher
	now    func() time.Time
}

// NewInvalidator returns an Invalidator on the codes kept in kv (the KV
// config of bff). It needs only the HMAC key of c, for whoever invalidates
// codes without issuing them; it fails if the key is missing or too short.
func NewInvalidator(c Config, kv kv.Store) (*Invalidator, error) {
	hasher, err := NewHasher(c.HmacKey)
	if err != nil {
		return nil, err
	}
	if kv == nil {
		return nil, errors.New("logincode: kv store is required")
	}
	return &Invalidator{store: NewStore(kv), hasher: hasher, now: time.Now}, nil
}

// Invalidate is Invalidator.Invalidate on the codes of s.
func (s *Service) Invalidate(ctx context.Context, phone string, codes []string) (int, error) {
	return (&Invalidator{store: s.store, hasher: s.hasher, now: s.now}).Invalidate(ctx, phone, codes)
}

// Invalidate drops every pending request of phone that has a live code (of
// the app or of the administrator) equal to one of codes, and returns how
// many it dropped. A dropped request takes both its codes with it: the
// person asks for a new one. Codes are normalized as the clients send them
// (dashes removed); those that cannot be a login code are ignored, and only
// the first MaxInvalidateCodes distinct ones are looked at. Nothing is
// counted against the check limits, and no attempt of a request is used:
// a code that does not match leaves its request as it was. Neither the
// codes nor the phone are logged.
func (v *Invalidator) Invalidate(ctx context.Context, phone string, codes []string) (int, error) {
	codes = normalizeCodes(codes)
	if phone == "" || len(codes) == 0 {
		return 0, nil
	}
	hashes, err := v.store.requests(ctx, phone)
	if err != nil {
		return 0, err
	}
	now := v.now().Unix()
	dropped := 0
	for _, h := range hashes {
		s, ok, err := v.store.get(ctx, h)
		if err != nil {
			return dropped, err
		}
		if !ok || s.Phone != phone || !v.matchesAny(h, phone, s, codes, now) {
			// Used, cancelled or expired (the requests hash is pruned on
			// the next request), or not one of these codes.
			continue
		}
		if err := v.store.delete(ctx, h); err != nil {
			return dropped, err
		}
		if err := v.store.removeRequest(ctx, phone, h); err != nil {
			return dropped, err
		}
		dropped++
	}
	return dropped, nil
}

// matchesAny reports whether a live code of the request phoneCodeHash, in
// state s, is one of codes at unix time now.
func (v *Invalidator) matchesAny(phoneCodeHash, phone string, s secret, codes []string, now int64) bool {
	app, admin := live(s.Hmac, s.ExpiresAt, now), live(s.AdminHmac, s.AdminExpiresAt, now)
	for _, code := range codes {
		if app && v.hasher.Verify(phoneCodeHash, phone, code, s.Hmac) ||
			admin && v.hasher.Verify(phoneCodeHash, phone, code, s.AdminHmac) {
			return true
		}
	}
	return false
}

// normalizeCodes returns the distinct well-formed codes of codes with their
// dashes removed, at most MaxInvalidateCodes of them, in order.
func normalizeCodes(codes []string) []string {
	out := make([]string, 0, min(len(codes), MaxInvalidateCodes))
	seen := make(map[string]bool, cap(out))
	for _, c := range codes {
		c = strings.ReplaceAll(c, "-", "")
		if !wellFormed(c) || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
		if len(out) == MaxInvalidateCodes {
			break
		}
	}
	return out
}

// Candidates returns the strings of text that may be a login code, for a
// server-side invalidation of the codes a person sends: every run of ASCII
// digits and dashes with exactly Length digits, without its dashes
// ("12-34-56" is one), distinct, at most MaxInvalidateCodes of them. A
// longer or shorter run of digits is not a code ("1234567" holds none).
// Most messages have none, and then nothing has to be looked up.
func Candidates(text string) []string {
	var (
		out    []string
		seen   map[string]bool
		digits = make([]byte, 0, Length)
		n      int // digits in the current run
	)
	flush := func() {
		if n == Length {
			c := string(digits)
			if !seen[c] {
				if seen == nil {
					seen = make(map[string]bool)
				}
				seen[c] = true
				out = append(out, c)
			}
		}
		digits, n = digits[:0], 0
	}
	for i := 0; i < len(text) && len(out) < MaxInvalidateCodes; i++ {
		switch ch := text[i]; {
		case ch >= '0' && ch <= '9':
			if n < Length {
				digits = append(digits, ch)
			}
			n++
		case ch == '-':
		default:
			flush()
		}
	}
	if len(out) < MaxInvalidateCodes {
		flush()
	}
	return out
}
