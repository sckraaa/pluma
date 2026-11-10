package main

import (
	"os"
	"path/filepath"
	"regexp"
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

// The invoke* wrappers have no generated code but do have table entries: a
// client takes their constructor for its layer from there.
func TestGenericWrapperGetsLayerEntry(t *testing.T) {
	const id = 0xaca9fd2e
	g := &generator{table: map[string]map[int]uint32{"invokeWithTakeout": {231: id}}, byID: map[uint32]string{id: "invokeWithTakeout"}}
	items, err := parseSchema("---functions---\ninvokeWithTakeout#aca9fd2e {X:Type} takeout_id:long query:!X = X;\ninvokeWithNothing#aca9fd2f {X:Type} query:!X = X;\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := g.addLayer(233, items); err != nil {
		t.Fatal(err)
	}
	if g.table["invokeWithTakeout"][233] != id {
		t.Fatalf("table: %v", g.table["invokeWithTakeout"])
	}
	if g.table["invokeWithNothing"] != nil {
		t.Fatalf("an unknown wrapper was added: %v", g.table)
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

// A conditional field whose name upstream already suffixed is the same field
// for a new constructor, not a new one: channelFull of layer 233 must encode
// the available_reactions the handlers fill.
func TestMapFieldsFindsSuffixedUpstreamField(t *testing.T) {
	src := "message ChatFull {\n" +
		"  string predicate_name = 1;\n" +
		"  TLConstructor constructor = 2;\n" +
		"  ChatReactions available_reactions_FLAGCHATREACTIONS = 21;\n" +
		"  repeated string available_reactions_FLAGVECTORSTRING = 73;\n" +
		"  bool blocked_FLAGBOOLEAN = 30;\n" +
		"  Bool blocked_BOOL = 31;\n" +
		"  PeerStories stories_FLAGPEERSTORIES = 40;\n" +
		"}\n"
	pm := &protoMessage{source: &src, fields: []protoField{
		{typ: "string", name: "predicate_name", number: 1},
		{typ: "TLConstructor", name: "constructor", number: 2},
		{typ: "ChatReactions", name: "available_reactions_FLAGCHATREACTIONS", number: 21},
		{repeated: true, typ: "string", name: "available_reactions_FLAGVECTORSTRING", number: 73},
		{typ: "bool", name: "blocked_FLAGBOOLEAN", number: 30},
		{typ: "Bool", name: "blocked_BOOL", number: 31},
		{typ: "PeerStories", name: "stories_FLAGPEERSTORIES", number: 40},
	}}
	it := item{fields: []field{
		{name: "flags", typ: "#"},
		{name: "blocked", typ: "true", flagWord: "flags", flagBit: 22},
		{name: "available_reactions", typ: "ChatReactions", flagWord: "flags", flagBit: 30},
		{name: "stories", typ: "PeerStories", flagWord: "flags2", flagBit: 4},
		{name: "reactions_limit", typ: "int", flagWord: "flags2", flagBit: 13},
	}}
	before := src
	fm, err := (&generator{}).mapFields("ChatFull", pm, &it, false)
	if err != nil {
		t.Fatal(err)
	}
	for tl, want := range map[string]string{
		"blocked":             "blocked_FLAGBOOLEAN",
		"available_reactions": "available_reactions_FLAGCHATREACTIONS",
		"stories":             "stories_FLAGPEERSTORIES",
		"reactions_limit":     "reactions_limit",
	} {
		if got := fm[tl].name; got != want {
			t.Errorf("%s: %s, want %s", tl, got, want)
		}
	}
	if added := strings.TrimPrefix(src, before[:len(before)-2]); added != "  google.protobuf.Int32Value reactions_limit = 74;\n}\n" {
		t.Errorf("added %q", added)
	}
}

func TestSuffixOf(t *testing.T) {
	for _, c := range []struct {
		f    field
		want string
	}{
		{field{typ: "true", flagWord: "flags"}, "_FLAGBOOLEAN"},
		{field{typ: "Bool"}, "_BOOL"},
		{field{typ: "Bool", flagWord: "flags"}, "_FLAGBOOL"},
		{field{typ: "long"}, "_INT64"},
		{field{typ: "int", flagWord: "flags"}, "_FLAGINT32"},
		{field{typ: "bytes", flagWord: "flags"}, "_FLAGBYTES"},
		{field{typ: "PeerColor", flagWord: "flags2"}, "_FLAGPEERCOLOR"},
		{field{typ: "DataJSON"}, "_DATAJSON"},
		{field{typ: "string", vector: true, flagWord: "flags"}, "_FLAGVECTORSTRING"},
		{field{typ: "KeyboardButtonRow", vector: true}, "_VECTORKEYBOARDBUTTONROW"},
	} {
		if got := suffixOf(&c.f); got != c.want {
			t.Errorf("%+v: %s, want %s", c.f, got, c.want)
		}
	}
}

// Two fields of one message with the same base name and the same type are
// either upstream's own (mtprotoc keeps the first name, as with title and
// title_STRING) or a field the generator failed to find and made again, which
// the handlers never fill. A new pair needs a look before it joins the list.
func TestNoNewSameTypeFieldPairs(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "third_party", "teamgram-proto", "mtproto", "schema.tl.sync.proto"))
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{
		"Chat.color google.protobuf.Int32Value":    true, // upstream
		"DialogFilter.title string":                true,
		"Document.size2 int64":                     true,
		"DocumentAttribute.duration double":        true,
		"MessageAction.title string":               true,
		"MessagePeerReaction.reaction string":      true,
		"MessageReplyHeader.reply_to_msg_id int32": true,
		"Poll.question string":                     true,
		"PollAnswer.text string":                   true,
		"ReactionCount.reaction string":            true,
		"User.color google.protobuf.Int32Value":    true,
		"Update.data bytes":                        true, // data:bytes of the widget queries of layer 233 beside data_FLAGBYTES
	}
	message := regexp.MustCompile(`(?s)\nmessage (\w+) \{\n(.*?)\n\}`)
	fieldRe := regexp.MustCompile(`(?m)^\s*(repeated\s+)?([\w.]+)\s+(\w+)\s*=\s*\d+;`)
	suffix := regexp.MustCompile(`_[A-Z0-9]+$`)
	for _, m := range message.FindAllStringSubmatch(string(src), -1) {
		seen := map[string]string{}
		for _, f := range fieldRe.FindAllStringSubmatch(m[2], -1) {
			base := suffix.ReplaceAllString(f[3], "")
			key := base + " " + f[1] + f[2]
			pair := m[1] + "." + key
			if other, ok := seen[key]; ok && !known[pair] {
				t.Errorf("%s: %s and %s have one base name and one type", m[1], other, f[3])
			}
			seen[key] = f[3]
		}
	}
}
