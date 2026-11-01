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
	"crypto/x509"
	"testing"
)

// toBER writes der as a device does: every constructed value of indefinite
// length, every OCTET STRING longer than 8 bytes as a constructed one of
// two pieces. The signed attributes ([0] inside a SignerInfo) are kept DER,
// as RFC 5652 requires, by keeping values of keepTags as they are.
func toBER(t *testing.T, der []byte, keep func(v []byte) bool) []byte {
	t.Helper()
	var out []byte
	for len(der) > 0 {
		v, rest := firstValue(t, der)
		der = rest
		if keep(v) {
			out = append(out, v...)
			continue
		}
		header, content, err := splitDER(v)
		if err != nil {
			t.Fatal(err)
		}
		tag := header[0]
		switch {
		case tag == 0x04 && len(content) > 8:
			half := len(content) / 2
			out = append(out, 0x24, 0x80)
			out = append(out, encode([]byte{0x04}, content[:half])...)
			out = append(out, encode([]byte{0x04}, content[half:])...)
			out = append(out, 0, 0)
		case tag&0x20 != 0:
			out = append(out, tag, 0x80)
			out = append(out, toBER(t, content, keep)...)
			out = append(out, 0, 0)
		default:
			out = append(out, v...)
		}
	}
	return out
}

func firstValue(t *testing.T, b []byte) (v, rest []byte) {
	t.Helper()
	i := 2
	l := int(b[1])
	if l >= 0x80 {
		n := l & 0x7f
		l = 0
		for _, x := range b[2 : 2+n] {
			l = l<<8 | int(x)
		}
		i += n
	}
	return b[:i+l], b[i+l:]
}

func TestBERToDER(t *testing.T) {
	ch := newChain(t)
	all := []*x509.Certificate{ch.device.cert, ch.deviceCA.cert, ch.iphone.cert}
	der := signedDataDER(t, []byte(devicePlist), ch.device, all, nil)
	// Certificates and the signed attributes stay DER, as devices send them.
	keep := func(v []byte) bool {
		return v[0] == 0xa0 && bytes.Contains(v, oidMessageDigestDER) || isCertificate(v)
	}
	ber := toBER(t, der, keep)
	if bytes.Equal(ber, der) {
		t.Fatal("the BER form is the DER one")
	}
	got, err := berToDER(ber)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, der) {
		t.Fatalf("BER to DER differs from the original (%d vs %d bytes)", len(got), len(der))
	}
	if content, err := verifySignedData(ber, ch.root.cert); err != nil || string(content) != devicePlist {
		t.Fatalf("a device's BER answer: %q, %v", content, err)
	}

	for name, b := range map[string][]byte{
		"truncated":         ber[:len(ber)-3],
		"trailing bytes":    append(append([]byte{}, der...), 0x05, 0x00),
		"primitive, no end": {0x04, 0x80, 0x01},
		"no end of content": {0x30, 0x80, 0x02, 0x01, 0x01},
	} {
		if _, err := berToDER(b); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

var oidMessageDigestDER = []byte{0x06, 0x09, 0x2a, 0x86, 0x48, 0x86, 0xf7, 0x0d, 0x01, 0x09, 0x04}

func isCertificate(v []byte) bool {
	_, err := x509.ParseCertificate(v)
	return err == nil
}
