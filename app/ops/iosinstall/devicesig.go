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

package main

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha1" //nolint:gosec // the device CAs of Apple sign with SHA-1
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"
)

// The answer of a device to a Profile Service profile is a CMS SignedData
// (RFC 5652) over a plist of the attributes the profile asked for, signed by
// the device's own identity: a certificate of "Apple iPhone Device CA",
// which chains to Apple Root CA ("Over-the-Air Profile Delivery and
// Configuration", Apple, phase 2). Apple's device CAs sign with SHA-1 and
// device certificates are kept long after they expire, so the chain is
// checked by hand: every signature up to the root, CA flags, no dates.
// crypto/x509 refuses SHA-1 signatures.

// appleRootCA is Apple Root CA (Apple Inc. Root), from
// https://www.apple.com/appleca/AppleIncRootCertificate.cer; its SHA-256 is
// B0B1730E CBC7FF45 05142C49 F1295E6E DA6BCAED 7E2C68C5 BE91B5A1 1001F024
// (pinned in the test).
//
//go:embed apple-root-ca.cer
var appleRootCA []byte

var (
	oidData          = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	oidSignedData    = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	oidMessageDigest = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}
	oidSHA1          = asn1.ObjectIdentifier{1, 3, 14, 3, 2, 26}
	oidSHA256        = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
)

type contentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue `asn1:"explicit,tag:0"`
}

type signedData struct {
	Version          int
	DigestAlgorithms asn1.RawValue
	EncapContentInfo encapContentInfo
	Certificates     asn1.RawValue `asn1:"optional,tag:0"`
	CRLs             asn1.RawValue `asn1:"optional,tag:1"`
	SignerInfos      []signerInfo  `asn1:"set"`
}

type encapContentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     []byte `asn1:"explicit,optional,tag:0"`
}

type signerInfo struct {
	Version            int
	SID                issuerAndSerial
	DigestAlgorithm    algorithmIdentifier
	SignedAttrs        asn1.RawValue `asn1:"optional,tag:0"`
	SignatureAlgorithm algorithmIdentifier
	Signature          []byte
	UnsignedAttrs      asn1.RawValue `asn1:"optional,tag:1"`
}

type issuerAndSerial struct {
	Issuer asn1.RawValue
	Serial *big.Int
}

type algorithmIdentifier struct {
	Algorithm  asn1.ObjectIdentifier
	Parameters asn1.RawValue `asn1:"optional"`
}

type attribute struct {
	Type   asn1.ObjectIdentifier
	Values asn1.RawValue `asn1:"set"`
}

var errNotApple = errors.New("not signed by an Apple device")

// verifyDeviceSignature checks that der is signed by a device identity of
// Apple and returns the signed content (the plist of attributes).
func verifyDeviceSignature(der []byte) ([]byte, error) {
	root, err := x509.ParseCertificate(appleRootCA)
	if err != nil {
		return nil, fmt.Errorf("apple root: %w", err)
	}
	return verifySignedData(der, root)
}

func verifySignedData(der []byte, root *x509.Certificate) ([]byte, error) {
	var ci contentInfo
	if rest, err := asn1.Unmarshal(der, &ci); err != nil || len(rest) != 0 {
		return nil, fmt.Errorf("signed data: not a CMS ContentInfo")
	}
	if !ci.ContentType.Equal(oidSignedData) {
		return nil, fmt.Errorf("signed data: content type %v", ci.ContentType)
	}
	var sd signedData
	if _, err := asn1.Unmarshal(ci.Content.Bytes, &sd); err != nil {
		return nil, fmt.Errorf("signed data: %w", err)
	}
	if !sd.EncapContentInfo.ContentType.Equal(oidData) || len(sd.EncapContentInfo.Content) == 0 {
		return nil, errors.New("signed data: no content")
	}
	content := sd.EncapContentInfo.Content
	if len(sd.SignerInfos) != 1 {
		return nil, fmt.Errorf("signed data: %d signers", len(sd.SignerInfos))
	}
	certs, err := x509.ParseCertificates(sd.Certificates.Bytes)
	if err != nil || len(certs) == 0 {
		return nil, errors.New("signed data: no certificates")
	}
	si := sd.SignerInfos[0]
	var signer *x509.Certificate
	for _, c := range certs {
		if c.SerialNumber.Cmp(si.SID.Serial) == 0 && bytes.Equal(c.RawIssuer, si.SID.Issuer.FullBytes) {
			signer = c
		}
	}
	if signer == nil {
		return nil, errors.New("signed data: the signer's certificate is missing")
	}

	hash, err := hashOf(si.DigestAlgorithm.Algorithm)
	if err != nil {
		return nil, err
	}
	signed := content
	if len(si.SignedAttrs.Bytes) > 0 {
		// The signature covers the attributes, DER-encoded as a SET (RFC
		// 5652, 5.4); their message digest covers the content.
		// The field is tagged [0] in place of SET: its bytes are the
		// attributes, wrapped back into a SET here.
		set, err := asn1.Marshal(asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSet, IsCompound: true, Bytes: si.SignedAttrs.Bytes})
		if err != nil {
			return nil, fmt.Errorf("signed attributes: %w", err)
		}
		var attrs []attribute
		if _, err := asn1.UnmarshalWithParams(set, &attrs, "set"); err != nil {
			return nil, fmt.Errorf("signed attributes: %w", err)
		}
		var digest []byte
		for _, a := range attrs {
			if a.Type.Equal(oidMessageDigest) {
				if _, err := asn1.Unmarshal(a.Values.Bytes, &digest); err != nil {
					return nil, fmt.Errorf("message digest: %w", err)
				}
			}
		}
		h := hash.New()
		h.Write(content)
		if digest == nil || !bytes.Equal(h.Sum(nil), digest) {
			return nil, errors.New("signed data: the message digest does not match the content")
		}
		signed = set
	}
	if err := checkSignature(signer.PublicKey, hash, signed, si.Signature); err != nil {
		return nil, fmt.Errorf("signed data: %w", err)
	}
	if err := chainsTo(signer, certs, root); err != nil {
		return nil, err
	}
	return content, nil
}

