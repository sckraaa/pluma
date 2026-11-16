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

import "context"

// PostDate is the date of the post msgId of the channel; ok is false when
// there is no such message, it is deleted or it is a service message.
func (d *Dao) PostDate(ctx context.Context, channelId int64, msgId int32) (date int64, ok bool, err error) {
	var dates []int64
	if err = d.db.QueryRowsPartial(ctx, &dates,
		"SELECT m.date2 FROM channel_messages m WHERE m.channel_id = ? AND m.id = ? AND "+isPost, channelId, msgId); err != nil {
		return 0, false, err
	}
	if len(dates) == 0 {
		return 0, false, nil
	}
	return dates[0], true, nil
}
