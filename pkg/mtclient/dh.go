package mtclient

import (
	"crypto/rsa"
	"crypto/sha1" //nolint:gosec // MTProto key exchange: tmp_aes_key, auth_key_id and the nonce hashes are defined over SHA-1
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"math/bits"
	"slices"
	"sync"

	"github.com/teamgram/proto/mtproto/crypto"
)

// RSAPad encrypts data (at most 144 bytes) for the server key pub as
// RSA_PAD of the key exchange (https://core.telegram.org/mtproto/auth_key,
// step 4.1): the encrypted_data of req_DH_params.
func RSAPad(random io.Reader, pub *rsa.PublicKey, data []byte) ([]byte, error) {
	if len(data) > 144 {
		return nil, fmt.Errorf("mtclient: RSA_PAD data is %d bytes, at most 144", len(data))
	}
	if pub.N.BitLen() != 2048 {
		return nil, fmt.Errorf("mtclient: server key is %d bits, want 2048", pub.N.BitLen())
	}
	dataWithPadding := make([]byte, 192)
	copy(dataWithPadding, data)
	if _, err := io.ReadFull(random, dataWithPadding[len(data):]); err != nil {
		return nil, err
	}
	reversed := slices.Clone(dataWithPadding)
	slices.Reverse(reversed)

	tempKey := make([]byte, 32)
	for {
		if _, err := io.ReadFull(random, tempKey); err != nil {
			return nil, err
		}
		h := sha256.New()
		h.Write(tempKey)
		h.Write(dataWithPadding)
		dataWithHash := h.Sum(slices.Clone(reversed))

		aesEncrypted, err := crypto.NewAES256IGECryptor(tempKey, make([]byte, 32)).Encrypt(dataWithHash)
		if err != nil {
			return nil, err
		}
		h2 := sha256.Sum256(aesEncrypted)
		keyAesEncrypted := make([]byte, 0, 256)
		for i := range tempKey {
			keyAesEncrypted = append(keyAesEncrypted, tempKey[i]^h2[i])
		}
		keyAesEncrypted = append(keyAesEncrypted, aesEncrypted...)

		m := new(big.Int).SetBytes(keyAesEncrypted)
		if m.Cmp(pub.N) >= 0 {
			continue // "the probability is ~ 1/2^16": pick another temp_key
		}
		c := new(big.Int).Exp(m, big.NewInt(int64(pub.E)), pub.N)
		return c.FillBytes(make([]byte, 256)), nil
	}
}

// TempAESKeyIV returns tmp_aes_key and tmp_aes_iv of the key exchange, which
// encrypt server_DH_inner_data and client_DH_inner_data.
func TempAESKeyIV(newNonce, serverNonce []byte) (key, iv []byte) {
	sum := func(a, b []byte) []byte {
		h := sha1.New() //nolint:gosec // defined by the protocol
		h.Write(a)
		h.Write(b)
		return h.Sum(nil)
	}
	ns := sum(newNonce, serverNonce)
	sn := sum(serverNonce, newNonce)
	nn := sum(newNonce, newNonce)
	key = append(append(make([]byte, 0, 32), ns...), sn[:12]...)
	iv = append(append(append(make([]byte, 0, 32), sn[12:20]...), nn...), newNonce[:4]...)
	return key, iv
}

// AuthKeyID returns auth_key_id: the low 64 bits of SHA1(auth_key).
func AuthKeyID(authKey []byte) int64 {
	h := sha1.Sum(authKey) //nolint:gosec // defined by the protocol
	return int64(binary.LittleEndian.Uint64(h[12:20]))
}

// newNonceHash returns new_nonce_hash1/2/3 of dh_gen_ok/retry/fail: the low
// 128 bits of SHA1(new_nonce + n + auth_key_aux_hash).
func newNonceHash(newNonce, authKey []byte, n byte) []byte {
	keyHash := sha1.Sum(authKey) //nolint:gosec // defined by the protocol
	h := sha1.New()              //nolint:gosec // defined by the protocol
	h.Write(newNonce)
	h.Write([]byte{n})
	h.Write(keyHash[:8])
	return h.Sum(nil)[4:20]
}

