// Command tllayergen adds the constructors and methods of a newer TL layer to
// the vendored codec (third_party/teamgram-proto/mtproto).
//
// teamgram/proto stops at layer 231 and its generator, mtprotoc, was never
// published. The codec is per-layer tables and switch cases, so an update adds
// text and changes none of the existing code: the tool reads the schema of each
// layer in turn and, for every constructor of it,
//
//   - an id already known for that constructor: only the layer table gets an
//     entry;
//   - a new id of a known constructor: a new case in its Encode and Decode, new
//     fields in its protobuf message, and the table entry;
//   - an unknown constructor: its protobuf message, its class and accessors, the
//     dispatch of the base type, the registries.
//
// Output follows mtprotoc and the decoder limits of ADR 0009 (bounded vectors,
// nested Encode errors returned). Running it again changes nothing. Afterwards
// run protoc as in mtproto/build2.sh, then gofmt.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type schemaFlag []string

func (s *schemaFlag) String() string     { return strings.Join(*s, ",") }
func (s *schemaFlag) Set(v string) error { *s = append(*s, v); return nil }

func main() {
	var schemas schemaFlag
	dir := flag.String("dir", "third_party/teamgram-proto/mtproto", "the codec directory")
	flag.Var(&schemas, "schema", "layer=path of a schema; repeat, in ascending order of layer")
	flag.Parse()
	if len(schemas) == 0 {
		log.Fatal("no -schema given")
	}
	g, err := load(*dir)
	if err != nil {
		log.Fatal(err)
	}
	for _, s := range schemas {
		layer, path, ok := strings.Cut(s, "=")
		if !ok {
			log.Fatalf("bad -schema %q", s)
		}
		n, err := strconv.Atoi(layer)
		if err != nil {
			log.Fatalf("bad layer in %q", s)
		}
		text, err := os.ReadFile(path)
		if err != nil {
			log.Fatal(err)
		}
		items, err := parseSchema(string(text))
		if err != nil {
			log.Fatalf("%s: %v", path, err)
		}
		if err := g.addLayer(n, items); err != nil {
			log.Fatalf("layer %d: %v", n, err)
		}
	}
	if err := g.save(*dir); err != nil {
		log.Fatal(err)
	}
	log.Printf("added: %d constructors, %d new ids of known constructors, %d true flags, %d layer table entries", g.stats.added, g.stats.changed, g.stats.flags, g.stats.entries)
}

// ---------------------------------------------------------------------------
// the schema

type field struct {
	name     string
	flagWord string // "" for a required field, else "flags", "flags2"…
	flagBit  int
	vector   bool
	typ      string // int, long, double, string, bytes, true, or a type name
}

type item struct {
	name     string // messages.report
	id       uint32
	fields   []field
	result   string // the type after "="
	isMethod bool
}

var lineRe = regexp.MustCompile(`^([\w.]+)#([0-9a-f]+)\s+(.*?)=\s*(.+);\s*$`)

func parseSchema(text string) ([]item, error) {
	var items []item
	methods := false
	for _, raw := range strings.Split(text, "\n") {
		l := strings.TrimSpace(raw)
		switch l {
		case "---functions---":
			methods = true
			continue
		case "---types---":
			methods = false
			continue
		}
		if l == "" || strings.HasPrefix(l, "//") {
			continue
		}
		m := lineRe.FindStringSubmatch(l)
		if m == nil || strings.HasPrefix(l, "vector#") {
			continue
		}
		id, err := strconv.ParseUint(m[2], 16, 32)
		if err != nil {
			return nil, fmt.Errorf("%q: %v", l, err)
		}
		it := item{name: m[1], id: uint32(id), result: strings.TrimSpace(m[4]), isMethod: methods}
		for _, tok := range strings.Fields(m[3]) {
			if strings.HasPrefix(tok, "{") {
				continue
			}
			f, err := parseField(tok)
			if err != nil {
				return nil, fmt.Errorf("%q: %v", l, err)
			}
			if f != nil {
				it.fields = append(it.fields, *f)
			}
		}
		items = append(items, it)
	}
	return items, nil
}

func parseField(tok string) (*field, error) {
	name, typ, ok := strings.Cut(tok, ":")
	if !ok {
		return nil, fmt.Errorf("field %q", tok)
	}
	f := field{name: name}
	if typ == "#" {
		f.typ = "#"
		return &f, nil
	}
	if cond, rest, ok := strings.Cut(typ, "?"); ok {
		word, bit, ok := strings.Cut(cond, ".")
		if !ok {
			return nil, fmt.Errorf("condition %q", cond)
		}
		n, err := strconv.Atoi(bit)
		if err != nil {
			return nil, err
		}
		f.flagWord, f.flagBit = word, n
		typ = rest
	}
	if strings.HasPrefix(typ, "Vector<") && strings.HasSuffix(typ, ">") {
		f.vector = true
		typ = typ[len("Vector<") : len(typ)-1]
	}
	if typ == "" {
		return nil, fmt.Errorf("unsupported type in %q", tok)
	}
	f.typ = typ
	return &f, nil
}

// protoType gives the protobuf type of a TL field.
func (f *field) protoType() (repeated bool, typ string) {
	repeated = f.vector
	switch f.typ {
	case "true":
		return false, "bool"
	case "int":
		if !f.vector && f.flagWord != "" {
			return false, "google.protobuf.Int32Value"
		}
		return repeated, "int32"
	case "long":
		if !f.vector && f.flagWord != "" {
			return false, "google.protobuf.Int64Value"
		}
		return repeated, "int64"
	case "double":
		if !f.vector && f.flagWord != "" {
			return false, "google.protobuf.DoubleValue"
		}
		return repeated, "double"
	case "string":
		if !f.vector && f.flagWord != "" {
			return false, "google.protobuf.StringValue"
		}
		return repeated, "string"
	case "bytes":
		return repeated, "bytes"
	}
	return repeated, protoTypeName(f.typ)
}

