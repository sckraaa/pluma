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

package dao

import (
	"testing"

	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dal/dataobject"
)

// The loaded users carry what their owner keeps of a contact: the close
// friend mark, and the row of the stories feeds the contact's stories are
// in (stories.togglePeerStoriesHidden moved them to the archive row, and
// user.stories_hidden says so).
func TestContactDataFlags(t *testing.T) {
	c := contactData(&dataobject.UserContactsDO{
		OwnerUserId:   1,
		ContactUserId: 2,
		CloseFriend:   true,
		StoriesHidden: true,
	})
	if c.GetUserId() != 1 || c.GetContactUserId() != 2 || !c.GetCloseFriend() || !c.GetStoriesHidden() {
		t.Errorf("contactData: %v", c)
	}
}
