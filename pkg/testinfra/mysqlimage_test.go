package testinfra

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/moby/moby/api/types/image"
)

// TestMySQLImageKey: the key of a schema image changes with everything that
// shapes its datadir, and only with that.
func TestMySQLImageKey(t *testing.T) {
	const base = "mysql:8.0.46@sha256:7dcddc01f13bab2f15cde676d44d01f61fc9f99fe7785e86196dfc07d358ae2b"
	schema := []schemaFile{{"1_a.sql", []byte("create table a (id int);")}, {"2_b.sql", []byte("create table b (id int);")}}
	key := mysqlImageKey(base, schema)
	if len(key) != 32 {
		t.Fatalf("key %q, want 32 hex digits", key)
	}
	if again := mysqlImageKey(base, slices.Clone(schema)); again != key {
		t.Fatalf("key of the same schema %s, then %s", key, again)
	}

	changed := map[string]string{
		"base image":   mysqlImageKey("mysql:8.0.47@sha256:"+base[len(base)-64:], schema),
		"file order":   mysqlImageKey(base, []schemaFile{schema[1], schema[0]}),
		"file name":    mysqlImageKey(base, []schemaFile{schema[0], {"3_b.sql", schema[1].data}}),
		"content":      mysqlImageKey(base, []schemaFile{schema[0], {"2_b.sql", []byte("create table b (id bigint);")}}),
		"file added":   mysqlImageKey(base, append(slices.Clone(schema), schemaFile{"z.sql", nil})),
		"file gone":    mysqlImageKey(base, schema[:1]),
		"empty":        mysqlImageKey(base, nil),
		"name/content": mysqlImageKey(base, []schemaFile{{"1_a.sql", []byte("create table a (id int);2_b.sql")}}),
	}
	args := MySQLServerArgs
	MySQLServerArgs = append(slices.Clone(args), "--sql-mode=")
	changed["server options"] = mysqlImageKey(base, schema)
	MySQLServerArgs = args
	for what, k := range changed {
		if k == key {
			t.Errorf("%s: the key stays %s", what, key)
		}
	}
}

func TestReadSchemaFiles(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.sql"), filepath.Join(dir, "sub", "a.sql")
	if err := os.MkdirAll(filepath.Dir(b), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("select 1;"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := readSchemaFiles([]string{a, b}); err == nil {
		t.Fatal("two files named a.sql accepted")
	}
	files, err := readSchemaFiles([]string{a})
	if err != nil || len(files) != 1 || files[0].name != "a.sql" || string(files[0].data) != "select 1;" {
		t.Fatalf("readSchemaFiles: %v, %v", files, err)
	}

	// The archive puts them in the init directory, in order.
	tr := tar.NewReader(initDirArchive([]schemaFile{{"2.sql", []byte("x")}, {"1.sql", []byte("yz")}}))
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		names = append(names, h.Name+"="+string(b))
	}
	if want := []string{"docker-entrypoint-initdb.d/2.sql=x", "docker-entrypoint-initdb.d/1.sql=yz"}; !slices.Equal(names, want) {
		t.Fatalf("archive %q, want %q", names, want)
	}
}

// TestStaleMySQLImages: the last used schema images stay, untagged copies go,
// and nothing else is touched.
func TestStaleMySQLImages(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	used := func(key string, ago time.Duration) {
		p := filepath.Join(dir, "mysql-"+key)
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, now.Add(-ago), now.Add(-ago)); err != nil {
			t.Fatal(err)
		}
	}
	ours := map[string]string{mysqlImageLabel: mysqlImageValue}
	img := func(id string, created time.Duration, labels map[string]string, tags ...string) image.Summary {
		return image.Summary{ID: id, Created: now.Add(-created).Unix(), Labels: labels, RepoTags: tags}
	}
	// Built long ago, used just now: it stays over newer ones used before.
	used("old", time.Minute)
	used("new1", time.Hour)
	used("new2", 2*time.Hour)
	images := []image.Summary{
		img("sha256:1", 30*24*time.Hour, ours, mysqlImageRepo+":old"),
		img("sha256:2", time.Hour, ours, mysqlImageRepo+":new1"),
		img("sha256:3", 2*time.Hour, ours, mysqlImageRepo+":new2"),
		// No lock file: its creation is its last use.
		img("sha256:4", 3*time.Hour, ours, mysqlImageRepo+":nolock"),
		img("sha256:5", time.Minute, ours),                     // untagged copy
		img("sha256:6", time.Minute, ours, "<none>:<none>"),    // untagged copy
		img("sha256:7", 90*24*time.Hour, ours, "someone:else"), // tagged by someone
		img("sha256:8", 90*24*time.Hour, nil, mysqlImageRepo+":nolabel"),
		img("sha256:9", 90*24*time.Hour, map[string]string{mysqlImageLabel: mysqlBuildValue}),
	}
	got := staleMySQLImages(images, dir, 2)
	want := []string{"sha256:5", "sha256:6", mysqlImageRepo + ":new2", mysqlImageRepo + ":nolock"}
	if !slices.Equal(got, want) {
		t.Fatalf("stale %q, want %q", got, want)
	}
	if got = staleMySQLImages(images, dir, 10); !slices.Equal(got, want[:2]) {
		t.Fatalf("stale with room for all %q, want %q", got, want[:2])
	}
}
