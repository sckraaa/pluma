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

package conf

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestMySQLConnectionBudget: the services' connection pools fit under
// MySQL's max_connections, or a burst ends in "Too many connections". Each
// service with a Mysql section opens its own pool (Active) and, for the box
// transactions (pkg/userbox.Open), go-zero's pool of up to 64 on the same
// DSN; administration and backups need a few more.
func TestMySQLConnectionBudget(t *testing.T) {
	const goZeroPool, admin = 64, 20
	files, _ := filepath.Glob(filepath.Join(repoRoot, "teamgramd/etc2/*.yaml"))
	need := admin
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var c struct {
			Mysql *struct {
				Active int `yaml:"Active"`
			} `yaml:"Mysql"`
		}
		if err := yaml.Unmarshal(data, &c); err != nil {
			t.Fatal(err)
		}
		if c.Mysql != nil {
			need += c.Mysql.Active + goZeroPool
		}
	}
	compose, err := os.ReadFile(filepath.Join(repoRoot, "docker-compose-env.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`--max-connections=(\d+)`).FindSubmatch(compose)
	if m == nil {
		t.Fatal("docker-compose-env.yaml: mysql has no --max-connections")
	}
	limit, _ := strconv.Atoi(string(m[1]))
	if need > limit {
		t.Errorf("the pools may open %d connections, MySQL allows %d", need, limit)
	}
	t.Logf("pools up to %d of %d connections", need, limit)
}
