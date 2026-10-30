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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A canned answer is found by its case line; one for a type name that is no
// method of the layer is an error, since the proxy never matches it.
func TestFakes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake.go")
	src := "switch name {\n\tcase \"TLMessagesSaveGif\":\n\t\treturn ok\n\tcase \"TLMessagesReport\":\n\t\treturn ok\n}\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	fakes, err := loadFakes(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(fakes) != 2 || fakes["TLMessagesSaveGif"] != 2 || fakes["TLMessagesReport"] != 4 {
		t.Fatalf("fakes %v", fakes)
	}
	methods := []*method{{GoName: "MessagesSaveGif"}, {GoName: "MessagesReport8953AB4E"}}
	err = unknownFakes(fakes, methods)
	if err == nil || !strings.Contains(err.Error(), "TLMessagesReport") || strings.Contains(err.Error(), "TLMessagesSaveGif") {
		t.Errorf("unknown fakes: %v", err)
	}
	delete(fakes, "TLMessagesReport")
	if err = unknownFakes(fakes, methods); err != nil {
		t.Errorf("known fakes: %v", err)
	}
}
