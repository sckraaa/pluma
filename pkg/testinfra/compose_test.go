package testinfra

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestComposeServices keeps the integration tests on what production runs:
// the images of docker-compose-env.yaml (pinned by digest) and its server
// settings.
func TestComposeServices(t *testing.T) {
	for _, service := range []string{"mysql", "redis", "kafka"} {
		if _, err := ComposeImage(service); err != nil {
			t.Error(err)
		}
	}

	c, err := readCompose()
	if err != nil {
		t.Fatal(err)
	}
	mysqlArgs := asStrings(t, c.Services["mysql"].Command)
	if !slices.Equal(mysqlArgs, MySQLServerArgs) {
		t.Errorf("mysql command is %q, MySQLServerArgs is %q", mysqlArgs, MySQLServerArgs)
	}

	redisArgs := asStrings(t, c.Services["redis"].Command)
	if len(redisArgs) < 3 || redisArgs[0] != "redis-server" || !slices.Contains(redisArgs, "--requirepass") ||
		!slices.Contains(redisArgs, "--appendonly") {
		t.Errorf("redis command %q: StartRedis runs redis-server --appendonly yes --requirepass", redisArgs)
	}

	if got := envValue(c.Services["kafka"].Environment, "KAFKA_NUM_PARTITIONS"); got != "3" {
		t.Errorf("kafka KAFKA_NUM_PARTITIONS is %q, StartKafka uses 3", got)
	}
}

func TestComposeImageRequiresDigest(t *testing.T) {
	for image, pinned := range map[string]bool{
		"mysql:8.0.46@sha256:" + strings.Repeat("ab", 32):               true,
		"quay.io/coreos/etcd:v3.5.11@sha256:" + strings.Repeat("0", 64): true,
		"mysql:8.0.46": false,
		"mysql@sha256:" + strings.Repeat("ab", 32): false,
		"mysql:8.0@sha256:abc":                     false,
	} {
		if got := pinnedImage.MatchString(image); got != pinned {
			t.Errorf("%s: pinned %v, want %v", image, got, pinned)
		}
	}
}

// TestRyukImagePinned: RyukImage is the reaper image of the testcontainers-go
// version in go.mod.
func TestRyukImagePinned(t *testing.T) {
	byDigest, tag, err := reaperRefs()
	if err != nil {
		t.Fatal(err)
	}
	if !pinnedImage.MatchString(RyukImage) {
		t.Fatalf("RyukImage %s is not pinned as name:tag@sha256:digest", RyukImage)
	}
	if _, digest, _ := strings.Cut(RyukImage, "@"); byDigest != "testcontainers/ryuk@"+digest || tag+"@"+digest != RyukImage {
		t.Fatalf("RyukImage %s: by digest %s, tag %s", RyukImage, byDigest, tag)
	}
}

// envValue returns the value of name in a compose environment section.
func envValue(env any, name string) string {
	switch e := env.(type) {
	case map[string]any:
		return fmt.Sprint(e[name])
	case []any:
		for _, x := range e {
			if s, ok := x.(string); ok && strings.HasPrefix(s, name+"=") {
				return strings.TrimPrefix(s, name+"=")
			}
		}
	}
	return ""
}

func asStrings(t *testing.T, v any) []string {
	t.Helper()
	list, ok := v.([]any)
	if !ok {
		t.Fatalf("command is %T, want a list", v)
	}
	out := make([]string, 0, len(list))
	for _, x := range list {
		s, ok := x.(string)
		if !ok {
			t.Fatalf("command element %v is %T", x, x)
		}
		out = append(out, s)
	}
	return out
}

func TestSchemaFilesOrder(t *testing.T) {
	files, err := SchemaFiles()
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = filepath.Base(f)
	}
	if names[0] != "1_teamgram.sql" || names[len(names)-1] != "z_init.sql" {
		t.Fatalf("schema files must start with 1_teamgram.sql and end with z_init.sql: %v", names)
	}
	pfs := slices.Index(names, "migrate-20261021-pfs.sql")
	allowlist := slices.Index(names, "migrate-20261021.sql")
	if pfs < 0 || allowlist < 0 || pfs > allowlist {
		t.Fatalf("migrate-20261021-pfs.sql must come right before migrate-20261021.sql (byte order): %v", names)
	}

	before, err := SchemaFilesBefore("migrate-20261021-pfs.sql")
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != pfs || filepath.Base(before[len(before)-1]) != names[pfs-1] {
		t.Fatalf("SchemaFilesBefore returned %d files, want %d", len(before), pfs)
	}
	if _, err = SchemaFilesBefore("no-such.sql"); err == nil {
		t.Fatal("SchemaFilesBefore accepted an unknown file")
	}
}
