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

package core

import (
	"encoding/json"

	"github.com/teamgram/teamgram-server/app/messenger/push/internal/notify"
)

// apnsBody is what APNs carries: the encrypted payload p, and for an alert
// a placeholder iOS shows only if the Notification Service Extension
// cannot replace it. mutable-content wakes the extension.
func apnsBody(push *notify.IOSPush, p string) []byte {
	aps := map[string]any{}
	if push.Alert {
		aps["alert"] = map[string]string{"body": push.Placeholder}
		aps["mutable-content"] = 1
		if push.Sound {
			aps["sound"] = "default"
		}
		if push.ThreadId != "" {
			aps["thread-id"] = push.ThreadId
		}
	} else {
		aps["content-available"] = 1
	}
	b, _ := json.Marshal(map[string]any{"aps": aps, "p": p})
	return b
}
