// Package testinfra starts the real infrastructure of the server (MySQL with
// the production schema, Redis, Kafka) in throwaway Docker containers for
// integration tests (testcontainers-go, docs/ops/testing.md).
//
// The images are the ones docker-compose-env.yaml runs in production, read
// from that file, and the server settings match it (TestComposeServices keeps
// them in sync). MySQL gets its schema the way production does: every
// teamgramd/deploy/sql/*.sql applied by the image's docker-entrypoint-initdb.d
// in file name order. That runs once per set of schema files: the initialized
// datadir is kept in a local image (pluma-testinfra-mysql, labelled
// pluma.testinfra=mysql-schema; the last used ones stay), and each server
// starts from a copy of it in seconds.
//
// Integration tests carry the build tag "integration", so plain `go test ./...`
// never needs Docker. A test package usually starts what it needs once in
// TestMain:
//
//	func TestMain(m *testing.M) {
//		ctx := context.Background()
//		db, err := testinfra.StartMySQL(ctx)
//		if err != nil { ... }
//		code := m.Run()
//		_ = db.Terminate(ctx)
//		os.Exit(code)
//	}
//
// Containers are labelled for the testcontainers reaper (Ryuk), which removes
// them even when the test binary is killed; its image is pinned by digest too
// (RyukImage).
package testinfra

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// RepoRoot returns the root of the repository: the nearest directory above the
// working directory (a test runs in its package directory) whose go.mod is
// the server module.
func RepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if b, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil && isServerModule(b) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("testinfra: repository root not found above the working directory")
		}
		dir = parent
	}
}

func isServerModule(goMod []byte) bool {
	const module = "module github.com/teamgram/teamgram-server\n"
	return len(goMod) >= len(module) && string(goMod[:len(module)]) == module
}

// randomSecret returns a random hex string for a throwaway password.
func randomSecret() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("testinfra: crypto/rand: %v", err))
	}
	return hex.EncodeToString(b)
}