func protoTypeName(tl string) string { return strings.ReplaceAll(tl, ".", "_") }

func isPrimitive(t string) bool {
	switch t {
	case "int", "long", "double", "string", "bytes":
		return true
	}
	return false
}

// goCamel is protoc-gen-go's GoCamelCase.
func goCamel(s string) string {
	var b []byte
	lower := func(c byte) bool { return 'a' <= c && c <= 'z' }
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '_' && (i == 0 || s[i-1] == '.'):
			b = append(b, 'X')
		case c == '_' && i+1 < len(s) && lower(s[i+1]):
		case '0' <= c && c <= '9':
			b = append(b, c)
		default:
			if lower(c) {
				c -= 'a' - 'A'
			}
			b = append(b, c)
			for ; i+1 < len(s) && lower(s[i+1]); i++ {
				b = append(b, s[i+1])
			}
		}
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// the files

type protoField struct {
	repeated bool
	typ      string
	name     string
	number   int
}

type protoMessage struct {
	fields []protoField
	source *string // the file text that holds it
}

type generator struct {
	nameReg  string
	idReg    string
	crcProto string
	crcGo    string
	sync     string
	service  string
	codec    string
	table    map[string]map[int]uint32 // key -> layer -> id
	byID     map[uint32]string         // id -> key
	messages map[string]*protoMessage

	// appended at the end of their files
	newCodec strings.Builder
	newSync  strings.Builder

	newConsts   strings.Builder
	newCRCProto strings.Builder
	newCRCGo    strings.Builder
	newIDReg    strings.Builder
	newRevName  strings.Builder
	newTable    map[string]bool // keys first added by this run
	pending     map[string]*strings.Builder
	notServed   []string // rpc lines of new methods, for RPCNotServed

	stats struct{ added, changed, entries, flags int }
}

var (
	tableKeyRe   = regexp.MustCompile(`(?m)^\tPredicate_(\w+): \{\n((?:(?:\t\t\d+:\s+-?\d+,.*)?\n)*)\t\},`)
	tableEntryRe = regexp.MustCompile(`(?m)^\t\t(\d+):\s+(-?\d+),`)
	revEntryRe   = regexp.MustCompile(`(?m)^\t(-?\d+):\s+Predicate_(\w+),`)
	protoMsgRe   = regexp.MustCompile(`(?ms)^message (\w+) \{\n(.*?)^\}`)
	protoFieldRe = regexp.MustCompile(`(?m)^\s*(repeated\s+)?([\w.]+)\s+(\w+)\s*=\s*(\d+);`)
)

func load(dir string) (*generator, error) {
	g := &generator{
		table:    map[string]map[int]uint32{},
		byID:     map[uint32]string{},
		messages: map[string]*protoMessage{},
		newTable: map[string]bool{},
	}
	files := []struct {
		name string
		dst  *string
	}{
		{"class_name_registers.go", &g.nameReg},
		{"class_id_registers.tl.go", &g.idReg},
		{"schema.tl.crc32.proto", &g.crcProto},
		{"schema.tl.crc32_helper.pb.go", &g.crcGo},
		{"schema.tl.sync.proto", &g.sync},
		{"schema.tl.sync_service.proto", &g.service},
		{"codec_schema.tl.pb.go", &g.codec},
	}
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(dir, f.name))
		if err != nil {
			return nil, err
		}
		*f.dst = string(b)
	}
	for _, m := range tableKeyRe.FindAllStringSubmatch(g.nameReg, -1) {
		d := map[int]uint32{}
		for _, e := range tableEntryRe.FindAllStringSubmatch(m[2], -1) {
			l, _ := strconv.Atoi(e[1])
			v, _ := strconv.ParseInt(e[2], 10, 64)
			d[l] = uint32(v)
		}
		g.table[m[1]] = d
	}
	for _, m := range revEntryRe.FindAllStringSubmatch(g.nameReg, -1) {
		v, _ := strconv.ParseInt(m[1], 10, 64)
		g.byID[uint32(v)] = m[2]
	}
	for _, src := range []*string{&g.sync, &g.service} {
		for _, m := range protoMsgRe.FindAllStringSubmatch(*src, -1) {
			pm := &protoMessage{source: src}
			for _, f := range protoFieldRe.FindAllStringSubmatch(m[2], -1) {
				n, _ := strconv.Atoi(f[4])
				pm.fields = append(pm.fields, protoField{repeated: f[1] != "", typ: f[2], name: f[3], number: n})
			}
			g.messages[m[1]] = pm
		}
	}
	return g, nil
}

// key of an item in the tables. mtprotoc gave two constructors its own names;
// such an item is found through its id.
func (g *generator) keyOf(it *item) string {
	key := strings.ReplaceAll(it.name, ".", "_")
	if _, ok := g.table[key]; ok {
		return key
	}
	if k, ok := g.byID[it.id]; ok {
		return k
	}
	return key
}

func (g *generator) addLayer(layer int, items []item) error {
	for i := range items {
		it := &items[i]
		key := g.keyOf(it)
		if g.knownID(key, it.id) {
			// The id does not depend on "flags.N?true" fields (the constructor
			// number is computed without them), so a known id may have new
			// ones: channel got forum_sidebar at 233 under the id of 232.
			if err := g.addTrueFlags(key, it); err != nil {
				return err
			}
		}
		if g.table[key][layer] == it.id {
			continue // already there
		}
		generic := false
		for _, f := range it.fields {
			if strings.Contains(f.typ, "!") {
				generic = true
			}
		}
		if generic {
			continue // invokeWithLayer and initConnection are hand-written, not in these tables
		}
		switch {
		case g.table[key] == nil:
			if err := g.addConstructor(key, it); err != nil {
				return err
			}
		default:
			if !g.knownID(key, it.id) {
				if err := g.addVersion(key, it); err != nil {
					return err
				}
				g.stats.changed++
			}
		}
		if g.table[key] == nil {
			g.table[key] = map[int]uint32{}
		}
		g.table[key][layer] = it.id
		g.byID[it.id] = key
		g.stats.entries++
	}
	return nil
}

