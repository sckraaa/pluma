package conf

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// repoRoot is the repository root relative to this package.
const repoRoot = "../.."

// configVars returns the ${NAME} variables referenced by the configs matching
// glob, mapped to the files that use them.
func configVars(t *testing.T, glob string) map[string][]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(repoRoot, glob))
	if err != nil || len(files) == 0 {
		t.Fatalf("no configs match %s: %v", glob, err)
	}
	vars := map[string][]string{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range envRef.FindAllSubmatch(data, -1) {
			vars[string(m[1])] = append(vars[string(m[1])], filepath.Base(f))
		}
	}
	return vars
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestDeployConfigVariablesAreProvided keeps the service configs, .env.example
// and docker-compose.yaml in sync: every variable a config needs is documented
// for operators and passed into the teamgram container.
func TestDeployConfigVariablesAreProvided(t *testing.T) {
	documented := map[string]bool{}
	f, err := os.Open(filepath.Join(repoRoot, ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if name, _, ok := strings.Cut(line, "="); ok && !strings.HasPrefix(line, "#") {
			documented[name] = true
		}
	}

	for _, glob := range []string{"teamgramd/etc/*.yaml", "teamgramd/etc2/*.yaml"} {
		for _, name := range sortedKeys(configVars(t, glob)) {
			if !documented[name] {
				t.Errorf("%s uses ${%s}, which .env.example does not document", glob, name)
			}
		}
	}

	compose, err := os.ReadFile(filepath.Join(repoRoot, "docker-compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for name, files := range configVars(t, "teamgramd/etc2/*.yaml") {
		required := regexp.MustCompile(`(?m)^\s+` + name + `: \$\{` + name + `:\?`)
		if !required.Match(compose) {
			t.Errorf("teamgramd/etc2 (%s) uses ${%s}, but docker-compose.yaml does not pass it as a required variable", strings.Join(files, ", "), name)
		}
	}
}

// TestDeployConfigsHaveNoDefaultCredentials guards against reintroducing
// hard-coded infrastructure credentials into the service configs.
func TestDeployConfigsHaveNoDefaultCredentials(t *testing.T) {
	forbidden := []string{
		"root:@tcp",            // MySQL root without a password
		"teamgram:teamgram@",   // default MySQL application password
		"miniostorage",         // default MinIO secret
		"AccessKeyID: minio",   // default MinIO user
		"12240908862933197005", // revoked server RSA key
	}
	files, _ := filepath.Glob(filepath.Join(repoRoot, "teamgramd/etc*/*.yaml"))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range forbidden {
			if strings.Contains(string(data), s) {
				t.Errorf("%s contains %q", f, s)
			}
		}
	}
}

// TestDeployListeners: in Docker, published ports name an IPv4 address
// (without one Docker also publishes them on IPv6 through docker-proxy, and
// the gateway sees every IPv6 client as one bridge address); on a bare-metal
// host only the client transports listen on all interfaces.
func TestDeployListeners(t *testing.T) {
	port := regexp.MustCompile(`(?m)^\s+- "([^"]+)"`)
	for _, f := range []string{"docker-compose.yaml", "docker-compose-env.yaml"} {
		data, err := os.ReadFile(filepath.Join(repoRoot, f))
		if err != nil {
			t.Fatal(err)
		}
		inPorts := false
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			switch {
			case trimmed == "ports:":
				inPorts = true
				continue
			case inPorts && !strings.HasPrefix(trimmed, "- ") && !strings.HasPrefix(trimmed, "#") && trimmed != "":
				inPorts = false
			}
			if !inPorts {
				continue
			}
			if m := port.FindStringSubmatch(line); m != nil && !strings.HasPrefix(m[1], "0.0.0.0:") && !strings.HasPrefix(m[1], "127.0.0.1:") {
				t.Errorf("%s publishes %q without an IPv4 address", f, m[1])
			}
		}
	}

	clientFacing := map[string]bool{"gnetway.yaml": true}
	files, _ := filepath.Glob(filepath.Join(repoRoot, "teamgramd/etc/*.yaml"))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, "0.0.0.0") {
				continue
			}
			if !clientFacing[filepath.Base(f)] {
				t.Errorf("%s listens on all interfaces: %q", f, trimmed)
			}
		}
	}
}
