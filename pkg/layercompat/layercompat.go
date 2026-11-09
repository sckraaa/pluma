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

// Package layercompat turns an object the server built into what one client
// session can read (ADR 0045). The codec chooses the constructor of a layer by
// itself, so only two things are left: a constructor the layer does not have,
// and the keyboards of layer 229, which changed shape rather than gained fields.
//
// Replies and pushes are built once and shared, so For never changes its
// argument: what must change is changed on a copy.
package layercompat

import (
	"errors"
	"fmt"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// layerInlineButtons is the first layer whose inline keyboards are
// keyboardInlineButton rows and whose buttons carry a ButtonType.
const layerInlineButtons = 229

// layerRichQuote is the first layer that reads quote_rich of a reply header.
const layerRichQuote = 232

// ErrNotRepresentable says that an object has no form in the layer and nothing
// to fall back to. It does not leave this package's caller as a partial body.
var ErrNotRepresentable = errors.New("object cannot be represented in this layer")

// For returns object as a session of the layer can read it: object itself when
// nothing has to change, otherwise a converted copy. It drops or replaces the
// parts the layer has no constructor for: media, actions and entities have an
// "unsupported" form of their own, an element of a vector is left out, an
// optional field is cleared. Whatever still does not encode is an error.
func For(object mtproto.TLObject, layer int32) (mtproto.TLObject, error) {
	if r, ok := object.(*mtproto.TLRpcResult); ok {
		result, err := For(r.Result, layer)
		if err != nil {
			return nil, err
		}
		return &mtproto.TLRpcResult{ReqMsgId: r.ReqMsgId, Result: result}, nil
	}
	m, ok := object.(proto.Message)
	if !ok || layer <= 0 {
		return object, nil
	}
	if !needsChange(m.ProtoReflect(), layer) {
		return object, nil
	}
	clone := proto.Clone(m)
	if !fixChild(clone.ProtoReflect(), layer) {
		return nil, fmt.Errorf("%T: %w", object, ErrNotRepresentable)
	}
	converted, ok := clone.(mtproto.TLObject)
	if !ok {
		return nil, fmt.Errorf("%T: %w", object, ErrNotRepresentable)
	}
	return converted, nil
}

// EncodeFor is For followed by Encode into a pooled buffer. It returns the body
// or an error, never a part of a body.
func EncodeFor(object mtproto.TLObject, layer int32) ([]byte, error) {
	o, err := For(object, layer)
	if err != nil {
		return nil, err
	}
	x := mtproto.GetEncodeBuf()
	defer mtproto.PutEncodeBuf(x)
	if err := o.Encode(x, layer); err != nil {
		return nil, err
	}
	return append([]byte(nil), x.GetBuf()...), nil
}

// constructorOf returns the predicate of a TL union object; ok is false when m
// is not one (wrappers, method arguments, vectors).
func constructorOf(m protoreflect.Message) (predicate string, ok bool) {
	fd := m.Descriptor().Fields().ByName("predicate_name")
	if fd == nil {
		return "", false
	}
	predicate = m.Get(fd).String()
	if predicate != "" {
		return predicate, true
	}
	if cfd := m.Descriptor().Fields().ByName("constructor"); cfd != nil && cfd.Kind() == protoreflect.EnumKind {
		return mtproto.GetClazzNameById(int32(m.Get(cfd).Enum())), true
	}
	return "", true
}

// representable says whether the layer has the constructor.
func representable(predicate string, layer int32) bool {
	return predicate != "" && mtproto.GetClazzID(predicate, int(layer)) != 0
}

func needsChange(m protoreflect.Message, layer int32) bool {
	if predicate, ok := constructorOf(m); ok && !representable(predicate, layer) {
		return true
	}
	switch v := m.Interface().(type) {
	case *mtproto.ReplyMarkup:
		if markupNeedsChange(v, layer) {
			return true
		}
	case *mtproto.MessageReplyHeader:
		if layer < layerRichQuote && len(v.GetQuoteRich()) != 0 && v.GetQuoteText() == nil {
			return true
		}
	}
	found := false
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Message() == nil || fd.IsMap() {
			return true
		}
		if fd.IsList() {
			l := v.List()
			for i := 0; i < l.Len(); i++ {
				if needsChange(l.Get(i).Message(), layer) {
					found = true
					return false
				}
			}
			return true
		}
		if needsChange(v.Message(), layer) {
			found = true
			return false
		}
		return true
	})
	return found
}