func hex32(id uint32) string { return fmt.Sprintf("0x%x", id) }

func tlName(key string) string   { return "TL" + goCamel("_" + key)[1:] }
func toName(key string) string   { return "To_" + goCamel("_" + key)[1:] }
func makeName(key string) string { return "Make" + tlName(key) }

func baseGo(tl string) string { return goCamel(protoTypeName(tl)) }

// knownID says whether an earlier layer already gave the constructor this id.
func (g *generator) knownID(key string, id uint32) bool {
	for _, v := range g.table[key] {
		if v == id {
			return true
		}
	}
	return false
}

func (g *generator) addConstructor(key string, it *item) error {
	g.stats.added++
	fmt.Fprintf(&g.newConsts, "\tPredicate_%s = %q\n", key, key)
	g.newTable[key] = true
	g.registerID(key, it, fmt.Sprintf("CRC32_%s", key))

	if it.isMethod {
		return g.addMethod(key, it)
	}
	return g.addClass(key, it)
}

// registerID adds the id to the crc enums, the reverse table and the id registry.
func (g *generator) registerID(key string, it *item, constName string) {
	fmt.Fprintf(&g.newCRCProto, "  %s = %d; // %08x\n", constName, int32(it.id), it.id)
	fmt.Fprintf(&g.newCRCGo, "\t%s TLConstructor = %d // %s\n", constName, int32(it.id), hex32(it.id))
	fmt.Fprintf(&g.newRevName, "\t%d: Predicate_%s, // %08x\n", int32(it.id), key, it.id)
	if it.isMethod {
		fmt.Fprintf(&g.newIDReg, "\t%d: func() TLObject { // %s\n\t\treturn &%s{\n\t\t\tConstructor: %d,\n\t\t}\n\t},\n",
			int32(it.id), hex32(it.id), tlName(key), int32(it.id))
	} else {
		fmt.Fprintf(&g.newIDReg, "\t%d: func() TLObject { // %s\n\t\to := %s(nil)\n\t\to.Data2.Constructor = %d\n\t\treturn o\n\t},\n",
			int32(it.id), hex32(it.id), makeName(key), int32(it.id))
	}
}

// addVersion adds a new id to a known constructor.
func (g *generator) addVersion(key string, it *item) error {
	class := tlName(key)
	constName := fmt.Sprintf("CRC32_%s_%08x", key, it.id)
	g.registerID(key, it, constName)

	if it.isMethod {
		msg := "TL_" + key
		pm := g.messages[msg]
		if pm == nil {
			return fmt.Errorf("no protobuf message %s", msg)
		}
		fm, err := g.mapFields(msg, pm, it, false)
		if err != nil {
			return err
		}
		enc := g.encodeCase(it, fm, false)
		if !insertAfter(&g.codec, "func (m *"+class+") Encode(", "\tswitch uint32(m.Constructor) {\n", enc) {
			return fmt.Errorf("no Encode switch of %s", class)
		}
		dec := g.decodeCase(it, fm, false)
		if !insertAfter(&g.codec, "func (m *"+class+") Decode(", "\tswitch uint32(m.Constructor) {\n", dec) {
			return fmt.Errorf("no Decode switch of %s", class)
		}
		return nil
	}

	base := protoTypeName(it.result)
	pm := g.messages[base]
	if pm == nil {
		return fmt.Errorf("no protobuf message %s", base)
	}
	fm, err := g.mapFields(base, pm, it, true)
	if err != nil {
		return err
	}
	if !insertAfter(&g.codec, "func (m *"+class+") Encode(", "\tswitch uint32(clazzId) {\n", g.encodeCase(it, fm, true)) {
		return fmt.Errorf("no Encode switch of %s", class)
	}
	if !insertAfter(&g.codec, "func (m *"+class+") Decode(", "\tswitch uint32(m.Data2.Constructor) {\n", g.decodeCase(it, fm, true)) {
		return fmt.Errorf("no Decode switch of %s", class)
	}
	baseG := goCamel(base)
	dispatch := fmt.Sprintf("\tcase %s:\n\t\tm2 := %s(m)\n\t\tm2.Decode(dBuf)\n", hex32(it.id), makeName(key))
	if !insertBeforeDefault(&g.codec, "func (m *"+baseG+") Decode(", dispatch) {
		return fmt.Errorf("no Decode of %s", baseG)
	}
	g.addAccessors(key, class, fm)
	return nil
}

// caseOf returns the bounds of "case id:" in the function of class, in src, or
// ok=false when src does not hold it. A class created by this run is in newCodec,
// not in codec.
func caseOf(src, class, fn string, id uint32) (start, end int, ok bool) {
	sig := "func (m *" + class + ") " + fn + "("
	i := strings.Index(src, sig)
	if i < 0 {
		return 0, 0, false
	}
	fnEnd := strings.Index(src[i:], "\n}\n")
	if fnEnd < 0 {
		return 0, 0, false
	}
	body := src[i : i+fnEnd]
	c := strings.Index(body, "\tcase "+hex32(id)+":\n")
	if c < 0 {
		return 0, 0, false
	}
	rest := body[c+1:]
	next := len(rest)
	for _, marker := range []string{"\n\tcase 0x", "\n\tdefault:\n"} {
		if j := strings.Index(rest, marker); j >= 0 && j < next {
			next = j
		}
	}
	return i + c, i + c + 1 + next, true
}

