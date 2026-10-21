package conf

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	zeroconf "github.com/zeromicro/go-zero/core/conf"
)

// envRef matches a ${NAME} reference to an environment variable.
var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Load loads a go-zero config file into v like conf.Load, after replacing
// every ${NAME} in it with the value of the environment variable NAME. This is
// how secrets (database and Redis passwords, MinIO keys, the RSA key
// fingerprint) reach the configs without being stored in them.
//
// Unlike conf.UseEnv (os.ExpandEnv), only the braced form is expanded, so any
// other '$' in a file is kept as is, and a reference to an unset variable is
// an error instead of an empty string: a missing secret stops the service at
// startup. A variable explicitly set to "" is allowed. Values are substituted
// verbatim, so keep them free of YAML/DSN syntax (generate secrets as hex).
func Load(path string, v any) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	content, err = ExpandEnv(content, os.LookupEnv)
	if err != nil {
		return err
	}

	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return zeroconf.LoadFromYamlBytes(content, v)
	case ".json":
		return zeroconf.LoadFromJsonBytes(content, v)
	case ".toml":
		return zeroconf.LoadFromTomlBytes(content, v)
	default:
		return fmt.Errorf("unrecognized file type: %s", path)
	}
}

// MustLoad is Load that exits the process on error, like conf.MustLoad.
func MustLoad(path string, v any) {
	if err := Load(path, v); err != nil {
		log.Fatalf("error: config file %s, %s", path, err.Error())
	}
}

// ExpandEnv replaces each ${NAME} in content with the value lookup returns
// for NAME. It fails, naming the variables but never their values, if a
// variable is not set or its value contains control characters (a newline
// would let a value inject extra config keys).
func ExpandEnv(content []byte, lookup func(string) (string, bool)) ([]byte, error) {
	var (
		missing = map[string]struct{}{}
		invalid = map[string]struct{}{}
	)

	out := envRef.ReplaceAllFunc(content, func(ref []byte) []byte {
		name := string(ref[2 : len(ref)-1])
		value, ok := lookup(name)
		switch {
		case !ok:
			missing[name] = struct{}{}
			return ref
		case strings.ContainsFunc(value, isControl):
			invalid[name] = struct{}{}
			return ref
		default:
			return []byte(value)
		}
	})

	var problems []string
	if len(missing) > 0 {
		problems = append(problems, "environment variables not set: "+joinKeys(missing))
	}
	if len(invalid) > 0 {
		problems = append(problems, "environment variables contain control characters: "+joinKeys(invalid))
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(problems, "; "))
	}

	return out, nil
}

func isControl(r rune) bool {
	return r < 0x20 || r == 0x7f
}

func joinKeys(m map[string]struct{}) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