// fixChild makes one object fit the layer and reports whether it does.
func fixChild(m protoreflect.Message, layer int32) bool {
	if predicate, ok := constructorOf(m); ok && !representable(predicate, layer) {
		if !convert(m, layer) {
			return false
		}
	}
	switch v := m.Interface().(type) {
	case *mtproto.ReplyMarkup:
		convertMarkup(v, layer)
	case *mtproto.MessageReplyHeader:
		if layer < layerRichQuote && len(v.GetQuoteRich()) != 0 && v.GetQuoteText() == nil {
			v.QuoteText = wrapperspb.String(flatRichText(v.GetQuoteRich()))
		}
	}

	type slot struct {
		fd protoreflect.FieldDescriptor
		v  protoreflect.Value
	}
	var slots []slot
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Message() != nil && !fd.IsMap() {
			slots = append(slots, slot{fd, v})
		}
		return true
	})
	cleared := false
	for _, s := range slots {
		if s.fd.IsList() {
			l := m.Mutable(s.fd).List()
			n := 0
			for i := 0; i < l.Len(); i++ {
				if fixChild(l.Get(i).Message(), layer) {
					l.Set(n, l.Get(i))
					n++
				}
			}
			l.Truncate(n)
			continue
		}
		if !fixChild(m.Mutable(s.fd).Message(), layer) {
			m.Clear(s.fd)
			cleared = true
		}
	}
	if cleared {
		// The field cleared may have been required: then this object has
		// no form either, and its parent decides what becomes of it.
		o, ok := m.Interface().(mtproto.TLObject)
		if !ok {
			return false
		}
		x := mtproto.GetEncodeBuf()
		defer mtproto.PutEncodeBuf(x)
		return o.Encode(x, layer) == nil
	}
	return true
}

// convert replaces an object the layer does not know with what the layer has
// for it, in place. false: nothing to replace it with.
func convert(m protoreflect.Message, layer int32) bool {
	switch v := m.Interface().(type) {
	case *mtproto.MessageMedia:
		proto.Reset(v)
		mtproto.MakeTLMessageMediaUnsupported(v)
		return representable(mtproto.Predicate_messageMediaUnsupported, layer)
	case *mtproto.MessageAction:
		proto.Reset(v)
		mtproto.MakeTLMessageActionEmpty(v)
		return representable(mtproto.Predicate_messageActionEmpty, layer)
	case *mtproto.MessageEntity:
		offset, length := v.GetOffset(), v.GetLength()
		proto.Reset(v)
		v.Offset, v.Length = offset, length
		mtproto.MakeTLMessageEntityUnknown(v)
		return representable(mtproto.Predicate_messageEntityUnknown, layer)
	}
	return false
}

// flatRichText renders the fragments of quote_rich as the plain text older
// clients show (RichQuote.flatText, Android 13.2.1 RichQuote.java:223-229).
func flatRichText(rich []*mtproto.RichText) string {
	var out []byte
	var walk func(*mtproto.RichText)
	walk = func(t *mtproto.RichText) {
		if t == nil {
			return
		}
		if s := t.GetText_STRING(); s != "" {
			out = append(out, s...)
		}
		if inner := t.GetText_RICHTEXT(); inner != nil {
			walk(inner)
		}
		for _, c := range t.GetTexts() {
			walk(c)
		}
	}
	for _, t := range rich {
		walk(t)
	}
	return string(out)
}