// chainsTo follows the issuers of cert among pool up to root, checking every
// signature and that every issuer is a CA. Validity dates are not checked:
// Apple tells to ignore them for device identities.
func chainsTo(cert *x509.Certificate, pool []*x509.Certificate, root *x509.Certificate) error {
	c := cert
	for depth := 0; depth < 5; depth++ {
		if bytes.Equal(c.RawIssuer, root.RawSubject) {
			if err := checkCertSignature(c, root); err != nil {
				return fmt.Errorf("%w: %v", errNotApple, err)
			}
			return nil
		}
		var parent *x509.Certificate
		for _, p := range pool {
			if p != c && bytes.Equal(p.RawSubject, c.RawIssuer) && p.IsCA {
				parent = p
				break
			}
		}
		if parent == nil {
			return fmt.Errorf("%w: no issuer %q", errNotApple, c.Issuer.CommonName)
		}
		if err := checkCertSignature(c, parent); err != nil {
			return fmt.Errorf("%w: %v", errNotApple, err)
		}
		c = parent
	}
	return fmt.Errorf("%w: chain too long", errNotApple)
}

func checkCertSignature(c, parent *x509.Certificate) error {
	if !parent.IsCA {
		return fmt.Errorf("%q is no CA", parent.Subject.CommonName)
	}
	var hash crypto.Hash
	switch c.SignatureAlgorithm {
	case x509.SHA1WithRSA, x509.ECDSAWithSHA1:
		hash = crypto.SHA1
	case x509.SHA256WithRSA, x509.ECDSAWithSHA256:
		hash = crypto.SHA256
	case x509.SHA384WithRSA, x509.ECDSAWithSHA384:
		hash = crypto.SHA384
	default:
		return fmt.Errorf("signature algorithm %v", c.SignatureAlgorithm)
	}
	return checkSignature(parent.PublicKey, hash, c.RawTBSCertificate, c.Signature)
}

func hashOf(oid asn1.ObjectIdentifier) (crypto.Hash, error) {
	switch {
	case oid.Equal(oidSHA1):
		return crypto.SHA1, nil
	case oid.Equal(oidSHA256):
		return crypto.SHA256, nil
	}
	return 0, fmt.Errorf("digest algorithm %v", oid)
}

func checkSignature(pub any, hash crypto.Hash, data, sig []byte) error {
	var digest []byte
	switch hash {
	case crypto.SHA1:
		s := sha1.Sum(data) //nolint:gosec // see the comment at the top
		digest = s[:]
	case crypto.SHA256:
		s := sha256.Sum256(data)
		digest = s[:]
	default:
		h := hash.New()
		h.Write(data)
		digest = h.Sum(nil)
	}
	switch k := pub.(type) {
	case *rsa.PublicKey:
		return rsa.VerifyPKCS1v15(k, hash, digest, sig)
	case *ecdsa.PublicKey:
		if !ecdsa.VerifyASN1(k, digest, sig) {
			return errors.New("bad ECDSA signature")
		}
		return nil
	}
	return fmt.Errorf("public key %T", pub)
}
