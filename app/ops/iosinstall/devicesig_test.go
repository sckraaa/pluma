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
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestAppleRootPinned(t *testing.T) {
	sum := sha256.Sum256(appleRootCA)
	if got := hex.EncodeToString(sum[:]); got != "b0b1730ecbc7ff4505142c49f1295e6eda6bcaed7e2c68c5be91b5a11001f024" {
		t.Fatalf("apple-root-ca.cer: SHA-256 %s", got)
	}
	root, err := x509.ParseCertificate(appleRootCA)
	if err != nil || root.Subject.CommonName != "Apple Root CA" || !root.IsCA {
		t.Fatalf("apple root: %v %v", root, err)
	}
}

// testCA is a CA of the test chain, root or intermediate.
type testCA struct {
	cert *x509.Certificate
	key  *rsa.PrivateKey
}

func newKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// issue makes a certificate for key, signed by parent (nil: self-signed),
// with SHA-1 as Apple's device CAs, valid in the past only, as an old
// device identity.
func issue(t *testing.T, name string, key *rsa.PrivateKey, parent *testCA, isCA bool, serial int64) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(serial),
		Subject:               pkix.Name{CommonName: name, Organization: []string{"Apple Inc."}},
		NotBefore:             time.Now().Add(-10 * 365 * 24 * time.Hour),
		NotAfter:              time.Now().Add(-365 * 24 * time.Hour),
		IsCA:                  isCA,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		SignatureAlgorithm:    x509.SHA256WithRSA,
	}
	signerCert, signerKey := tmpl, key
	if parent != nil {
		signerCert, signerKey = parent.cert, parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, signerCert, &key.PublicKey, signerKey)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// testChain is a root, the iPhone CA under it, the device CA under that and
// a device identity.
type testChain struct {
	root, iphone, deviceCA *testCA
	device                 *testCA
}

func newChain(t *testing.T) *testChain {
	ch := &testChain{}
	k := newKey(t)
	ch.root = &testCA{issue(t, "Apple Root CA", k, nil, true, 1), k}
	k = newKey(t)
	ch.iphone = &testCA{issue(t, "Apple iPhone Certification Authority", k, ch.root, true, 2), k}
	k = newKey(t)
	ch.deviceCA = &testCA{issue(t, "Apple iPhone Device CA", k, ch.iphone, true, 3), k}
	k = newKey(t)
	ch.device = &testCA{issue(t, "00008130-001A2B3C4D5E6F70", k, ch.deviceCA, false, 4), k}
	return ch
}

// signedDataDER builds the CMS SignedData a device sends: the plist,
// signed attributes with its SHA-1 digest, an RSA-SHA1 signature.
func signedDataDER(t *testing.T, content []byte, signer *testCA, certs []*x509.Certificate, digestOf []byte) []byte {
	t.Helper()
	if digestOf == nil {
		digestOf = content
	}
	digest := sha1.Sum(digestOf)
	md, _ := asn1.Marshal(digest[:])
	ct, _ := asn1.Marshal(oidData)
	attrs := []attribute{
		{Type: asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}, Values: asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSet, IsCompound: true, Bytes: ct}},
		{Type: oidMessageDigest, Values: asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSet, IsCompound: true, Bytes: md}},
	}
	set, err := asn1.MarshalWithParams(attrs, "set")
	if err != nil {
		t.Fatal(err)
	}
	h := sha1.Sum(set)
	sig, err := rsa.SignPKCS1v15(rand.Reader, signer.key, crypto.SHA1, h[:])
	if err != nil {
		t.Fatal(err)
	}
	var setBody asn1.RawValue
	if _, err := asn1.Unmarshal(set, &setBody); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	for _, c := range certs {
		raw = append(raw, c.Raw...)
	}
	sd := signedData{
		Version:          1,
		DigestAlgorithms: asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSet, IsCompound: true, Bytes: mustMarshal(t, algorithmIdentifier{Algorithm: oidSHA1})},
		EncapContentInfo: encapContentInfo{ContentType: oidData, Content: content},
		Certificates:     asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: raw},
		SignerInfos: []signerInfo{{
			Version:            1,
			SID:                issuerAndSerial{Issuer: asn1.RawValue{FullBytes: signer.cert.RawIssuer}, Serial: signer.cert.SerialNumber},
			DigestAlgorithm:    algorithmIdentifier{Algorithm: oidSHA1},
			SignedAttrs:        asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: setBody.Bytes},
			SignatureAlgorithm: algorithmIdentifier{Algorithm: asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1}},
			Signature:          sig,
		}},
	}
	inner := mustMarshal(t, sd)
	// contentInfo, with its [0] written out: Marshal writes FullBytes as is.
	return mustMarshal(t, struct {
		ContentType asn1.ObjectIdentifier
		Content     asn1.RawValue
	}{oidSignedData, asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: inner}})
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := asn1.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

const devicePlist = `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict><key>UDID</key><string>00008130-001A2B3C4D5E6F70</string></dict></plist>`

func TestVerifySignedData(t *testing.T) {
	ch := newChain(t)
	content := []byte(devicePlist)
	all := []*x509.Certificate{ch.device.cert, ch.deviceCA.cert, ch.iphone.cert}

	got, err := verifySignedData(signedDataDER(t, content, ch.device, all, nil), ch.root.cert)
	if err != nil || string(got) != devicePlist {
		t.Fatalf("a device's answer: %q, %v", got, err)
	}

	other := newChain(t)
	for name, c := range map[string]struct {
		der  []byte
		want string
	}{
		"another root":         {signedDataDER(t, content, other.device, []*x509.Certificate{other.device.cert, other.deviceCA.cert, other.iphone.cert}, nil), "not signed by an Apple device"},
		"a missing CA":         {signedDataDER(t, content, ch.device, []*x509.Certificate{ch.device.cert, ch.deviceCA.cert}, nil), `no issuer "Apple iPhone Certification Authority"`},
		"a changed content":    {signedDataDER(t, content, ch.device, all, []byte("other")), "message digest"},
		"a self-made identity": {signedDataDER(t, content, other.device, append([]*x509.Certificate{other.device.cert}, all[1:]...), nil), "not signed by an Apple device"},
		"no CMS":               {[]byte(devicePlist), "ContentInfo"},
	} {
		_, err := verifySignedData(c.der, ch.root.cert)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want an error with %q", name, err, c.want)
		}
	}

	// A leaf that is no CA cannot issue the device's certificate.
	k := newKey(t)
	fake := &testCA{issue(t, "Apple iPhone Device CA", k, ch.iphone, false, 9), k}
	k = newKey(t)
	dev := &testCA{issue(t, "device", k, fake, false, 10), k}
	_, err = verifySignedData(signedDataDER(t, content, dev, []*x509.Certificate{dev.cert, fake.cert, ch.iphone.cert}, nil), ch.root.cert)
	if !errors.Is(err, errNotApple) {
		t.Errorf("a non-CA issuer: %v", err)
	}
}
