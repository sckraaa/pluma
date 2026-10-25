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

package conformance

import (
	"os/exec"
	"strings"
	"syscall"
)

// hostModel returns the model of the Mac and its macOS version.
func hostModel() (model, system string) {
	model, _ = syscall.Sysctl("hw.model")
	if model == "" {
		model = "Mac"
	}
	system = "macOS"
	if v, err := syscall.Sysctl("kern.osproductversion"); err == nil && v != "" {
		system += " " + v
	}
	return model, system
}

// platformLocale is the language and region chosen in System Settings; a
// program started from Finder or an IDE has no LANG.
func platformLocale() string {
	out, err := exec.Command("/usr/bin/defaults", "read", "-g", "AppleLocale").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
