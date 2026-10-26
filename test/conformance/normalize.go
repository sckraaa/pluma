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

// Package conformance compares our server with Telegram (task 007, ADR
// 0002, 0018): a scenario runs against the Telegram test environment once,
// its normalized answers become a fixture in testdata, and the same
// scenario against our server must give the same tree.
package conformance

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Tree turns a TL object into a tree: {"_": predicate, field: value} with
// the fields that are set, field names as in the TL schema. Lists are
// lists, wrapped scalars are plain, bytes are base64.
func Tree(m proto.Message) any {
	if m == nil {
		return nil
	}
	return tree(m.ProtoReflect())
}

func tree(m protoreflect.Message) any {
	if !m.IsValid() {
		return nil
	}
	fields := m.Descriptor().Fields()
	// The TL wrappers (TLAuthSentCode and the like) hold the object in data2.
	if fields.Len() == 1 && fields.Get(0).Name() == "data2" {
		return tree(m.Get(fields.Get(0)).Message())
	}
	if strings.HasPrefix(string(m.Descriptor().FullName()), "google.protobuf.") && fields.ByName("value") != nil {
		return scalar(fields.ByName("value"), m.Get(fields.ByName("value")))
	}
	out := map[string]any{}
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		switch name := string(fd.Name()); name {
		case "predicate_name":
			out["_"] = v.String()
		case "constructor":
		default:
			out[tlName(name)] = value(fd, v)
		}
		return true
	})
	return out
}

func value(fd protoreflect.FieldDescriptor, v protoreflect.Value) any {
	if fd.IsList() {
		l := v.List()
		items := make([]any, l.Len())
		for i := range items {
			if fd.Kind() == protoreflect.MessageKind {
				items[i] = tree(l.Get(i).Message())
			} else {
				items[i] = scalar(fd, l.Get(i))
			}
		}
		return items
	}
	if fd.IsMap() {
		return fmt.Sprintf("<map of %d>", v.Map().Len())
	}
	if fd.Kind() == protoreflect.MessageKind {
		return tree(v.Message())
	}
	return scalar(fd, v)
}

func scalar(fd protoreflect.FieldDescriptor, v protoreflect.Value) any {
	switch fd.Kind() {
	case protoreflect.BytesKind:
		return base64.StdEncoding.EncodeToString(v.Bytes())
	case protoreflect.BoolKind:
		return v.Bool()
	case protoreflect.StringKind:
		return v.String()
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return v.Int()
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind, protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return int64(v.Uint())
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return v.Float()
	case protoreflect.EnumKind:
		return int64(v.Enum())
	}
	return v.Interface()
}

// tlFieldSuffix is the type suffix the proto generator adds to a field
// that has several types in the schema: peer_PEER, title_STRING.
var tlFieldSuffix = regexp.MustCompile(`_[A-Z0-9]+$`)

func tlName(name string) string {
	return tlFieldSuffix.ReplaceAllString(name, "")
}

// Labeler replaces the values that differ between servers and runs by
// labels: the same value gets the same label within one scenario, so a
// fixture still says "the message of this update is the one sent".
type Labeler struct {
	ids      map[string]string
	pts      map[string]int64 // first value of each counter
	nextIds  int
	personal map[string]map[string]string // field -> value -> label
}

// NewLabeler returns an empty Labeler.
func NewLabeler() *Labeler {
	return &Labeler{ids: map[string]string{}, pts: map[string]int64{}, personal: map[string]map[string]string{}}
}

// Field classes.
var (
	idFields = set("id", "user_id", "chat_id", "channel_id", "msg_id", "max_id", "access_hash", "random_id",
		"reply_to_msg_id", "top_msg_id", "self_id", "inviter_id", "admin_id", "photo_id", "dc_id_hint",
		"messages", // the ids of updateDeleteMessages; a list of messages is objects and is not labeled
		"document_id", "background_emoji_id")
	timeFields   = set("date", "expires", "edit_date", "was_online", "until_date", "timeout", "expires_at")
	opaqueFields = set("phone_code_hash", "file_reference", "hash", "bytes", "secret", "nonce", "salt",
		"stripped_thumb", "future_auth_token", "token",
		"country") // where the recording ran: not kept
	counterFields = set("pts", "qts", "seq")
	// personalFields say who the account is: a fixture recorded with a real
	// account keeps none of them.
	personalFields = set("first_name", "last_name", "username", "phone")
)

func set(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// Label returns t with the volatile values replaced.
func (l *Labeler) Label(t any) any {
	return l.label("", t)
}

func (l *Labeler) label(key string, t any) any {
	switch v := t.(type) {
	case map[string]any:
		// In key order: the labels must not depend on the map's order.
		ks := make([]string, 0, len(v))
		for k := range v {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		out := make(map[string]any, len(v))
		for _, k := range ks {
			out[k] = l.label(k, v[k])
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = l.label(key, x)
		}
		return out
	}
	switch {
	case idFields[key] && t != int64(0):
		raw := fmt.Sprint(t)
		lab, ok := l.ids[raw]
		if !ok {
			l.nextIds++
			lab = fmt.Sprintf("$id%d", l.nextIds)
			l.ids[raw] = lab
		}
		return lab
	case timeFields[key]:
		return "$time"
	case opaqueFields[key]:
		return "$opaque"
	case personalFields[key] && t != "":
		labels := l.personal[key]
		if labels == nil {
			labels = map[string]string{}
			l.personal[key] = labels
		}
		raw := fmt.Sprint(t)
		lab, ok := labels[raw]
		if !ok {
			lab = fmt.Sprintf("$%s%d", key, len(labels)+1)
			labels[raw] = lab
		}
		return lab
	case counterFields[key]:
		n, ok := t.(int64)
		if !ok {
			return "$" + key
		}
		first, seen := l.pts[key]
		if !seen {
			l.pts[key], first = n, n
		}
		return fmt.Sprintf("$%s%+d", key, n-first)
	}
	return t
}
