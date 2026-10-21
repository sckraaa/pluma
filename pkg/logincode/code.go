package logincode

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
)

// Length is the number of digits in a login code. Telegram uses 5; a closed
// server has no SMS cost to save and a small group to protect, so it takes
// one more (10x fewer successful guesses). Official clients size the code
// input from auth.sentCodeType*.length, as they do for Telegram's 6-digit
// email codes.
const Length = 6

// MinHmacKeyLength is the shortest accepted Config.HmacKey, in bytes.
const MinHmacKeyLength = 32

var codeSpace = big.NewInt(1_000_000) // 10^Length

// Generate returns a new uniformly random login code of Length digits.
func Generate() (string, error) {
	return generate(rand.Reader)
}

func generate(r io.Reader) (string, error) {
	n, err := rand.Int(r, codeSpace)
	if err != nil {
		return "", fmt.Errorf("generate login code: %w", err)
	}
	return fmt.Sprintf("%0*d", Length, n.Int64()), nil
}

// wellFormed reports whether code looks like a login code: exactly Length
// ASCII digits. Clients send the digits only.
func wellFormed(code string) bool {
	if len(code) != Length {
		return false
	}
	for i := 0; i < len(code); i++ {
		if code[i] < '0' || code[i] > '9' {
			return false
		}
	}
	return true
}

// Hasher computes the value stored instead of a login code:
// HMAC-SHA256 under a server secret over (phone_code_hash, phone, code).
//
// A keyed MAC rather than a salted hash: with only 10^Length possible codes, anyone
// who can read Redis would invert a salted hash instantly; without the key the
// stored value is useless. Binding the phone and phone_code_hash makes a value
// valid only for the login transaction it was issued for.
type Hasher struct {
	key []byte
}

// NewHasher returns a Hasher keyed by key, which must be at least
// MinHmacKeyLength bytes (generate it with `openssl rand -hex 32`).
func NewHasher(key string) (*Hasher, error) {
	if len(key) < MinHmacKeyLength {
		return nil, fmt.Errorf("login code HMAC key must be at least %d bytes (LOGIN_CODE_HMAC_KEY, see docs/ops/security-setup.md)", MinHmacKeyLength)
	}
	return &Hasher{key: []byte(key)}, nil
}

// Sum returns the hex HMAC of code for the login transaction phoneCodeHash of phone.
func (h *Hasher) Sum(phoneCodeHash, phone, code string) string {
	m := hmac.New(sha256.New, h.key)
	var n [binary.MaxVarintLen64]byte
	for _, field := range []string{"pluma-login-code-v1", phoneCodeHash, phone, code} {
		// Length-prefixed fields: no two different inputs share an encoding.
		m.Write(n[:binary.PutUvarint(n[:], uint64(len(field)))])
		m.Write([]byte(field))
	}
	return hex.EncodeToString(m.Sum(nil))
}

// Verify reports, in constant time, whether sum is the HMAC of code for
// phoneCodeHash and phone.
func (h *Hasher) Verify(phoneCodeHash, phone, code, sum string) bool {
	want, err := hex.DecodeString(sum)
	if err != nil || len(want) != sha256.Size {
		return false
	}
	got, _ := hex.DecodeString(h.Sum(phoneCodeHash, phone, code))
	return hmac.Equal(got, want)
}