// addTrueFlags adds the "flags.N?true" fields a known id does not have yet in
// its Encode and Decode cases.
func (g *generator) addTrueFlags(key string, it *item) error {
	class := tlName(key)
	union := !it.isMethod
	msg := "TL_" + key
	if union {
		msg = protoTypeName(it.result)
	}
	pm := g.messages[msg]
	if pm == nil {
		return nil // the generic wrappers
	}
	for i := range it.fields {
		f := it.fields[i]
		if f.typ != "true" {
			continue
		}
		sub := item{name: it.name, id: it.id, fields: []field{f}, result: it.result, isMethod: it.isMethod}
		fm, err := g.mapFields(msg, pm, &sub, union)
		if err != nil {
			return err
		}
		goName := goCamel(fm[f.name].name)
		get := "m.Get" + goName + "()"

		// Encode
		src := &g.codec
		start, end, ok := caseOf(*src, class, "Encode", it.id)
		if !ok {
			continue // a class of this run, generated from this very line
		}
		region := (*src)[start:end]
		if strings.Contains(region, get+" == true") {
			continue
		}
		closure := "var get" + upperFirst(f.flagWord) + " = func() uint32 {"
		k := strings.Index(region, closure)
		if k < 0 {
			return fmt.Errorf("%s: no flag function for %s", class, f.flagWord)
		}
		ret := strings.Index(region[k:], "\t\t\treturn flags\n")
		if ret < 0 {
			return fmt.Errorf("%s: flag function of %s has no end", class, f.flagWord)
		}
		add := fmt.Sprintf("\t\t\tif %s == true {\n\t\t\t\tflags |= 1 << %d\n\t\t\t}\n", get, f.flagBit)
		pos := start + k + ret
		*src = (*src)[:pos] + add + (*src)[pos:]

		// Decode
		ds, de, ok := caseOf(*src, class, "Decode", it.id)
		if !ok {
			return fmt.Errorf("%s: no Decode case %s", class, hex32(it.id))
		}
		dregion := (*src)[ds:de]
		marker := fmt.Sprintf("var %s = dBuf.UInt()\n\t\t_ = %s\n", f.flagWord, f.flagWord)
		m := strings.Index(dregion, marker)
		if m < 0 {
			return fmt.Errorf("%s: no %s in Decode", class, f.flagWord)
		}
		set := fmt.Sprintf("m.%s = true", goName)
		if union {
			set = fmt.Sprintf("m.Set%s(true)", goName)
		}
		dadd := fmt.Sprintf("\t\tif (%s & (1 << %d)) != 0 {\n\t\t\t%s\n\t\t}\n", f.flagWord, f.flagBit, set)
		dpos := ds + m + len(marker)
		*src = (*src)[:dpos] + dadd + (*src)[dpos:]
		g.stats.flags++
		if union {
			g.addAccessors(key, class, fm)
		}
	}
	return nil
}

// fieldMap is, for every TL field but the flag words, the Go name of its field.
type fieldMap map[string]protoField

// mapFields finds the protobuf field of each TL field, adding what is missing.
func (g *generator) mapFields(msgName string, pm *protoMessage, it *item, union bool) (fieldMap, error) {
	fm := fieldMap{}
	for i := range it.fields {
		f := &it.fields[i]
		if f.typ == "#" {
			continue
		}
		repeated, typ := f.protoType()
		var found *protoField
		for j := range pm.fields {
			pf := &pm.fields[j]
			if pf.repeated != repeated || pf.typ != typ {
				continue
			}
			if pf.name == f.name || strings.HasPrefix(pf.name, f.name+"_") && pf.name == f.name+suffixOf(f) {
				found = pf
				break
			}
		}
		if found == nil {
			name := f.name
			for j := range pm.fields {
				if pm.fields[j].name == name {
					name = f.name + suffixOf(f)
				}
			}
			next := 3
			for j := range pm.fields {
				if pm.fields[j].number >= next {
					next = pm.fields[j].number + 1
				}
			}
			pf := protoField{repeated: repeated, typ: typ, name: name, number: next}
			pm.fields = append(pm.fields, pf)
			rep := ""
			if repeated {
				rep = "repeated "
			}
			line := fmt.Sprintf("  %s%s %s = %d;\n", rep, typ, name, next)
			if !appendToMessage(pm.source, msgName, line) {
				return nil, fmt.Errorf("message %s not found in its file", msgName)
			}
			found = &pm.fields[len(pm.fields)-1]
		}
		fm[f.name] = *found
	}
	return fm, nil
}

func suffixOf(f *field) string {
	var s string
	switch {
	case f.vector:
		s = "VECTOR" + strings.ToUpper(strings.ReplaceAll(primitiveProto(f.typ), ".", ""))
	case isPrimitive(f.typ):
		s = strings.ToUpper(primitiveProto(f.typ))
		if f.flagWord != "" {
			s = "FLAG" + s
		}
	default:
		s = strings.ToUpper(strings.ReplaceAll(protoTypeName(f.typ), "_", ""))
	}
	return "_" + s
}

// resultProto is the protobuf name of a method's result type.
func resultProto(tl string) string {
	if strings.HasPrefix(tl, "Vector<") {
		inner := tl[len("Vector<") : len(tl)-1]
		if isPrimitive(inner) {
			inner = upperFirst(inner) // Vector_Int, Vector_Long, Vector_String
		}
		return "Vector_" + protoTypeName(inner)
	}
	return protoTypeName(tl)
}

func primitiveProto(t string) string {
	switch t {
	case "int":
		return "int32"
	case "long":
		return "int64"
	}
	return protoTypeName(t)
}

func appendToMessage(src *string, name, line string) bool {
	start := strings.Index(*src, "message "+name+" {\n")
	if start < 0 {
		return false
	}
	end := strings.Index((*src)[start:], "\n}")
	if end < 0 {
		return false
	}
	pos := start + end + 1
	*src = (*src)[:pos] + line + (*src)[pos:]
	return true
}

func insertAfter(src *string, funcSig, marker, text string) bool {
	i := strings.Index(*src, funcSig)
	if i < 0 {
		return false
	}
	j := strings.Index((*src)[i:], marker)
	if j < 0 {
		return false
	}
	pos := i + j + len(marker)
	*src = (*src)[:pos] + text + (*src)[pos:]
	return true
}

