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

package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/channels/internal/svc"
)

// served are the methods of the service that are served (task 038 on).
var served = map[string]bool{
	"ChannelsCreateChannel":            true,
	"ChannelsGetChannels":              true,
	"ChannelsGetFullChannel":           true,
	"ChannelsGetParticipant":           true,
	"ChannelsGetMessages":              true,
	"ChannelsExportMessageLink":        true,
	"ChannelsJoinChannel24B524C5":      true,
	"ChannelsJoinChannel7F6A1E22":      true,
	"ChannelsLeaveChannel":             true,
	"ChannelsGetParticipants":          true,
	"ChannelsReadHistory":              true,
	"ChannelsDeleteMessages":           true,
	"ChannelsInviteToChannelC9E33D54":  true,
	"ChannelsInviteToChannel199F3A6C":  true,
	"ChannelsGetAdminedPublicChannels": true,
	"ChannelsEditTitle":                true,
	"ChannelsDeleteChannel":            true,
	"ChannelsEditAdmin":                true,
	"ChannelsEditBanned":               true,
	"ChannelsEditCreator":              true,
	"ChannelsDeleteHistory9BAA9647":    true,
	"ChannelsDeleteHistoryAF369D42":    true,
	"ChannelsDeleteParticipantHistory": true,
	"ChannelsReadMessageContents":      true,
	"ChannelsEditPhoto":                true,
	"ChannelsSetDiscussionGroup":       true,
	"ChannelsGetGroupsForDiscussion":   true,
	"ChannelsToggleSignatures":         true,
	"ChannelsToggleSlowMode":           true,
	"ChannelsTogglePreHistoryHidden":   true,
	"ChannelsToggleParticipantsHidden": true,
	"ChannelsGetAdminLog":              true,
	"ChannelsSetStickers":              true,

	// Task 078: the owner who leaves, the channels to leave at the limit.
	"ChannelsGetFutureCreatorAfterLeave": true,
	"ChannelsGetInactiveChannels":        true,
}

// Every method of the service not served yet answers ERR_ENTERPRISE_IS_BLOCKED
// (400), never an internal error, which Android's and Web K's transports
// retry without end.
func TestNotServed(t *testing.T) {
	s := New(&svc.ServiceContext{})
	iface := reflect.TypeOf((*mtproto.RPCChannelsServer)(nil)).Elem()
	v := reflect.ValueOf(s)
	for i := range iface.NumMethod() {
		m := iface.Method(i)
		if served[m.Name] {
			continue
		}
		req := reflect.New(m.Type.In(1).Elem())
		out := v.MethodByName(m.Name).Call([]reflect.Value{reflect.ValueOf(context.Background()), req})
		err, _ := out[1].Interface().(error)
		if !errors.Is(err, mtproto.ErrEnterpriseIsBlocked) {
			t.Errorf("%s: %v, want ERR_ENTERPRISE_IS_BLOCKED", m.Name, err)
		}
	}
	if iface.NumMethod() == 0 {
		t.Fatal("no methods")
	}
}
