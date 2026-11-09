package main

import (
	"strings"
	"testing"
)

func TestParseSchema(t *testing.T) {
	items, err := parseSchema(`boolFalse#bc799737 = Bool;
vector#1cb5c415 {t:Type} # [ t ] = Vector t;
---functions---
channels.toggleForum#a2952b54 channel:InputChannel enabled:Bool tabs:Bool sidebar:Bool = Updates;
invokeAfterMsg#cb9f372d {X:Type} msg_id:long query:!X = X;
---types---
messageMediaWidget#2238f214 flags:# is_copy:flags.0?true width:int public_data:flags.3?string ids:flags2.4?Vector<long> = MessageMedia;
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("%d items", len(items))
	}
	if items[0].name != "boolFalse" || items[0].id != 0xbc799737 || items[0].isMethod {
		t.Fatalf("%+v", items[0])
	}
	if !items[1].isMethod || items[1].name != "channels.toggleForum" || len(items[1].fields) != 4 {
		t.Fatalf("%+v", items[1])
	}
	w := items[3]
	if w.isMethod || w.result != "MessageMedia" {
		t.Fatalf("%+v", w)
	}
	f := w.fields
	if f[0].typ != "#" || f[1].flagWord != "flags" || f[1].typ != "true" {
		t.Fatalf("%+v", f[:2])
	}
	if !f[4].vector || f[4].typ != "long" || f[4].flagWord != "flags2" || f[4].flagBit != 4 {
		t.Fatalf("%+v", f[4])
	}
}

func TestProtoTypeOfFlags(t *testing.T) {
	cases := []struct {
		f    field
		rep  bool
		want string
	}{
		{field{typ: "int"}, false, "int32"},
		{field{typ: "int", flagWord: "flags"}, false, "google.protobuf.Int32Value"},
		{field{typ: "string", flagWord: "flags"}, false, "google.protobuf.StringValue"},
		{field{typ: "bytes", flagWord: "flags"}, false, "bytes"},
		{field{typ: "true", flagWord: "flags"}, false, "bool"},
		{field{typ: "long", vector: true, flagWord: "flags"}, true, "int64"},
		{field{typ: "wallet.Card", flagWord: "flags"}, false, "wallet_Card"},
	}
	for _, c := range cases {
		rep, typ := c.f.protoType()
		if rep != c.rep || typ != c.want {
			t.Errorf("%+v: %v %s", c.f, rep, typ)
		}
	}
}

func TestGoCamel(t *testing.T) {
	for in, want := range map[string]string{
		"TL_messages_rateTranscribedAudio": "TLMessagesRateTranscribedAudio",
		"help_PeerColorSet":                "Help_PeerColorSet",
		"reply_to_msg_id_FLAGINT32":        "ReplyToMsgId_FLAGINT32",
		"rows_VECTORKEYBOARDBUTTONROW":     "Rows_VECTORKEYBOARDBUTTONROW",
	} {
		if got := goCamel(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}

func TestResultProto(t *testing.T) {
	for in, want := range map[string]string{
		"Bool":                "Bool",
		"messages.WebPage":    "messages_WebPage",
		"Vector<string>":      "Vector_String",
		"Vector<RecentStory>": "Vector_RecentStory",
	} {
		if got := resultProto(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}

func TestEncodeCaseHandlesFlagWords(t *testing.T) {
	g := &generator{}
	it := item{id: 0xdeadbeef, fields: []field{
		{name: "flags", typ: "#"},
		{name: "a", typ: "true", flagWord: "flags", flagBit: 1},
		{name: "n", typ: "int", flagWord: "flags", flagBit: 2},
	}}
	fm := fieldMap{"a": {name: "a", typ: "bool"}, "n": {name: "n", typ: "google.protobuf.Int32Value"}}
	got := g.encodeCase(&it, fm, true)
	for _, want := range []string{"case 0xdeadbeef:", "x.UInt(0xdeadbeef)", "m.GetA() == true", "x.UInt(getFlags())", "m.GetN().Value", "flags |= 1 << 2"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
}
