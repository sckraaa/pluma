package boxaudit

import (
	"reflect"
	"sort"
	"strconv"
	"testing"
)

// TestRepository keeps box transactions free of what this package finds.
// To fix a finding, move the call before the transaction or after its
// commit (Box.Emit, Box.InvalidateCache), use the Tx variant of the DAO
// method, or handle the error; if it is not a violation, mark the line with
// a "// boxaudit:ok <reason>" comment.
func TestRepository(t *testing.T) {
	findings, err := Check("../..", "./app/...", "./pkg/...")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// TestWritersInRepository keeps the box state and the journal written by
// pkg/userbox alone. To fix a finding, write through userbox.Run (Box
// methods) instead.
func TestWritersInRepository(t *testing.T) {
	findings, err := Writers("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// TestWriters runs the writer rule over a fixture (testdata/writers): SQL
// in Go strings and in a table definition is found, reads, other tables,
// comments, tests, pkg/userbox and the events in pkg/outbox are not.
func TestWriters(t *testing.T) {
	findings, err := Writers("testdata/writers")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range findings {
		got = append(got, f.Rule+"@"+f.Pos.Filename+":"+strconv.Itoa(f.Pos.Line))
	}
	want := []string{"writer@pkg/outbox/outbox.go:5", "writer@tables.xml:4", "writer@writers.go:6", "writer@writers.go:7", "writer@writers.go:13", "writer@writers.go:17"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findings\n%v\nwant\n%v\n%v", got, want, findings)
	}
}

// TestRules runs the checker over a fixture with a violation of each kind
// (testdata/fixture: its comments name the rule each line breaks).
func TestRules(t *testing.T) {
	findings, err := Check(".", "./testdata/fixture")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range findings {
		got = append(got, f.Rule+"@"+f.Pos.Filename[len("testdata/fixture/"):]+":"+strconv.Itoa(f.Pos.Line))
	}
	sort.Strings(got)
	// Lines 59-67: one violation each, every dropped error is reported too;
	// 72 is marked; 85-86: the helper with a Box parameter.
	want := []string{
		"discarded@fixture.go:59",
		"discarded@fixture.go:60",
		"discarded@fixture.go:61", "pool@fixture.go:61",
		"discarded@fixture.go:62", "pool@fixture.go:62",
		"discarded@fixture.go:63", "network@fixture.go:63",
		"discarded@fixture.go:64", "network@fixture.go:64",
		"discarded@fixture.go:65", "network@fixture.go:65",
		"discarded@fixture.go:66", "nested@fixture.go:66",
		"discarded@fixture.go:67", "nested@fixture.go:67",
		"discarded@fixture.go:85", "pool@fixture.go:85",
		"discarded@fixture.go:86",
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findings\n%v\nwant\n%v\n%v", got, want, findings)
	}
}
