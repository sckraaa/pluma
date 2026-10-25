package gnet

import (
	"crypto/rand"
	"encoding/binary"
)

// pq of resPQ (https://core.telegram.org/mtproto/auth_key, step 2) is the
// product of two distinct primes p < q that the client must find before
// req_DH_params: a proof of work in front of the RSA decryption and DH
// exponentiation the server does for it. Every req_pq gets a new pq.
//
// p and q are random primes in [2^30, 2^31), as in the protocol documentation
// (p = 0x494C553B, q = 0x53911073), so pq is below 2^62:
//   - "Normally, pq is less than or equal to 2^63-1": the factorizers of the
//     official clients add modulo pq in uint64 and need pq < 2^63;
//   - the Android client keeps p and q in uint32 and sends them as 4 bytes.
//
// Factoring such a pq takes a client about 2^15.5 Pollard rho steps
// (milliseconds); generating it takes the server about 2 µs, so it is done
// on the event loop.
const (
	pqPrimeBits = 31
	// pqSize is the length of pq in resPQ: 8 bytes big-endian, as
	// Telegram sends it (pq >= 2^60 has no leading zero byte).
	pqSize = 8
)

// newPQ returns a random pq and its factors p < q.
func newPQ() (pq, p, q uint64) {
	var r pqRandom
	p = r.prime()
	q = r.prime()
	for q == p {
		q = r.prime()
	}
	if p > q {
		p, q = q, p
	}
	return p * q, p, q
}

// pqRandom hands out random numbers from crypto/rand, read in blocks.
type pqRandom struct {
	buf  [64]byte
	left int // bytes of buf not handed out yet
}

func (r *pqRandom) uint32() uint32 {
	if r.left == 0 {
		_, _ = rand.Read(r.buf[:]) // never fails
		r.left = len(r.buf)
	}
	r.left -= 4
	return binary.LittleEndian.Uint32(r.buf[r.left:])
}

// prime returns a random prime of pqPrimeBits bits.
func (r *pqRandom) prime() uint64 {
	for {
		// The top bit set (pqPrimeBits bits exactly), odd.
		n := uint64(r.uint32())&(1<<(pqPrimeBits-1)-1) | 1<<(pqPrimeBits-1) | 1
		if isPrime32(n) {
			return n
		}
	}
}

var smallOddPrimes = [...]uint64{3, 5, 7, 11, 13, 17, 19, 23, 29, 31, 37, 41, 43, 47, 53, 59, 61}

// isPrime32 reports whether n, which must be below 2^32, is prime: trial
// division by small primes, then Miller-Rabin with the bases 2, 7 and 61,
// which has no strong pseudoprime below 4,759,123,141 (Jaeschke, 1993) and
// so is deterministic here. Products of numbers below 2^32 fit in uint64.
func isPrime32(n uint64) bool {
	if n < 2 {
		return false
	}
	if n%2 == 0 {
		return n == 2
	}
	for _, sp := range smallOddPrimes {
		if n%sp == 0 {
			return n == sp
		}
	}
	// n-1 = d * 2^s with d odd; n > 61 here, so every base is below n.
	d, s := n-1, 0
	for d%2 == 0 {
		d /= 2
		s++
	}
bases:
	for _, a := range [...]uint64{2, 7, 61} {
		x := powMod32(a, d, n)
		if x == 1 || x == n-1 {
			continue
		}
		for i := 1; i < s; i++ {
			x = x * x % n
			if x == n-1 {
				continue bases
			}
		}
		return false
	}
	return true
}

// powMod32 returns a^e mod n for a, n < 2^32.
func powMod32(a, e, n uint64) uint64 {
	r := uint64(1)
	for a %= n; e > 0; e >>= 1 {
		if e&1 == 1 {
			r = r * a % n
		}
		a = a * a % n
	}
	return r
}

// putPQ encodes pq as resPQ carries it.
func putPQ(pq uint64) string {
	var b [pqSize]byte
	binary.BigEndian.PutUint64(b[:], pq)
	return string(b[:])
}

// parsePQNumber parses pq, p or q of req_DH_params and p_q_inner_data: a
// big-endian number of at most 8 bytes. The value is compared, not the
// bytes: the Android client sends p and q as 4 bytes, iOS and Web K without
// leading zeros.
func parsePQNumber(s string) (uint64, bool) {
	if len(s) == 0 || len(s) > 8 {
		return 0, false
	}
	var v uint64
	for i := 0; i < len(s); i++ {
		v = v<<8 | uint64(s[i])
	}
	return v, true
}

// hasPQ reports whether pq of a client is the pq of this handshake.
func (st *HandshakeStateCtx) hasPQ(pq string) bool {
	v, ok := parsePQNumber(pq)
	return ok && st.Pq != 0 && v == st.Pq
}

// hasFactors reports whether p and q of a client are the factors of the pq
// of this handshake, p < q.
func (st *HandshakeStateCtx) hasFactors(p, q string) bool {
	pv, ok1 := parsePQNumber(p)
	qv, ok2 := parsePQNumber(q)
	return ok1 && ok2 && st.P != 0 && pv == st.P && qv == st.Q
}
