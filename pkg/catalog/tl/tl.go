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

// Package tl turns the objects of Telegram Web K (tweb) back into TL bytes
// of the server's layer, so that the server's own codec decodes them (task
// 024, ADR 0028). tweb returns API objects as JavaScript values: the
// predicate in "_", fields by their TL names, true flags in "pFlags", long
// as a number or a decimal string, bytes as Uint8Array, which the collector
// writes as {"$b64": …} (a file_reference tweb keeps as an array of
// numbers). Mapping such values onto the server's Go types by
// field names would have to guess between fields of one name; serializing
// them by the schema they came from and decoding the bytes leaves nothing
// to guess.
//
// api.tl is the schema of layer 228, the layer the catalog collections are
// gathered with (the server serves several layers, ADR 0045), from tdesktop v7.0.9
// (Telegram/SourceFiles/mtproto/scheme/api.tl).
package tl

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/teamgram/proto/mtproto"
)

//go:embed api.tl
var apiTL string

// Layer is the layer of the embedded schema.
const Layer = 228

const (
	idVector    = 0x1cb5c415
	idBoolTrue  = 0x997275b5
	idBoolFalse = 0xbc799737
)

// param is a field of a constructor.
type param struct {
	name string
	typ  string // "int", "long", "Vector<Document>", "#", "true"…
	flag string // the flags field it depends on, "" if none
	bit  uint
}

// Constructor is a constructor of the schema.
type Constructor struct {
	Name   string
	ID     uint32
	Result string
	params []param
}

// Schema is the set of constructors of a TL schema (types, not methods).
type Schema struct {
	byName map[string]*Constructor
}

// Default is the schema of the server's layer.
var Default = mustParse(apiTL)

func mustParse(src string) *Schema {
	s, err := Parse(src)
	if err != nil {
		panic(err)
	}
	return s
}

// Parse reads the constructors of a TL schema, up to its functions.
func Parse(src string) (*Schema, error) {
	s := &Schema{byName: map[string]*Constructor{}}
	for n, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "---functions---") {
			break
		}
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "---") {
			continue
		}
		c, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("tl: line %d: %w", n+1, err)
		}
		if c == nil {
			continue
		}
		s.byName[c.Name] = c
	}
	if s.byName["boolTrue"] == nil || s.byName["document"] == nil {
		return nil, errors.New("tl: not an API schema")
	}
	return s, nil
}

// parseLine reads "name#id a:int flags:# b:flags.0?string = Type;"; the
// built-in vector line (with {t:Type}) gives nil.
func parseLine(line string) (*Constructor, error) {
	line = strings.TrimSuffix(line, ";")
	lhs, result, ok := strings.Cut(line, " = ")
	if !ok {
		return nil, fmt.Errorf("no result type in %q", line)
	}
	fields := strings.Fields(lhs)
	name, hexID, ok := strings.Cut(fields[0], "#")
	if !ok {
		return nil, fmt.Errorf("no id in %q", fields[0])
	}
	if strings.Contains(lhs, "{") {
		return nil, nil
	}
	id, err := strconv.ParseUint(hexID, 16, 32)
	if err != nil {
		return nil, err
	}
	c := &Constructor{Name: name, ID: uint32(id), Result: strings.TrimSpace(result)}
	for _, f := range fields[1:] {
		pname, ptype, ok := strings.Cut(f, ":")
		if !ok {
			return nil, fmt.Errorf("bad field %q", f)
		}
		p := param{name: pname, typ: ptype}
		if cond, t, ok := strings.Cut(ptype, "?"); ok {
			flag, bit, ok := strings.Cut(cond, ".")
			if !ok {
				return nil, fmt.Errorf("bad condition %q", cond)
			}
			b, err := strconv.ParseUint(bit, 10, 5)
			if err != nil {
				return nil, err
			}
			p.typ, p.flag, p.bit = t, flag, uint(b)
		}
		c.params = append(c.params, p)
	}
	return c, nil
}

