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

package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrderMatchesTheInitDirectory(t *testing.T) {
	files, err := Load("../../teamgramd/deploy/sql")
	if err != nil {
		t.Fatal(err)
	}
	if files[0].Name != "1_teamgram.sql" {
		t.Fatalf("first %s, want 1_teamgram.sql", files[0].Name)
	}
	// The seed z_init.sql runs after every schema file; only z_* files that
	// change its rows come after it.
	seed := -1
	for i, f := range files {
		if f.Name == "z_init.sql" {
			seed = i
		}
	}
	if seed < 0 {
		t.Fatal("no z_init.sql")
	}
	for i, f := range files {
		if z := strings.HasPrefix(f.Name, "z_"); z != (i >= seed) {
			t.Fatalf("%s is on the wrong side of z_init.sql", f.Name)
		}
	}
	for i := 1; i < len(files); i++ {
		if files[i-1].Name >= files[i].Name {
			t.Fatalf("%s before %s", files[i-1].Name, files[i].Name)
		}
	}
	for _, f := range files {
		if strings.Contains(strings.ToUpper(f.SQL), "DELIMITER") {
			t.Errorf("%s uses DELIMITER, a mysql client command the server does not understand", f.Name)
		}
	}
}

func TestLoadEmptyDir(t *testing.T) {
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("empty directory accepted")
	}
}

func TestPendingAndChecksums(t *testing.T) {
	dir := t.TempDir()
	for name, text := range map[string]string{"a.sql": "SELECT 1;", "b.sql": "SELECT 2;", "c.sql": "SELECT 3;"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	p, err := Pending(files, map[string]string{"a.sql": files[0].Sum})
	if err != nil || len(p) != 2 || p[0].Name != "b.sql" || p[1].Name != "c.sql" {
		t.Fatalf("pending %v, %v", p, err)
	}
	if _, err := Pending(files, map[string]string{"a.sql": strings.Repeat("0", 64)}); err == nil || !strings.Contains(err.Error(), "changed after it was applied") {
		t.Fatalf("edited applied file: %v", err)
	}
	if _, err := Pending(files, map[string]string{"zz.sql": strings.Repeat("0", 64)}); err == nil || !strings.Contains(err.Error(), "does not know") {
		t.Fatalf("unknown applied file: %v", err)
	}
}

func TestDSNEnablesMultiStatements(t *testing.T) {
	d, err := DSN("u:p@tcp(mysql:3306)/teamgram?charset=utf8mb4&parseTime=true")
	if err != nil || !strings.Contains(d, "multiStatements=true") {
		t.Fatalf("%s %v", d, err)
	}
}