func insertBeforeDefault(src *string, funcSig, text string) bool {
	i := strings.Index(*src, funcSig)
	if i < 0 {
		return false
	}
	j := strings.Index((*src)[i:], "\tdefault:\n")
	if j < 0 {
		return false
	}
	pos := i + j
	*src = (*src)[:pos] + text + (*src)[pos:]
	return true
}

// addClass emits the class of a new constructor and, for a new type, its base.
func (g *generator) addClass(key string, it *item) error {
	base := protoTypeName(it.result)
	pm := g.messages[base]
	if pm == nil {
		pm = &protoMessage{source: &g.sync}
		g.messages[base] = pm
		g.sync += fmt.Sprintf("\nmessage %s {\n  string predicate_name = 1;\n  TLConstructor constructor = 2;\n}\n", base)
		pm.fields = append(pm.fields,
			protoField{typ: "string", name: "predicate_name", number: 1},
			protoField{typ: "TLConstructor", name: "constructor", number: 2})
		g.addBase(base)
	}
	fm, err := g.mapFields(base, pm, it, true)
	if err != nil {
		return err
	}
	fmt.Fprintf(&g.newSync, "\n// %s\nmessage TL_%s {\n  %s data2 = 1;\n}\n", it.name, key, base)

	class := tlName(key)
	baseG := goCamel(base)
	w := &g.newCodec
	fmt.Fprintf(w, `
///////////////////////////////////////////////////////////////////////////////
// %[1]s

// %[2]s
func (m *%[6]s) %[2]s() *%[1]s {
	m.PredicateName = Predicate_%[5]s
	return &%[1]s{
		Data2: m,
	}
}

// %[3]s
func %[3]s(data2 *%[6]s) *%[1]s {
	if data2 == nil {
		return &%[1]s{Data2: &%[6]s{
			PredicateName: Predicate_%[5]s,
		}}
	} else {
		data2.PredicateName = Predicate_%[5]s
		return &%[1]s{Data2: data2}
	}
}

func (m *%[1]s) To_%[6]s() *%[6]s {
	m.Data2.PredicateName = Predicate_%[5]s
	return m.Data2
}

`, class, toName(key), makeName(key), key, key, baseG)
	g.addAccessors(key, class, fm)
	g.newCodec.WriteString(pendingAccessors(g, class))
	fmt.Fprintf(w, `func (m *%[1]s) GetPredicateName() string {
	return Predicate_%[2]s
}

func (m *%[1]s) Encode(x *EncodeBuf, layer int32) error {
	clazzId := GetClazzID(Predicate_%[2]s, int(layer))
	switch uint32(clazzId) {
%[3]s
	default:
		return fmt.Errorf("not found clazzId by (%%s, %%d)", Predicate_%[2]s, layer)
	}
}

func (m *%[1]s) CalcByteSize(layer int32) int {
	return 0
}

func (m *%[1]s) Decode(dBuf *DecodeBuf) error {
	switch uint32(m.Data2.Constructor) {
%[4]s
	default:
		return fmt.Errorf("invalid constructorId: 0x%%x", uint32(m.Data2.Constructor))
	}
}
`, class, key, strings.TrimSuffix(g.encodeCase(it, fm, true), "\n"), strings.TrimSuffix(g.decodeCase(it, fm, true), "\n"))

	encodeCase := fmt.Sprintf("\tcase Predicate_%s:\n\t\tt := m.%s()\n\t\terr := t.Encode(x, layer)\n\t\tif err != nil {\n\t\t\treturn err\n\t\t}\n", key, toName(key))
	if !insertBeforeDefault(&g.codec, "func (m *"+baseG+") Encode(", encodeCase) {
		// the base is new: its Encode is in newCodec
		if !insertBeforeDefaultBuilder(&g.newCodec, "func (m *"+baseG+") Encode(", encodeCase) {
			return fmt.Errorf("no Encode of %s", baseG)
		}
	}
	dispatch := fmt.Sprintf("\tcase %s:\n\t\tm2 := %s(m)\n\t\tm2.Decode(dBuf)\n", hex32(it.id), makeName(key))
	if !insertBeforeDefault(&g.codec, "func (m *"+baseG+") Decode(", dispatch) {
		if !insertBeforeDefaultBuilder(&g.newCodec, "func (m *"+baseG+") Decode(", dispatch) {
			return fmt.Errorf("no Decode of %s", baseG)
		}
	}
	return nil
}

func insertBeforeDefaultBuilder(b *strings.Builder, funcSig, text string) bool {
	s := b.String()
	if !insertBeforeDefault(&s, funcSig, text) {
		return false
	}
	b.Reset()
	b.WriteString(s)
	return true
}

func (g *generator) addBase(base string) {
	baseG := goCamel(base)
	fmt.Fprintf(&g.newCodec, `
///////////////////////////////////////////////////////////////////////////////
// %[1]s

func (m *%[1]s) Encode(x *EncodeBuf, layer int32) error {
	if m == nil { // pluma: a required field left unset is an error, not a panic
		return fmt.Errorf("%[1]s: nil object")
	}
	predicateName := m.PredicateName
	if predicateName == "" {
		if n, ok := clazzIdNameRegisters2[int32(m.Constructor)]; ok {
			predicateName = n
		}
	}

	switch predicateName {

	default:
		return fmt.Errorf("invalid predicate error: %%s", m.PredicateName)
	}

	return nil
}

func (m *%[1]s) CalcByteSize(layer int32) int {
	return 0
}

func (m *%[1]s) Decode(dBuf *DecodeBuf) error {
	m.Constructor = TLConstructor(dBuf.Int())
	switch uint32(m.Constructor) {

	default:
		return fmt.Errorf("invalid constructorId: 0x%%x", uint32(m.Constructor))
	}
	return dBuf.GetError()
}
`, baseG)
}

