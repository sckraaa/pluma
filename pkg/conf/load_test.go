package conf

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeromicro/go-zero/core/stores/kv"
)

func lookupFrom(env map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := env[name]
		return v, ok
	}
}

func TestExpandEnv(t *testing.T) {
	env := map[string]string{
		"PASS":  "0123abcd",
		"EMPTY": "",
		"USER":  "teamgram",
		"NL":    "x\nInjected: true",
		"TAB":   "a\tb",
	}
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr string
	}{
		{name: "plain", in: `Pass: "${PASS}"`, want: `Pass: "0123abcd"`},
		{name: "several in one line", in: `DSN: ${USER}:${PASS}@tcp(mysql:3306)/teamgram`, want: `DSN: teamgram:0123abcd@tcp(mysql:3306)/teamgram`},
		{name: "explicitly empty", in: `Pass: "${EMPTY}"`, want: `Pass: ""`},
		{name: "unbraced dollar kept", in: `X: $PASS $ $$ ${ }`, want: `X: $PASS $ $$ ${ }`},
		{name: "invalid name kept", in: `X: ${1PASS} ${PASS-x}`, want: `X: ${1PASS} ${PASS-x}`},
		{name: "no references", in: "A: 1\nB: two\n", want: "A: 1\nB: two\n"},
		{name: "missing", in: `Pass: "${NOPE}" ${ALSO_NOPE} ${NOPE}`, wantErr: "environment variables not set: ALSO_NOPE, NOPE"},
		{name: "newline injection", in: `Pass: ${NL}`, wantErr: "control characters: NL"},
		{name: "tab", in: `Pass: ${TAB}`, wantErr: "control characters: TAB"},
		{name: "missing and invalid", in: `${NOPE} ${NL}`, wantErr: "not set: NOPE; environment variables contain control characters: NL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExpandEnv([]byte(tt.in), lookupFrom(env))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
				}
				if strings.Contains(err.Error(), "Injected") {
					t.Fatalf("error leaks a value: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

type testConfig struct {
	Name string
	DSN  string
	KV   kv.KvConf
}

func TestLoad(t *testing.T) {
	const yaml = `Name: test
DSN: "teamgram:${TEST_MYSQL_PASSWORD}@tcp(mysql:3306)/teamgram?charset=utf8mb4"
KV:
  - Host: redis:6379
    Pass: "${TEST_REDIS_PASSWORD}"
`
	path := filepath.Join(t.TempDir(), "svc.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("missing variable", func(t *testing.T) {
		os.Unsetenv("TEST_MYSQL_PASSWORD")
		os.Unsetenv("TEST_REDIS_PASSWORD")
		var c testConfig
		err := Load(path, &c)
		if err == nil || !strings.Contains(err.Error(), "TEST_MYSQL_PASSWORD, TEST_REDIS_PASSWORD") {
			t.Fatalf("error = %v, want both variables named", err)
		}
	})

	t.Run("expanded", func(t *testing.T) {
		// A digits-only secret must stay a string thanks to the quotes.
		t.Setenv("TEST_MYSQL_PASSWORD", "deadbeef")
		t.Setenv("TEST_REDIS_PASSWORD", "123456")
		var c testConfig
		if err := Load(path, &c); err != nil {
			t.Fatal(err)
		}
		if c.DSN != "teamgram:deadbeef@tcp(mysql:3306)/teamgram?charset=utf8mb4" {
			t.Fatalf("DSN = %q", c.DSN)
		}
		if len(c.KV) != 1 || c.KV[0].Pass != "123456" || c.KV[0].Host != "redis:6379" {
			t.Fatalf("KV = %+v", c.KV)
		}
	})

	t.Run("unknown extension", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "svc.ini")
		if err := os.WriteFile(p, []byte("Name: x"), 0o600); err != nil {
			t.Fatal(err)
		}
		var c testConfig
		if err := Load(p, &c); err == nil {
			t.Fatal("want error for unknown extension")
		}
	})
}

// FuzzExpandEnv checks that expansion never panics, that every reference is
// replaced when all variables are set, and that text without references is
// returned unchanged.
func FuzzExpandEnv(f *testing.F) {
	f.Add([]byte(`DSN: ${A}:${B}@tcp(x)/y $C $$ ${`), "secret")
	f.Add([]byte("${}${${A}}"), "")
	f.Fuzz(func(t *testing.T, content []byte, value string) {
		lookup := func(string) (string, bool) { return value, true }
		out, err := ExpandEnv(content, lookup)
		if strings.ContainsFunc(value, isControl) {
			if envRef.Match(content) && err == nil {
				t.Fatal("control characters accepted")
			}
			return
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !envRef.Match(content) && !bytes.Equal(out, content) {
			t.Fatalf("content without references changed: %q -> %q", content, out)
		}
		if want := envRef.ReplaceAllLiteral(content, []byte(value)); !bytes.Equal(out, want) {
			t.Fatalf("got %q, want %q", out, want)
		}
	})
}
