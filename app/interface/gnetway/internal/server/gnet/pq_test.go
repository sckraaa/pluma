package gnet

import (
	"crypto/rand"
	"encoding/binary"
	"math/big"
	"testing"
)

func TestIsPrime32(t *testing.T) {
	// Every number below 2^20 against a sieve.
	const n = 1 << 20
	composite := make([]bool, n)
	for i := 2; i*i < n; i++ {
		if !composite[i] {
			for j := i * i; j < n; j += i {
				composite[j] = true
			}
		}
	}
	for i := uint64(0); i < n; i++ {
		if want := i >= 2 && !composite[i]; isPrime32(i) != want {
			t.Fatalf("isPrime32(%d) = %v", i, !want)
		}
	}

	// Strong pseudoprimes to some of the bases, and the ends of the range.
	for _, c := range []struct {
		n     uint64
		prime bool
	}{
		{2047, false},       // base 2
		{1373653, false},    // bases 2, 3
		{25326001, false},   // bases 2, 3, 5
		{3215031751, false}, // bases 2, 3, 5, 7
		{4294967291, true},  // the largest prime below 2^32
		{4294967295, false},
		{1<<31 - 1, true},
	} {
		if isPrime32(c.n) != c.prime {
			t.Errorf("isPrime32(%d) = %v", c.n, !c.prime)
		}
	}

	// Random numbers below 2^32 against math/big, exact below 2^64.
	var b [4]byte
	for i := 0; i < 200000; i++ {
		_, _ = rand.Read(b[:])
		v := uint64(binary.LittleEndian.Uint32(b[:])) | 1
		if isPrime32(v) != new(big.Int).SetUint64(v).ProbablyPrime(0) {
			t.Fatalf("isPrime32(%d) disagrees with math/big", v)
		}
	}
}

func TestNewPQ(t *testing.T) {
	seen := make(map[uint64]bool)
	for i := 0; i < 1000; i++ {
		pq, p, q := newPQ()
		if p >= q || p*q != pq {
			t.Fatalf("pq %d, p %d, q %d", pq, p, q)
		}
		for _, f := range []uint64{p, q} {
			if f>>(pqPrimeBits-1) != 1 || !new(big.Int).SetUint64(f).ProbablyPrime(0) {
				t.Fatalf("factor %d is not a %d-bit prime", f, pqPrimeBits)
			}
		}
		// Below 2^62 (the clients need < 2^63), at least 2^60.
		if pq>>62 != 0 || pq>>60 == 0 {
			t.Fatalf("pq %#x out of range", pq)
		}
		if seen[pq] {
			t.Fatalf("pq %d twice", pq)
		}
		seen[pq] = true

		if s := putPQ(pq); len(s) != pqSize || s[0] == 0 {
			t.Fatalf("pq encoded as %x", s)
		}
		if v, ok := parsePQNumber(putPQ(pq)); !ok || v != pq {
			t.Fatalf("pq %d parsed as %d", pq, v)
		}
	}
}

func TestHandshakeStateFactors(t *testing.T) {
	pq, p, q := newPQ()
	st := HandshakeStateCtx{Pq: pq, P: p, Q: q}
	be := func(v uint64, n int) string {
		b := binary.BigEndian.AppendUint64(nil, v)
		return string(b[8-n:])
	}
	minimal := func(v uint64) string { return string(new(big.Int).SetUint64(v).Bytes()) }

	if !st.hasPQ(putPQ(pq)) || st.hasPQ(putPQ(pq+2)) || st.hasPQ("") || st.hasPQ("\x00"+putPQ(pq)) {
		t.Fatal("hasPQ")
	}
	for _, c := range []struct {
		name string
		p, q string
		ok   bool
	}{
		{"4 bytes (Android)", be(p, 4), be(q, 4), true},
		{"minimal (iOS, Web)", minimal(p), minimal(q), true},
		{"8 bytes", be(p, 8), be(q, 8), true},
		{"swapped", be(q, 4), be(p, 4), false},
		{"p + 2", be(p+2, 4), be(q, 4), false},
		{"1 and pq", be(1, 1), be(pq, 8), false},
		{"empty p", "", be(q, 4), false},
		{"9 bytes", "\x00" + be(p, 8), be(q, 4), false},
		{"the old fixed factors", "\x49\x4C\x55\x3B", "\x53\x91\x10\x73", false},
	} {
		if st.hasFactors(c.p, c.q) != c.ok {
			t.Errorf("%s: hasFactors = %v", c.name, !c.ok)
		}
	}
	if (&HandshakeStateCtx{}).hasFactors("\x00", "\x00") || (&HandshakeStateCtx{}).hasPQ("\x00") {
		t.Fatal("a state without pq matches zero")
	}
}

func BenchmarkNewPQ(b *testing.B) {
	for i := 0; i < b.N; i++ {
		newPQ()
	}
}