// addAccessors queues the Set/Get pair of every field the class does not have yet.
func (g *generator) addAccessors(key, class string, fm fieldMap) {
	if g.pending == nil {
		g.pending = map[string]*strings.Builder{}
	}
	names := make([]string, 0, len(fm))
	for n := range fm {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		pf := fm[n]
		goName := goCamel(pf.name)
		if strings.Contains(g.codec, "func (m *"+class+") Set"+goName+"(") {
			continue
		}
		rep, t := "", goType(pf.typ)
		if pf.repeated {
			rep = "[]"
		}
		b := g.pending[class]
		if b == nil {
			b = &strings.Builder{}
			g.pending[class] = b
		}
		fmt.Fprintf(b, "func (m *%[1]s) Set%[2]s(v %[3]s%[4]s) { m.Data2.%[2]s = v }\nfunc (m *%[1]s) Get%[2]s() %[3]s%[4]s { return m.Data2.%[2]s }\n\n", class, goName, rep, t)
	}
}

func pendingAccessors(g *generator, class string) string {
	b := g.pending[class]
	if b == nil {
		return ""
	}
	delete(g.pending, class)
	return b.String()
}

func goType(p string) string {
	switch p {
	case "int32", "int64", "double", "string", "bool":
		if p == "double" {
			return "float64"
		}
		return p
	case "bytes":
		return "[]byte"
	case "google.protobuf.Int32Value":
		return "*wrapperspb.Int32Value"
	case "google.protobuf.Int64Value":
		return "*wrapperspb.Int64Value"
	case "google.protobuf.DoubleValue":
		return "*wrapperspb.DoubleValue"
	case "google.protobuf.StringValue":
		return "*wrapperspb.StringValue"
	}
	return "*" + goCamel(p)
}

// addMethod emits the request class of a new method.
func (g *generator) addMethod(key string, it *item) error {
	msg := "TL_" + key
	pm := &protoMessage{source: &g.service}
	g.messages[msg] = pm
	g.service += fmt.Sprintf("\nmessage %s {\n  TLConstructor constructor = 1;\n}\n", msg)
	pm.fields = append(pm.fields, protoField{typ: "TLConstructor", name: "constructor", number: 1}, protoField{name: "-", number: 2})
	fm, err := g.mapFields(msg, pm, it, false)
	if err != nil {
		return err
	}
	g.notServed = append(g.notServed, fmt.Sprintf("  rpc %s(TL_%s) returns (%s) {}\n", key, key, resultProto(it.result)))
	class := tlName(key)
	fmt.Fprintf(&g.newCodec, `
// %[1]s
// /////////////////////////////////////////////////////////////////////////////
func (m *%[1]s) Encode(x *EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
%[2]s
	default:
		return fmt.Errorf("invalid constructorId: 0x%%x", uint32(m.Constructor))
	}

	return nil
}

func (m *%[1]s) CalcByteSize(layer int32) int {
	return 0
}

func (m *%[1]s) Decode(dBuf *DecodeBuf) error {
	switch uint32(m.Constructor) {
%[3]s
	default:
		return fmt.Errorf("invalid constructorId: 0x%%x", uint32(m.Constructor))
	}
}
`, class, strings.TrimSuffix(g.encodeCase(it, fm, false), "\n"), strings.TrimSuffix(g.decodeCase(it, fm, false), "\n"))
	return nil
}

// ---------------------------------------------------------------------------
// the code of one constructor

// nestedErr is the handling of a nested Encode; ADR 0009 patches of the vendored
// codec use the same form.
func encodeNested(expr, indent string) string {
	return fmt.Sprintf("%[1]sif err := %[2]s.Encode(x, layer); err != nil {\n%[1]s\treturn err\n%[1]s}\n", indent, expr)
}

func (g *generator) encodeCase(it *item, fm fieldMap, union bool) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "\tcase %s:\n\t\tx.UInt(%s)\n\n", hex32(it.id), hex32(it.id))
	words := flagWords(it)
	for _, w := range words {
		b.WriteString(fmt.Sprintf("\t\tvar get%s = func() uint32 {\n\t\t\tvar flags uint32 = 0\n\n", upperFirst(w)))
		for i := range it.fields {
			f := &it.fields[i]
			if f.flagWord != w {
				continue
			}
			get := "m.Get" + goCamel(fm[f.name].name) + "()"
			cond := get + " != nil"
			if f.typ == "true" {
				cond = get + " == true"
			}
			fmt.Fprintf(&b, "\t\t\tif %s {\n\t\t\t\tflags |= 1 << %d\n\t\t\t}\n", cond, f.flagBit)
		}
		b.WriteString("\n\t\t\treturn flags\n\t\t}\n\n")
	}
	for i := range it.fields {
		f := &it.fields[i]
		if f.typ == "#" {
			fmt.Fprintf(&b, "\t\tx.UInt(get%s())\n", upperFirst(f.name))
			continue
		}
		if f.typ == "true" {
			continue
		}
		g.encodeField(&b, f, fm[f.name], i)
	}
	b.WriteString("\n\t\treturn nil\n")
	return b.String()
}

func upperFirst(s string) string { return strings.ToUpper(s[:1]) + s[1:] }

func flagWords(it *item) []string {
	var w []string
	for _, f := range it.fields {
		if f.typ == "#" {
			w = append(w, f.name)
		}
	}
	return w
}

