package mtclient

import (
	"bytes"
	"crypto/rand"
	"errors"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/crypto"
)

// The client side of the 2FA password (https://core.telegram.org/api/srp),
// as official clients compute it. The password is never sent: a new one
// goes as its verifier, a check as an SRP proof.

// ErrPasswordAlgo is returned for an algorithm other than
// passwordKdfAlgoSHA256SHA256PBKDF2HMACSHA512iter100000SHA256ModPow, or SRP
// values that fail the client's checks.
var ErrPasswordAlgo = errors.New("mtclient: unsupported or unsafe password algorithm")

// NewPasswordAlgo returns new_algo of account.password ready for a new
// password: its salt1 with 32 random bytes appended, as official clients do.
func NewPasswordAlgo(newAlgo *mtproto.PasswordKdfAlgo) (*mtproto.PasswordKdfAlgo, error) {
	if newAlgo.GetPredicateName() != mtproto.Predicate_passwordKdfAlgoModPow {
		return nil, ErrPasswordAlgo
	}
	salt1 := make([]byte, len(newAlgo.GetSalt1())+32)
	copy(salt1, newAlgo.GetSalt1())
	if _, err := rand.Read(salt1[len(newAlgo.GetSalt1()):]); err != nil {
		return nil, err
	}
	return mtproto.MakeTLPasswordKdfAlgoModPow(&mtproto.PasswordKdfAlgo{
		Salt1: salt1,
		Salt2: bytes.Clone(newAlgo.GetSalt2()),
		G:     newAlgo.GetG(),
		P:     bytes.Clone(newAlgo.GetP()),
	}).To_PasswordKdfAlgo(), nil
}

// PasswordHash returns new_password_hash of password under algo: the
// verifier v = g^x mod p, 256 bytes.
func PasswordHash(algo *mtproto.PasswordKdfAlgo, password string) ([]byte, error) {
	srp, err := srpOf(algo)
	if err != nil {
		return nil, err
	}
	return srp.GetVBytes(algo.GetSalt1(), []byte(password)), nil
}

// CheckPassword returns the proof of password for account.password p (its
// current_algo, srp_B and srp_id): inputCheckPasswordSRP with A and M1.
func CheckPassword(p *mtproto.Account_Password, password string) (*mtproto.InputCheckPasswordSRP, error) {
	if !p.GetHasPassword() || p.GetSrpId() == nil {
		return nil, errors.New("mtclient: the account has no password")
	}
	algo := p.GetCurrentAlgo()
	srp, err := srpOf(algo)
	if err != nil {
		return nil, err
	}
	x := srp.GetX(algo.GetSalt1(), []byte(password))
	A, M1 := srp.CalcClientM(algo.GetSalt1(), x, p.GetSrp_B())
	if A == nil || M1 == nil {
		return nil, ErrPasswordAlgo
	}
	return mtproto.MakeTLInputCheckPasswordSRP(&mtproto.InputCheckPasswordSRP{
		SrpId: p.GetSrpId().GetValue(),
		A:     A,
		M1:    M1,
	}).To_InputCheckPasswordSRP(), nil
}

// NoPassword is inputCheckPasswordEmpty, the current password of an account
// without one.
func NoPassword() *mtproto.InputCheckPasswordSRP {
	return mtproto.MakeTLInputCheckPasswordEmpty(nil).To_InputCheckPasswordSRP()
}

func srpOf(algo *mtproto.PasswordKdfAlgo) (*crypto.SRPUtil, error) {
	if algo.GetPredicateName() != mtproto.Predicate_passwordKdfAlgoModPow || len(algo.GetP()) != 256 {
		return nil, ErrPasswordAlgo
	}
	return crypto.MakeSRPUtil(&crypto.PasswordKdfAlgoModPow{
		Salt1: algo.GetSalt1(),
		Salt2: algo.GetSalt2(),
		G:     algo.GetG(),
		P:     algo.GetP(),
	}), nil
}