// Constructor returns the constructor of name, nil if the schema has none.
func (s *Schema) Constructor(name string) *Constructor {
	return s.byName[name]
}

// EncodeJSON serializes the tweb object in data (JSON) into TL bytes.
func (s *Schema) EncodeJSON(data []byte) ([]byte, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	return s.Encode(v)
}

// Encode serializes a tweb object, decoded from JSON with UseNumber, into
// TL bytes.
func (s *Schema) Encode(v any) ([]byte, error) {
	var b bytes.Buffer
	if err := s.object(&b, v, "object"); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// Decode serializes a tweb object and decodes it with the server's codec.
func (s *Schema) Decode(v any) (mtproto.TLObject, error) {
	b, err := s.Encode(v)
	if err != nil {
		return nil, err
	}
	d := mtproto.NewDecodeBuf(b)
	o := d.Object()
	if err := d.GetError(); err != nil {
		return nil, err
	}
	if o == nil {
		return nil, errors.New("tl: nothing decoded")
	}
	if d.GetOffset() != len(b) {
		return nil, fmt.Errorf("tl: %d of %d bytes decoded", d.GetOffset(), len(b))
	}
	return o, nil
}

// object writes a boxed object: its constructor id and its fields.
func (s *Schema) object(b *bytes.Buffer, v any, path string) error {
	m, ok := v.(map[string]any)
	if !ok {
		return fmt.Errorf("tl: %s: an object expected, %T found", path, v)
	}
	name, _ := m["_"].(string)
	c := s.byName[name]
	if c == nil {
		return fmt.Errorf("tl: %s: constructor %q not in layer %d", path, name, Layer)
	}
	putUint32(b, c.ID)
	return s.fields(b, c, m, path+"."+name)
}

func (s *Schema) fields(b *bytes.Buffer, c *Constructor, m map[string]any, path string) error {
	pflags, _ := m["pFlags"].(map[string]any)
	present := func(p param) bool {
		if p.typ == "true" {
			return truthy(pflags[p.name]) || truthy(m[p.name])
		}
		v, ok := m[p.name]
		return ok && v != nil
	}
	for _, p := range c.params {
		if p.typ == "#" {
			var flags uint32
			for _, q := range c.params {
				if q.flag == p.name && present(q) {
					flags |= 1 << q.bit
				}
			}
			putUint32(b, flags)
			continue
		}
		if p.flag != "" && !present(p) {
			continue
		}
		if p.typ == "true" {
			continue
		}
		v, ok := m[p.name]
		if !ok {
			return fmt.Errorf("tl: %s: no field %s", path, p.name)
		}
		if err := s.value(b, p.typ, v, path+"."+p.name); err != nil {
			return err
		}
	}
	return nil
}

func truthy(v any) bool {
	t, ok := v.(bool)
	return ok && t
}

// value writes v as TL type typ.
func (s *Schema) value(b *bytes.Buffer, typ string, v any, path string) error {
	switch typ {
	case "int":
		n, err := integer(v, 32)
		if err != nil {
			return fmt.Errorf("tl: %s: %w", path, err)
		}
		putUint32(b, uint32(int32(n)))
	case "long":
		n, err := integer(v, 64)
		if err != nil {
			return fmt.Errorf("tl: %s: %w", path, err)
		}
		var x [8]byte
		binary.LittleEndian.PutUint64(x[:], uint64(n))
		b.Write(x[:])
	case "double":
		f, err := number(v)
		if err != nil {
			return fmt.Errorf("tl: %s: %w", path, err)
		}
		var x [8]byte
		binary.LittleEndian.PutUint64(x[:], math.Float64bits(f))
		b.Write(x[:])
	case "string":
		str, ok := v.(string)
		if !ok {
			return fmt.Errorf("tl: %s: a string expected, %T found", path, v)
		}
		putBytes(b, []byte(str))
	case "bytes":
		raw, err := byteValue(v)
		if err != nil {
			return fmt.Errorf("tl: %s: %w", path, err)
		}
		putBytes(b, raw)
	case "int128", "int256":
		raw, err := byteValue(v)
		if err != nil {
			return fmt.Errorf("tl: %s: %w", path, err)
		}
		if want := map[string]int{"int128": 16, "int256": 32}[typ]; len(raw) != want {
			return fmt.Errorf("tl: %s: %d bytes for %s", path, len(raw), typ)
		}
		b.Write(raw)
	case "Bool":
		t, ok := v.(bool)
		if !ok {
			return fmt.Errorf("tl: %s: a boolean expected, %T found", path, v)
		}
		if t {
			putUint32(b, idBoolTrue)
		} else {
			putUint32(b, idBoolFalse)
		}
	default:
		if elem, ok := strings.CutPrefix(typ, "Vector<"); ok {
			elem = strings.TrimSuffix(elem, ">")
			list, ok := v.([]any)
			if !ok {
				return fmt.Errorf("tl: %s: a list expected, %T found", path, v)
			}
			putUint32(b, idVector)
			putUint32(b, uint32(len(list)))
			for i, e := range list {
				if err := s.value(b, elem, e, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
			return nil
		}
		// A boxed type: its name, after any namespace, is capitalized
		// (Document, help.PeerColorOption).
		name := typ[strings.LastIndexByte(typ, '.')+1:]
		if name == "" || name[0] < 'A' || name[0] > 'Z' {
			return fmt.Errorf("tl: %s: type %q not supported", path, typ)
		}
		return s.object(b, v, path)
	}
	return nil
}

// integer reads a JSON number or a decimal string as a signed integer of
// the given bits; tweb writes a long beyond 2^53 as a string.
func integer(v any, bits int) (int64, error) {
	var str string
	switch x := v.(type) {
	case json.Number:
		str = x.String()
	case string:
		str = x
	default:
		return 0, fmt.Errorf("a number expected, %T found", v)
	}
	n, err := strconv.ParseInt(str, 10, bits)
	if err != nil {
		return 0, err
	}
	return n, nil
}

func number(v any) (float64, error) {
	x, ok := v.(json.Number)
	if !ok {
		return 0, fmt.Errorf("a number expected, %T found", v)
	}
	return x.Float64()
}

// byteValue reads bytes written as {"$b64": …}, or as the array of
// numbers tweb keeps a file_reference as.
func byteValue(v any) ([]byte, error) {
	if list, ok := v.([]any); ok {
		out := make([]byte, len(list))
		for i, e := range list {
			n, err := integer(e, 16)
			if err != nil || n < 0 || n > 255 {
				return nil, fmt.Errorf("byte %d of an array: %v", i, e)
			}
			out[i] = byte(n)
		}
		return out, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf(`{"$b64": …} or an array of bytes expected, %T found`, v)
	}
	s, ok := m["$b64"].(string)
	if !ok || len(m) != 1 {
		return nil, errors.New(`{"$b64": …} expected`)
	}
	return base64.StdEncoding.DecodeString(s)
}

func putUint32(b *bytes.Buffer, v uint32) {
	var x [4]byte
	binary.LittleEndian.PutUint32(x[:], v)
	b.Write(x[:])
}

// putBytes writes TL bytes: a length of one byte (under 254) or of four
// with the marker 254, the data, and padding to four bytes.
func putBytes(b *bytes.Buffer, data []byte) {
	n := len(data)
	head := 1
	if n < 254 {
		b.WriteByte(byte(n))
	} else {
		b.WriteByte(254)
		b.WriteByte(byte(n))
		b.WriteByte(byte(n >> 8))
		b.WriteByte(byte(n >> 16))
		head = 4
	}
	b.Write(data)
	for pad := (head + n) % 4; pad != 0 && pad < 4; pad++ {
		b.WriteByte(0)
	}
}