func (g *generator) encodeField(b *bytes.Buffer, f *field, pf protoField, idx int) {
	get := "m.Get" + goCamel(pf.name) + "()"
	indent := "\t\t"
	val := get
	if f.flagWord != "" {
		fmt.Fprintf(b, "\t\tif %s != nil {\n", get)
		indent = "\t\t\t"
		if !f.vector && isPrimitive(f.typ) && f.typ != "bytes" {
			val = get + ".Value"
		}
	}
	switch {
	case f.vector && isPrimitive(f.typ):
		m := map[string]string{"int": "VectorInt", "long": "VectorLong", "string": "VectorString", "bytes": "VectorBytes"}[f.typ]
		if m == "" {
			log.Fatalf("%s: vector of %s", f.name, f.typ)
		}
		fmt.Fprintf(b, "%sx.%s(%s)\n", indent, m, get)
	case f.vector:
		fmt.Fprintf(b, "%[1]sx.Int(int32(CRC32_vector))\n%[1]s{\n%[1]s\tvar (\n%[1]s\t\tsz     = int32(0)\n%[1]s\t\toffset = x.GetOffset()\n%[1]s\t)\n%[1]s\tlist%[2]d := %[3]s\n%[1]s\tx.Int(int32(len(list%[2]d)))\n%[1]s\tfor _, v := range list%[2]d {\n", indent, idx, get)
		b.WriteString(encodeNested("v", indent+"\t\t"))
		fmt.Fprintf(b, "%[1]s\t\tsz += 1\n%[1]s\t}\n%[1]s\tx.IntOffset(offset, sz)\n%[1]s}\n", indent)
	case f.typ == "int":
		fmt.Fprintf(b, "%sx.Int(%s)\n", indent, val)
	case f.typ == "long":
		fmt.Fprintf(b, "%sx.Long(%s)\n", indent, val)
	case f.typ == "double":
		fmt.Fprintf(b, "%sx.Double(%s)\n", indent, val)
	case f.typ == "string":
		fmt.Fprintf(b, "%sx.String(%s)\n", indent, val)
	case f.typ == "bytes":
		fmt.Fprintf(b, "%sx.StringBytes(%s)\n", indent, val)
	default:
		b.WriteString(encodeNested(get, indent))
	}
	if f.flagWord != "" {
		b.WriteString("\t\t}\n\n")
	}
}

func (g *generator) decodeCase(it *item, fm fieldMap, union bool) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "\tcase %s:\n", hex32(it.id))
	set := func(pf protoField, expr string) string {
		if union {
			return fmt.Sprintf("m.Set%s(%s)", goCamel(pf.name), expr)
		}
		return fmt.Sprintf("m.%s = %s", goCamel(pf.name), expr)
	}
	for i := range it.fields {
		f := &it.fields[i]
		if f.typ == "#" {
			fmt.Fprintf(&b, "\t\tvar %[1]s = dBuf.UInt()\n\t\t_ = %[1]s\n", f.name)
			continue
		}
		pf := fm[f.name]
		indent := "\t\t"
		if f.flagWord != "" {
			fmt.Fprintf(&b, "\t\tif (%s & (1 << %d)) != 0 {\n", f.flagWord, f.flagBit)
			indent = "\t\t\t"
		}
		b.WriteString(g.decodeField(f, pf, indent, i, set))
		if f.flagWord != "" {
			b.WriteString("\t\t}\n")
		}
	}
	b.WriteString("\n\t\treturn dBuf.GetError()\n")
	return b.String()
}

func (g *generator) decodeField(f *field, pf protoField, indent string, idx int, set func(protoField, string) string) string {
	wrap := func(e string) string {
		if f.flagWord == "" || f.vector {
			return e
		}
		switch f.typ {
		case "int":
			return fmt.Sprintf("&wrapperspb.Int32Value{Value: %s}", e)
		case "long":
			return fmt.Sprintf("&wrapperspb.Int64Value{Value: %s}", e)
		case "double":
			return fmt.Sprintf("&wrapperspb.DoubleValue{Value: %s}", e)
		case "string":
			return fmt.Sprintf("&wrapperspb.StringValue{Value: %s}", e)
		}
		return e
	}
	switch {
	case f.typ == "true":
		return indent + set(pf, "true") + "\n"
	case f.vector && isPrimitive(f.typ):
		m := map[string]string{"int": "VectorInt", "long": "VectorLong", "string": "VectorString", "bytes": "VectorBytes"}[f.typ]
		return indent + set(pf, "dBuf."+m+"()") + "\n"
	case f.vector:
		et := goCamel(protoTypeName(f.typ))
		return fmt.Sprintf(`%[1]sc%[2]d := dBuf.Int()
%[1]sif c%[2]d != int32(CRC32_vector) {
%[1]s	// dBuf.err = fmt.Errorf("invalid CRC32_vector, c%%d: %%d", %[2]d, c%[2]d)
%[1]s	return dBuf.GetError()
%[1]s}
%[1]sl%[2]d := dBuf.Int()
%[1]sif dBuf.vectorTooLong(l%[2]d, unsafe.Sizeof(%[3]s{})) || l%[2]d > maxVectorLen { // pluma: bound the length by the remaining input and budget
%[1]s	return fmt.Errorf("invalid vector length: %%d", l%[2]d)
%[1]s}
%[1]sv%[2]d := make([]*%[3]s, l%[2]d)
%[1]sfor i := int32(0); i < l%[2]d && dBuf.err == nil; i++ { // pluma: stop at the first decode error
%[1]s	v%[2]d[i] = &%[3]s{}
%[1]s	v%[2]d[i].Decode(dBuf)
%[1]s}
%[1]s%[4]s
`, indent, idx, et, set(pf, fmt.Sprintf("v%d", idx)))
	case f.typ == "int":
		return indent + set(pf, wrap("dBuf.Int()")) + "\n"
	case f.typ == "long":
		return indent + set(pf, wrap("dBuf.Long()")) + "\n"
	case f.typ == "double":
		return indent + set(pf, wrap("dBuf.Double()")) + "\n"
	case f.typ == "string":
		return indent + set(pf, wrap("dBuf.String()")) + "\n"
	case f.typ == "bytes":
		return indent + set(pf, "dBuf.StringBytes()") + "\n"
	}
	t := goCamel(protoTypeName(f.typ))
	return fmt.Sprintf("%[1]sm%[2]d := &%[3]s{}\n%[1]sm%[2]d.Decode(dBuf)\n%[1]s%[4]s\n", indent, idx, t, set(pf, fmt.Sprintf("m%d", idx)))
}

