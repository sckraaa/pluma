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

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/teamgram/teamgram-server/pkg/catalog/importer"
)

type fakeCatalogImporter struct{ dir string }

func (f *fakeCatalogImporter) Run(_ context.Context, dir string) (*importer.Result, error) {
	f.dir = dir
	return &importer.Result{Answers: 21, Sets: 330, Documents: 20000, NewDocuments: 12}, nil
}

func TestCatalogCommand(t *testing.T) {
	fake := &fakeCatalogImporter{}
	old := openCatalogImporter
	openCatalogImporter = func(string) (catalogImporter, error) { return fake, nil }
	defer func() { openCatalogImporter = old }()

	var out bytes.Buffer
	if err := catalogCommand([]string{"catalog", "import", "/srv/catalog/run1"}, "bff.yaml", &out); err != nil {
		t.Fatal(err)
	}
	if fake.dir != "/srv/catalog/run1" || !strings.Contains(out.String(), "21 answers, 330 sets, 20000 documents (12 stored now)") {
		t.Fatalf("dir %q, output %q", fake.dir, out.String())
	}
	for _, args := range [][]string{{"catalog"}, {"catalog", "import"}, {"catalog", "export", "x"}, {"catalog", "import", "a", "b"}} {
		if err := catalogCommand(args, "bff.yaml", &out); !errors.Is(err, errUsage) {
			t.Errorf("%v: %v", args, err)
		}
	}
}

func TestCatalogReceiveUsage(t *testing.T) {
	var out bytes.Buffer
	for _, args := range [][]string{{"catalog", "receive"}, {"catalog", "receive", "-listen", ":1"}, {"catalog", "receive", "-dir", "x", "extra"}} {
		if err := catalogCommand(args, "", &out); !errors.Is(err, errUsage) {
			t.Errorf("%v: %v", args, err)
		}
	}
}
