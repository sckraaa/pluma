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

package sfu

// The routes of JoinRequest.routes (task 116, ADR 0074): a route names a
// set of public addresses of a node, its own or those of a relay in front
// of it, whose candidates the join answer carries.
const (
	// RouteDirect is the node's own public addresses (Media.PublicIPs).
	RouteDirect = "direct"
	// MaxRoutes is the most routes one join names.
	MaxRoutes = 8
	// maxRouteLen bounds a name, which is also a label of the metrics.
	maxRouteLen = 16
)

// ValidRoute reports whether s is a well-formed route name: 1 to 16
// lower-case letters, digits and '-', starting with a letter.
func ValidRoute(s string) bool {
	if len(s) == 0 || len(s) > maxRouteLen || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		ok := c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}
