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
	"sync"
	"time"

	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// freeFileParts is upload_max_fileparts_default: 2 GB in parts of 512 KB
// without Premium, 4 GB with it (task 025; dfs takes up to the Premium
// limit, core.telegram.org/api/files).
const freeFileParts = 4000

// premiumMemo keeps the answer for the parts of one big upload: each part
// is a request.
var premiumMemo = struct {
	sync.Mutex
	until map[int64]time.Time
	value map[int64]bool
}{until: map[int64]time.Time{}, value: map[int64]bool{}}

// premiumMemoFor bounds how old a kept answer is.
const premiumMemoFor = time.Minute

// isPremium tells whether the caller has Premium, remembered for a minute.
func (c *FilesCore) isPremium() (bool, error) {
	id := c.MD.UserId
	now := time.Now()
	premiumMemo.Lock()
	if until, ok := premiumMemo.until[id]; ok && now.Before(until) {
		v := premiumMemo.value[id]
		premiumMemo.Unlock()
		return v, nil
	}
	premiumMemo.Unlock()

	me, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: id})
	if err != nil {
		return false, err
	}
	v := me.Premium()
	premiumMemo.Lock()
	premiumMemo.until[id] = now.Add(premiumMemoFor)
	premiumMemo.value[id] = v
	premiumMemo.Unlock()
	return v, nil
}
