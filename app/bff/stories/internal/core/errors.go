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
	"errors"
	"fmt"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/stories"

	"google.golang.org/grpc/status"
)

// The errors of the stories that the schema's list does not have. Android
// names the ones it reads: STORIES_TOO_MUCH, STORY_SEND_FLOOD_WEEKLY_<until>,
// STORY_SEND_FLOOD_MONTHLY_<until>, PREMIUM_ACCOUNT_REQUIRED, BOOSTS_REQUIRED
// (StoriesController.java:4558-4646). Boosts and Premium never stop a story
// here (ADR 0041).
var (
	// errStoryIdInvalid: no such story, or one the caller may not see. The
	// name is from memory of the methods' error lists on core.telegram.org
	// and was not read again for this task; no client reads it by name.
	errStoryIdInvalid = status.Error(mtproto.ErrBadRequest, "STORY_ID_INVALID")
	// errStoriesTooMuch: the peer has the most stories the feeds hold.
	errStoriesTooMuch = status.Error(mtproto.ErrBadRequest, "STORIES_TOO_MUCH")
	// errUserPublicMissing: a link of a story needs the public username of
	// its owner; the name of Telegram's error for it was not read.
	errUserPublicMissing = status.Error(mtproto.ErrBadRequest, "USER_PUBLIC_MISSING")
)

// limitError turns a limit of pkg/stories into the error the clients read.
func limitError(err error) error {
	var flood *stories.FloodError
	switch {
	case errors.Is(err, stories.ErrTooMany):
		return errStoriesTooMuch
	case errors.As(err, &flood):
		if flood.Monthly {
			return status.Error(mtproto.ErrBadRequest, fmt.Sprintf("STORY_SEND_FLOOD_MONTHLY_%d", flood.Until))
		}
		return status.Error(mtproto.ErrBadRequest, fmt.Sprintf("STORY_SEND_FLOOD_WEEKLY_%d", flood.Until))
	}
	return err
}

// refused tells an error that says "no" (a 4xx of the schema: the peer's
// privacy, a restriction) from one that may pass at the next try.
func refused(err error) bool {
	s, ok := status.FromError(err)
	return ok && s.Code() >= 400 && s.Code() < 500
}
