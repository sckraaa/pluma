package testinfra

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// SchemaDir is the directory of the SQL schema and migrations, relative to the
// repository root. Production applies it through docker-entrypoint-initdb.d
// (docker-compose-env.yaml).
const SchemaDir = "teamgramd/deploy/sql"

// SchemaFiles returns the paths of the schema files in the order the MySQL
// image applies them on the first start: every *.sql file of SchemaDir sorted
// by name in byte order (1_teamgram.sql, the migrate-*.sql files, z_init.sql
// and the z_ files that change its data).
// Note that migrate-20261021-pfs.sql sorts before migrate-20261021.sql.
func SchemaFiles() ([]string, error) {
	root, err := RepoRoot()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, SchemaDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	// The image's entrypoint globs /docker-entrypoint-initdb.d/* in the C
	// locale: plain byte order of the names.
	slices.SortFunc(files, func(a, b string) int { return strings.Compare(filepath.Base(a), filepath.Base(b)) })
	if len(files) == 0 {
		return nil, fmt.Errorf("testinfra: no .sql files in %s", dir)
	}
	return files, nil
}

// SchemaFilesBefore returns the schema files that the MySQL image applies
// before name (a file name in SchemaDir), for tests of a migration against a
// database in the state it finds in production. It fails if name is not a
// schema file.
func SchemaFilesBefore(name string) ([]string, error) {
	files, err := SchemaFiles()
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(files, func(f string) bool { return filepath.Base(f) == name })
	if i < 0 {
		return nil, fmt.Errorf("testinfra: %s is not a file of %s", name, SchemaDir)
	}
	return files[:i], nil
}

// SchemaFile returns the path of the schema file name.
func SchemaFile(name string) (string, error) {
	root, err := RepoRoot()
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, SchemaDir, name)
	if _, err = os.Stat(path); err != nil {
		return "", err
	}
	return path, nil
}