// FactorizePQ splits pq of resPQ into its two prime factors p < q, the proof
// of work of the key exchange (Pollard's rho; the server keeps pq below 2^63,
// as the protocol says it normally is).
func FactorizePQ(pq uint64) (p, q uint64, err error) {
	if pq < 4 || pq >= 1<<63 {
		return 0, 0, fmt.Errorf("mtclient: pq %d is not a product of two primes", pq)
	}
	if pq%2 == 0 {
		return 2, pq / 2, nil
	}
	mulMod := func(a, b uint64) uint64 {
		hi, lo := bits.Mul64(a, b)
		return bits.Rem64(hi, lo, pq)
	}
	gcd := func(a, b uint64) uint64 {
		for b != 0 {
			a, b = b, a%b
		}
		return a
	}
	for c := uint64(1); c < 64; c++ {
		f := func(x uint64) uint64 { return (mulMod(x, x) + c) % pq }
		x, y, d := uint64(2), uint64(2), uint64(1)
		for d == 1 {
			x = f(x)
			y = f(f(y))
			if x > y {
				d = gcd(x-y, pq)
			} else {
				d = gcd(y-x, pq)
			}
		}
		if d != pq {
			p, q = d, pq/d
			if p > q {
				p, q = q, p
			}
			return p, q, nil
		}
	}
	return 0, 0, fmt.Errorf("mtclient: cannot factorize pq %d", pq)
}

// knownGoodPrimes caches dh_prime values that passed checkDHPrime: the
// primality tests take tens of milliseconds and the server always sends the
// same prime.
var knownGoodPrimes sync.Map // string(prime bytes) -> true

// checkDHPrime checks dh_prime and g of server_DH_inner_data as the protocol
// requires (https://core.telegram.org/mtproto/auth_key, "client is expected
// to check"): dh_prime a 2048-bit safe prime, g a generator of the subgroup
// of order (dh_prime-1)/2.
func checkDHPrime(g int64, p *big.Int) error {
	if p.BitLen() != 2048 {
		return fmt.Errorf("mtclient: dh_prime is %d bits, want 2048", p.BitLen())
	}
	mod := func(n int64) int64 { return new(big.Int).Mod(p, big.NewInt(n)).Int64() }
	var ok bool
	switch g {
	case 2:
		ok = mod(8) == 7
	case 3:
		ok = mod(3) == 2
	case 4:
		ok = true
	case 5:
		r := mod(5)
		ok = r == 1 || r == 4
	case 6:
		r := mod(24)
		ok = r == 19 || r == 23
	case 7:
		r := mod(7)
		ok = r == 3 || r == 5 || r == 6
	}
	if !ok {
		return fmt.Errorf("mtclient: g = %d does not generate the subgroup of dh_prime", g)
	}
	key := string(p.Bytes())
	if _, known := knownGoodPrimes.Load(key); known {
		return nil
	}
	half := new(big.Int).Rsh(p, 1)
	if !p.ProbablyPrime(20) || !half.ProbablyPrime(20) {
		return errors.New("mtclient: dh_prime is not a safe prime")
	}
	knownGoodPrimes.Store(key, true)
	return nil
}

// checkDHValue checks g_a or g_b: 1 < v < dh_prime - 1 and, as the protocol
// recommends, 2^(2048-64) < v < dh_prime - 2^(2048-64), the bounds the
// gateway checks g_b against.
func checkDHValue(v, p *big.Int) error {
	low := new(big.Int).Lsh(big.NewInt(1), 2048-64)
	high := new(big.Int).Sub(p, low)
	if v.Cmp(low) <= 0 || v.Cmp(high) >= 0 {
		return errors.New("mtclient: DH value out of the safe range")
	}
	return nil
}
