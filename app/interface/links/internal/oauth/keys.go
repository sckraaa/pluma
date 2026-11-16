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

package oauth

import (
	"bytes"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
)

// minKeyBits is the smallest RSA key the provider signs with (RFC 7518
// 3.3: "A key of size 2048 bits or larger MUST be used with these
// algorithms").
const minKeyBits = 2048

// signingKey is an RSA key of the ID tokens with its key id.
type signingKey struct {
	key *rsa.PrivateKey
	kid string
}

// Keys are the keys of the provider: the first signs the ID tokens, every
// one is published in the JWKS (a replaced key stays there while the ID
// tokens it signed may still be checked, docs/ops/bots.md).
type Keys struct {
	keys []signingKey
	// binding ties the page of a login request to the browser it was shown
	// in (Provider.bindingOf).
	binding []byte
}

// LoadKeys reads the keys of the file at path, PEM blocks of RSA private
// keys (PKCS #8 from openssl genpkey, or PKCS #1). A file without any (an
// empty one) is (nil, nil): the provider is off. Anything else that is no
// RSA key of at least 2048 bits is an error, which stops the service.
func LoadKeys(path string) (*Keys, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("oauth: read the signing keys: %w", err)
	}
	return ParseKeys(data)
}

// ParseKeys is LoadKeys of the contents of the file.
func ParseKeys(data []byte) (*Keys, error) {
	var keys []signingKey
	for rest := data; ; {
		var b *pem.Block
		b, rest = pem.Decode(rest)
		if b == nil {
			if len(bytes.TrimSpace(rest)) != 0 {
				return nil, errors.New("oauth: the signing key file has data that is no PEM block")
			}
			break
		}
		k, err := parseKey(b)
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	for _, k := range keys {
		if seen[k.kid] {
			return nil, errors.New("oauth: the signing key file has a key twice")
		}
		seen[k.kid] = true
	}
	// The binding key is derived from the signing key, so that it needs no
	// secret of its own; a new signing key invalidates only the pages of
	// requests open at the time (urlauth.RequestTTL).
	der, err := x509.MarshalPKCS8PrivateKey(keys[0].key)
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, der)
	mac.Write([]byte("pluma oauth page binding"))
	return &Keys{keys: keys, binding: mac.Sum(nil)}, nil
}

func parseKey(b *pem.Block) (signingKey, error) {
	var (
		key *rsa.PrivateKey
		err error
	)
	switch b.Type {
	case "PRIVATE KEY":
		var k any
		if k, err = x509.ParsePKCS8PrivateKey(b.Bytes); err == nil {
			var ok bool
			if key, ok = k.(*rsa.PrivateKey); !ok {
				return signingKey{}, fmt.Errorf("oauth: a signing key is %T, not RSA", k)
			}
		}
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(b.Bytes)
	default:
		return signingKey{}, fmt.Errorf("oauth: a PEM block %q is no private key", b.Type)
	}
	if err != nil {
		return signingKey{}, fmt.Errorf("oauth: parse a signing key: %w", err)
	}
	if key.N.BitLen() < minKeyBits {
		return signingKey{}, fmt.Errorf("oauth: a signing key of %d bits, want at least %d", key.N.BitLen(), minKeyBits)
	}
	kid, err := keyId(&key.PublicKey)
	if err != nil {
		return signingKey{}, err
	}
	return signingKey{key: key, kid: kid}, nil
}

// keyId is the kid of a key: SHA-256 of its public key (the DER of its
// SubjectPublicKeyInfo) in base64url (task 106, «Провайдер входа»).
func keyId(pub *rsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(der)
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// jwk is a public key in the JWKS (RFC 7517, RFC 7518 6.3.1).
type jwk struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// JWKS is the document of /.well-known/jwks.json.
func (k *Keys) JWKS() ([]byte, error) {
	set := struct {
		Keys []jwk `json:"keys"`
	}{}
	for _, s := range k.keys {
		pub := s.key.PublicKey
		set.Keys = append(set.Keys, jwk{
			Kty: "RSA", Use: "sig", Alg: "RS256", Kid: s.kid,
			N: base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		})
	}
	return json.Marshal(set)
}

// Sign returns claims as a JWS of the compact serialization signed with
// RS256 (RFC 7515, RFC 7518 3.3) by the first key: an ID token.
func (k *Keys) Sign(claims any) (string, error) {
	s := k.keys[0]
	header, err := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": s.kid})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	input := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}
