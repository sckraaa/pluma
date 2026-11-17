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

package statsgraph

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/teamgram/proto/mtproto"
)

// Token is what a zoom_token of a graph stands for (ADR 0066, p. 2): the
// graph, whose it is and for whom, signed so that stats.loadAsyncGraph
// needs no state. The handler checks Viewer against the user of the
// request and the viewer's rights again; the token only says the server
// gave it.
type Token struct {
	// Kind of graph, the name the statistics give it; 1 to MaxTokenField
	// bytes.
	Kind string
	// PeerType and PeerID of the channel, the group or the poll's chat.
	PeerType int32
	PeerID   int64
	// Object: the post, the story or the poll, 0 for the graphs of a
	// channel or a group.
	Object int64
	// Viewer is the user the token was given to.
	Viewer int64
	// Lang and Dark the graph was written in: loadAsyncGraph carries
	// neither. Lang is at most MaxTokenField bytes.
	Lang string
	Dark bool
	// Expires is the unix time the token stops being valid at.
	Expires int64
}

const (
	// TokenLifetime is how long a token is valid, in seconds (ADR 0066,
	// p. 2): a statistics screen kept open for a day still zooms; after it
	// the client gets GRAPH_EXPIRED_RELOAD and loads the statistics again.
	TokenLifetime = 24 * 3600
	// MaxTokenField is the length of Kind and Lang a token holds.
	MaxTokenField = 32
	// MinKeyBytes is the length of the key: the length of HMAC-SHA256's
	// output (RFC 2104, section 3).
	MinKeyBytes = 32
	// maxTokenLen bounds what Verify decodes: a token of this package is
	// under 200 characters.
	maxTokenLen  = 512
	tokenVersion = "v1"
)

// Signer makes and checks the tokens with one key.
type Signer struct {
	key []byte
}

// ParseKey reads the key of the tokens from the environment's form: the
// standard base64 of at least MinKeyBytes random bytes (`openssl rand
// -base64 32`). The errors never hold the key.
func ParseKey(s string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, errors.New("statsgraph: the key is not base64")
	}
	if len(key) < MinKeyBytes {
		return nil, fmt.Errorf("statsgraph: the key has %d bytes, at least %d", len(key), MinKeyBytes)
	}
	return key, nil
}

// NewSigner returns a Signer of key, at least MinKeyBytes long.
func NewSigner(key []byte) (*Signer, error) {
	if len(key) < MinKeyBytes {
		return nil, fmt.Errorf("statsgraph: the key has %d bytes, at least %d", len(key), MinKeyBytes)
	}
	return &Signer{key: append([]byte(nil), key...)}, nil
}

// Sign returns the token of t: "v1.", the base64url of its fields, ".",
// the base64url of their HMAC-SHA256.
func (s *Signer) Sign(t Token) (string, error) {
	if t.Kind == "" || len(t.Kind) > MaxTokenField || len(t.Lang) > MaxTokenField {
		return "", fmt.Errorf("statsgraph: token of kind %q, language %q", t.Kind, t.Lang)
	}
	payload := base64.RawURLEncoding.EncodeToString(t.encode())
	return tokenVersion + "." + payload + "." + base64.RawURLEncoding.EncodeToString(s.mac(tokenVersion, payload)), nil
}

// Verify returns the token of str at unix time now, or the error of
// stats.loadAsyncGraph to answer with: GRAPH_INVALID_RELOAD for a token
// this key did not sign, GRAPH_OUTDATED_RELOAD for one of another version
// of the format, GRAPH_EXPIRED_RELOAD for one past its time.
func (s *Signer) Verify(str string, now int64) (Token, error) {
	if len(str) > maxTokenLen {
		return Token{}, mtproto.ErrGraphInvalidReload
	}
	parts := strings.Split(str, ".")
	if len(parts) != 3 || !versionRe(parts[0]) {
		return Token{}, mtproto.ErrGraphInvalidReload
	}
	if parts[0] != tokenVersion {
		return Token{}, mtproto.ErrGraphOutdatedReload
	}
	sum, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sum, s.mac(parts[0], parts[1])) {
		return Token{}, mtproto.ErrGraphInvalidReload
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Token{}, mtproto.ErrGraphInvalidReload
	}
	t, ok := decodeToken(raw)
	if !ok {
		// Signed by this key but not readable: a bug of the format, never
		// a client's doing.
		return Token{}, mtproto.ErrGraphInvalidReload
	}
	if now >= t.Expires {
		return Token{}, mtproto.ErrGraphExpiredReload
	}
	return t, nil
}

// versionRe tells whether v has the form of a version: "v" and digits.
func versionRe(v string) bool {
	if len(v) < 2 || len(v) > 4 || v[0] != 'v' {
		return false
	}
	for _, c := range v[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// mac is the HMAC-SHA256 of the token's version and fields, under a label
// of its own, so the key signs nothing else by accident.
func (s *Signer) mac(version, payload string) []byte {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte("statsgraph zoom token\x00"))
	m.Write([]byte(version))
	m.Write([]byte{'.'})
	m.Write([]byte(payload))
	return m.Sum(nil)
}

// encode writes the fields of t: kind (length and bytes), peer type, peer
// id, object, viewer, language (length and bytes), dark, expiry; integers
// big-endian.
func (t Token) encode() []byte {
	b := make([]byte, 0, 2+len(t.Kind)+len(t.Lang)+4+8*4+1)
	b = append(b, byte(len(t.Kind)))
	b = append(b, t.Kind...)
	b = binary.BigEndian.AppendUint32(b, uint32(t.PeerType))
	b = binary.BigEndian.AppendUint64(b, uint64(t.PeerID))
	b = binary.BigEndian.AppendUint64(b, uint64(t.Object))
	b = binary.BigEndian.AppendUint64(b, uint64(t.Viewer))
	b = append(b, byte(len(t.Lang)))
	b = append(b, t.Lang...)
	if t.Dark {
		b = append(b, 1)
	} else {
		b = append(b, 0)
	}
	return binary.BigEndian.AppendUint64(b, uint64(t.Expires))
}

func decodeToken(b []byte) (t Token, ok bool) {
	str := func() (string, bool) {
		if len(b) < 1 || int(b[0]) > MaxTokenField || len(b) < 1+int(b[0]) {
			return "", false
		}
		n := int(b[0])
		s := string(b[1 : 1+n])
		b = b[1+n:]
		return s, true
	}
	u64 := func() (uint64, bool) {
		if len(b) < 8 {
			return 0, false
		}
		v := binary.BigEndian.Uint64(b)
		b = b[8:]
		return v, true
	}

	if t.Kind, ok = str(); !ok || t.Kind == "" {
		return Token{}, false
	}
	if len(b) < 4 {
		return Token{}, false
	}
	t.PeerType = int32(binary.BigEndian.Uint32(b))
	b = b[4:]
	var v [3]uint64
	for i := range v {
		if v[i], ok = u64(); !ok {
			return Token{}, false
		}
	}
	t.PeerID, t.Object, t.Viewer = int64(v[0]), int64(v[1]), int64(v[2])
	if t.Lang, ok = str(); !ok {
		return Token{}, false
	}
	if len(b) < 1 || b[0] > 1 {
		return Token{}, false
	}
	t.Dark = b[0] == 1
	b = b[1:]
	exp, ok := u64()
	if !ok || len(b) != 0 {
		return Token{}, false
	}
	t.Expires = int64(exp)
	return t, true
}
