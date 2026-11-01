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
	"errors"
	"fmt"
)

// A device writes its CMS answer in BER, not DER: constructed values of
// indefinite length (0x80 ... 00 00), the content as a constructed OCTET
// STRING of pieces (RFC 5652 allows both, 5.1). encoding/asn1 reads DER
// only. berToDER rewrites the encoding as DER: definite lengths, and a
// constructed OCTET STRING as the one primitive string of its pieces. The
// signed attributes are DER already (RFC 5652, 5.4), so their bytes, which
// the signature covers, come out as they were.

const maxBERDepth = 32

var errBER = errors.New("not a BER encoding")

// berToDER returns der of the one value encoded in ber.
func berToDER(ber []byte) ([]byte, error) {
	out, rest, err := berValue(ber, 0)
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("%w: %d bytes after the value", errBER, len(rest))
	}
	return out, nil
}

// berValue converts the value at the start of b and returns it and what
// follows it.
func berValue(b []byte, depth int) (der, rest []byte, err error) {
	if depth > maxBERDepth {
		return nil, nil, fmt.Errorf("%w: nested too deep", errBER)
	}
	if len(b) < 2 {
		return nil, nil, fmt.Errorf("%w: truncated", errBER)
	}
	tagLen := 1
	if b[0]&0x1f == 0x1f { // high tag number: base-128 bytes
		for tagLen < len(b) && b[tagLen]&0x80 != 0 {
			tagLen++
		}
		tagLen++
		if tagLen >= len(b) {
			return nil, nil, fmt.Errorf("%w: truncated tag", errBER)
		}
	}
	tag := b[:tagLen]
	constructed := b[0]&0x20 != 0
	b = b[tagLen:]

	var content []byte
	switch l := b[0]; {
	case l == 0x80: // indefinite: constructed values until 00 00
		if !constructed {
			return nil, nil, fmt.Errorf("%w: a primitive value of indefinite length", errBER)
		}
		b = b[1:]
		var children [][]byte
		for {
			if len(b) >= 2 && b[0] == 0 && b[1] == 0 {
				b = b[2:]
				break
			}
			if len(b) == 0 {
				return nil, nil, fmt.Errorf("%w: no end of contents", errBER)
			}
			child, r, err := berValue(b, depth+1)
			if err != nil {
				return nil, nil, err
			}
			children = append(children, child)
			b = r
		}
		der, err = assembleBytes(tag, children)
		return der, b, err
	case l < 0x80:
		if int(l) > len(b)-1 {
			return nil, nil, fmt.Errorf("%w: truncated value", errBER)
		}
		content, rest = b[1:1+int(l)], b[1+int(l):]
	default:
		n := int(l & 0x7f)
		if n > 4 || len(b) < 1+n {
			return nil, nil, fmt.Errorf("%w: bad length", errBER)
		}
		length := 0
		for _, x := range b[1 : 1+n] {
			length = length<<8 | int(x)
		}
		if length < 0 || length > len(b)-1-n {
			return nil, nil, fmt.Errorf("%w: truncated value", errBER)
		}
		content, rest = b[1+n:1+n+length], b[1+n+length:]
	}
	if !constructed {
		return encode(tag, content), rest, nil
	}
	var children [][]byte
	for len(content) > 0 {
		child, r, err := berValue(content, depth+1)
		if err != nil {
			return nil, nil, err
		}
		children = append(children, child)
		content = r
	}
	der, err = assembleBytes(tag, children)
	return der, rest, err
}

// assembleBytes encodes a constructed value of children; a constructed
// OCTET STRING (universal 4) becomes the primitive string of its pieces.
func assembleBytes(tag []byte, children [][]byte) ([]byte, error) {
	if len(tag) == 1 && tag[0] == 0x24 {
		var s []byte
		for _, c := range children {
			if len(c) < 2 || c[0] != 0x04 {
				return nil, fmt.Errorf("%w: a piece of an OCTET STRING is no OCTET STRING", errBER)
			}
			_, body, err := splitDER(c)
			if err != nil {
				return nil, err
			}
			s = append(s, body...)
		}
		return encode([]byte{0x04}, s), nil
	}
	var content []byte
	for _, c := range children {
		content = append(content, c...)
	}
	return encode(tag, content), nil
}

// splitDER returns the header and the content of a DER value.
func splitDER(v []byte) (header, content []byte, err error) {
	if len(v) < 2 {
		return nil, nil, errBER
	}
	i := 1
	l := int(v[i])
	i++
	if l >= 0x80 {
		n := l & 0x7f
		if n > 4 || len(v) < i+n {
			return nil, nil, errBER
		}
		l = 0
		for _, x := range v[i : i+n] {
			l = l<<8 | int(x)
		}
		i += n
	}
	if len(v) != i+l {
		return nil, nil, errBER
	}
	return v[:i], v[i:], nil
}

// encode writes tag, the DER length of content and content.
func encode(tag, content []byte) []byte {
	out := append([]byte{}, tag...)
	n := len(content)
	switch {
	case n < 0x80:
		out = append(out, byte(n))
	default:
		var lb []byte
		for x := n; x > 0; x >>= 8 {
			lb = append([]byte{byte(x)}, lb...)
		}
		out = append(out, 0x80|byte(len(lb)))
		out = append(out, lb...)
	}
	return append(out, content...)
}
