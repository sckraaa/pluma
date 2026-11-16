package handshake

import (
	"math/big"
	"testing"

	"github.com/teamgram/proto/mtproto/crypto"
)

func TestIsGoodDHValue(t *testing.T) {
	one := big.NewInt(1)
	p := gBigIntDH2048P
	low := new(big.Int).Lsh(one, 2048-64)
	high := new(big.Int).Sub(p, low)
	add := func(a, b *big.Int) *big.Int { return new(big.Int).Add(a, b) }
	sub := func(a, b *big.Int) *big.Int { return new(big.Int).Sub(a, b) }
	gB := new(big.Int).Exp(gBigIntDH2048G, new(big.Int).SetBytes(crypto.GenerateNonce(256)), p)

	for _, c := range []struct {
		name string
		v    *big.Int
		ok   bool
	}{
		{"0", big.NewInt(0), false},
		{"1", one, false},
		{"2", big.NewInt(2), false},
		{"2^(2048-64) - 1", sub(low, one), false},
		{"2^(2048-64)", low, false},
		{"2^(2048-64) + 1", add(low, one), true},
		{"g^b", gB, true},
		{"dh_prime - 2^(2048-64) - 1", sub(high, one), true},
		{"dh_prime - 2^(2048-64)", high, false},
		{"dh_prime - 2^(2048-64) + 1", add(high, one), false},
		{"dh_prime - 1", sub(p, one), false},
		{"dh_prime", p, false},
		{"dh_prime + 1", add(p, one), false},
	} {
		if isGoodDHValue(c.v) != c.ok {
			t.Errorf("%s: isGoodDHValue = %v", c.name, !c.ok)
		}
	}
}

// TestDHGroupCopies: DHPrime and DHGenerator hand out copies, so a caller
// cannot change the group every handshake uses.
func TestDHGroupCopies(t *testing.T) {
	p, g := DHPrime(), DHGenerator()
	if p.Cmp(new(big.Int).SetBytes(dh2048P)) != 0 || g.Int64() != 3 {
		t.Fatal("wrong group")
	}
	p.SetInt64(5)
	g.SetInt64(7)
	if gBigIntDH2048P.Cmp(new(big.Int).SetBytes(dh2048P)) != 0 || gBigIntDH2048G.Int64() != 3 {
		t.Fatal("the group changed through a copy")
	}
}
