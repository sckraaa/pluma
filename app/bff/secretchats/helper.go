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

package secretchats_helper

import (
	"github.com/teamgram/teamgram-server/app/bff/secretchats/internal/server/grpc/service"
)

// New returns RPCSecretChats: the Diffie-Hellman group every call starts
// with (task 068).
func New() *service.Service {
	return service.New()
}