// ---------------------------------------------------------------------------
// writing

func (g *generator) save(dir string) error {
	var accessors strings.Builder
	classes := make([]string, 0, len(g.pending))
	for c := range g.pending {
		classes = append(classes, c)
	}
	sort.Strings(classes)
	for _, c := range classes {
		accessors.WriteString(g.pending[c].String())
	}

	// the constructors tables
	src := g.nameReg
	var consts strings.Builder
	consts.WriteString(g.newConsts.String())
	if !replaceOnce(&src, "\n)\n\nvar clazzNameRegisters2", "\n"+consts.String()+")\n\nvar clazzNameRegisters2") {
		return fmt.Errorf("class_name_registers: const block not found")
	}
	keys := make([]string, 0, len(g.newTable))
	for k := range g.newTable {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var newEntries strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&newEntries, "\tPredicate_%s: {\n", k)
		for _, l := range sortedDesc(g.table[k]) {
			fmt.Fprintf(&newEntries, "\t\t%d: %d, // %08x\n", l, int32(g.table[k][l]), g.table[k][l])
		}
		newEntries.WriteString("\t},\n")
	}
	if !replaceOnce(&src, "\n}\n\nvar clazzIdNameRegisters2", "\n"+newEntries.String()+"}\n\nvar clazzIdNameRegisters2") {
		return fmt.Errorf("class_name_registers: table end not found")
	}
	// the layers that were added to known constructors
	src = addLayerEntries(src, g.table, g.newTable)
	if !insertBeforeMapEnd(&src, "var clazzIdNameRegisters2 = map[int32]string{", g.newRevName.String()) {
		return fmt.Errorf("class_name_registers: reverse table end not found")
	}
	g.nameReg = src

	idReg := g.idReg
	if !insertBeforeMapEnd(&idReg, "var clazzIdRegisters2 = map[int32]func() TLObject{", g.newIDReg.String()) {
		return fmt.Errorf("class_id_registers: table end not found")
	}
	g.idReg = idReg

	if !replaceLast(&g.crcProto, "\n}\n", "\n"+g.newCRCProto.String()+"}\n") {
		return fmt.Errorf("crc32.proto: enum end not found")
	}
	if !replaceLast(&g.crcGo, "\n)\n", "\n"+g.newCRCGo.String()+")\n") {
		return fmt.Errorf("crc32_helper: const end not found")
	}

	if len(g.notServed) != 0 {
		// Without a route the session proxy answers ErrEnterpriseIsBlocked;
		// listing them here puts them into the API coverage matrix.
		const header = "service RPCNotServed {\n"
		if !strings.Contains(g.service, header) {
			g.service += "\n// Methods of the layers after 231 that no service serves yet: the session\n// proxy has no route for them and answers ErrEnterpriseIsBlocked.\n" + header + "}\n"
		}
		if !insertBeforeMapEnd(&g.service, header, strings.Join(g.notServed, "")) {
			return fmt.Errorf("sync_service: RPCNotServed not found")
		}
	}
	g.sync += g.newSync.String()
	g.codec += accessors.String() + g.newCodec.String()

	for _, f := range []struct {
		name string
		text string
	}{
		{"class_name_registers.go", g.nameReg},
		{"class_id_registers.tl.go", g.idReg},
		{"schema.tl.crc32.proto", g.crcProto},
		{"schema.tl.crc32_helper.pb.go", g.crcGo},
		{"schema.tl.sync.proto", g.sync},
		{"schema.tl.sync_service.proto", g.service},
		{"codec_schema.tl.pb.go", g.codec},
	} {
		if err := os.WriteFile(filepath.Join(dir, f.name), []byte(f.text), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func sortedDesc(m map[int]uint32) []int {
	l := make([]int, 0, len(m))
	for k := range m {
		l = append(l, k)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(l)))
	return l
}

func replaceOnce(src *string, old, repl string) bool {
	i := strings.Index(*src, old)
	if i < 0 {
		return false
	}
	*src = (*src)[:i] + repl + (*src)[i+len(old):]
	return true
}

func replaceLast(src *string, old, repl string) bool {
	i := strings.LastIndex(*src, old)
	if i < 0 {
		return false
	}
	*src = (*src)[:i] + repl + (*src)[i+len(old):]
	return true
}

func insertBeforeMapEnd(src *string, header, text string) bool {
	i := strings.Index(*src, header)
	if i < 0 {
		return false
	}
	j := strings.Index((*src)[i:], "\n}\n")
	if j < 0 {
		return false
	}
	pos := i + j + 1
	*src = (*src)[:pos] + text + (*src)[pos:]
	return true
}

// addLayerEntries writes the table entries of known constructors for layers
// they were not listed at, newest first as the file has them.
func addLayerEntries(src string, table map[string]map[int]uint32, skip map[string]bool) string {
	out := tableKeyRe.ReplaceAllStringFunc(src, func(block string) string {
		m := tableKeyRe.FindStringSubmatch(block)
		key := m[1]
		if skip[key] {
			return block
		}
		have := map[int]bool{}
		for _, e := range tableEntryRe.FindAllStringSubmatch(m[2], -1) {
			l, _ := strconv.Atoi(e[1])
			have[l] = true
		}
		var add strings.Builder
		for _, l := range sortedDesc(table[key]) {
			if !have[l] {
				fmt.Fprintf(&add, "\t\t%d: %d, // %08x\n", l, int32(table[key][l]), table[key][l])
			}
		}
		if add.Len() == 0 {
			return block
		}
		i := strings.Index(block, "\n")
		return block[:i+1] + add.String() + block[i+1:]
	})
	return out
}
