package testinfra

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/teamgram/teamgram-server/pkg/outbox"

	"gopkg.in/yaml.v3"
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

	// Topics come from topics.sh only, in production (kafka-init, the
	// same image) and in the tests (StartKafka).
	if got := envValue(c.Services["kafka"].Environment, "KAFKA_AUTO_CREATE_TOPICS_ENABLE"); got != "false" {
		t.Errorf("kafka KAFKA_AUTO_CREATE_TOPICS_ENABLE is %q, want false (StartKafka sets it)", got)
	}
	if got := envValue(c.Services["kafka"].Environment, "KAFKA_NUM_PARTITIONS"); got != "" {
		t.Errorf("kafka KAFKA_NUM_PARTITIONS is %q: the partitions are topics.sh's", got)
	}
	if init, kafka := c.Services["kafka-init"].Image, c.Services["kafka"].Image; init != kafka {
		t.Errorf("kafka-init runs %q, kafka %q", init, kafka)
	}
}

// TestTopicsScript: topics.sh creates every topic the services use, with the
// partitions the relay expects.
func TestTopicsScript(t *testing.T) {
	root, err := RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join(root, TopicsScript))
	if err != nil {
		t.Fatal(err)
	}
	partitions := map[string]string{}
	for _, m := range regexp.MustCompile(`(?m)^\s+"(\S+) (\d+) \S+"$`).FindAllStringSubmatch(string(script), -1) {
		partitions[m[1]] = m[2]
	}
	used := map[string]string{}
	for file, keys := range map[string][][]string{
		"msg.yaml":  {{"Topics", "Inbox"}, {"Topics", "Sync"}, {"InboxConsumer", "Topic"}},
		"sync.yaml": {{"SyncConsumer", "Topic"}},
		"bff.yaml":  {{"SyncClient", "Topic"}},
	} {
		for _, dir := range []string{"teamgramd/etc", "teamgramd/etc2"} {
			b, err := os.ReadFile(filepath.Join(root, dir, file))
			if err != nil {
				t.Fatal(err)
			}
			var conf map[string]any
			if err = yaml.Unmarshal(b, &conf); err != nil {
				t.Fatalf("%s/%s: %v", dir, file, err)
			}
			for _, path := range keys {
				section, _ := conf[path[0]].(map[string]any)
				topic, _ := section[path[1]].(string)
				if topic == "" {
					t.Errorf("%s/%s: no %s", dir, file, strings.Join(path, "."))
					continue
				}
				used[topic] = strconv.Itoa(outbox.Partitions)
			}
		}
	}
	for topic, want := range used {
		if got := partitions[topic]; got != want {
			t.Errorf("topics.sh: %s with %q partitions, want %s", topic, got, want)
		}
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
		if v, ok := e[name]; ok {
			return fmt.Sprint(v)
		}
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
