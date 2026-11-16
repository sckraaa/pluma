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
	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/channelperm"

	"google.golang.org/grpc/status"
)

// errChatAdminRequired is the error of a viewer without the rights of the
// statistics: 403, as the pages of stats.getBroadcastStats and the other
// methods give it (core.telegram.org, read 2026-11-15, ADR 0066).
var errChatAdminRequired = status.Error(mtproto.ErrForbidden, "CHAT_ADMIN_REQUIRED")

// Kinds of channel a method asks for.
const (
	anyChannel = iota
	broadcastOnly
	megagroupOnly
)

// statsChannel is the channel of in whose statistics the viewer asks for,
// or the error of the method: CHANNEL_INVALID for a channel that is not
// there or a wrong access hash, CHANNEL_PRIVATE for one the viewer may not
// read (deleted, blocked, private and not a member), BROADCAST_REQUIRED or
// MEGAGROUP_REQUIRED for the wrong kind, CHAT_ADMIN_REQUIRED for a viewer
// without the rights (channelperm.CanViewStats).
func (c *StatisticsCore) statsChannel(in *mtproto.InputChannel, kind int) (*channelpb.MutableChannel, error) {
	id := in.GetChannelId()
	if in.GetPredicateName() != mtproto.Predicate_inputChannel || id == 0 {
		return nil, mtproto.ErrChannelInvalid
	}
	mc, err := c.mutableChannel(id)
	if err != nil {
		return nil, err
	}
	if mc.GetChannel().GetAccessHash() != in.GetAccessHash() {
		return nil, mtproto.ErrChannelInvalid
	}
	return mc, c.mayView(mc, kind)
}

// statsChannelById is statsChannel of a channel named by its id alone: the
// one of a zoom token, which only this server signs.
func (c *StatisticsCore) statsChannelById(id int64, kind int) (*channelpb.MutableChannel, error) {
	mc, err := c.mutableChannel(id)
	if err != nil {
		return nil, err
	}
	return mc, c.mayView(mc, kind)
}

func (c *StatisticsCore) mutableChannel(id int64) (*channelpb.MutableChannel, error) {
	return c.svcCtx.Channels.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: id, Id: []int64{c.MD.GetUserId()}})
}

// readable checks that the viewer may read mc: CHANNEL_PRIVATE when not.
func (c *StatisticsCore) readable(mc *channelpb.MutableChannel) error {
	ch := mc.GetChannel()
	me := c.MD.GetUserId()
	p, _ := mc.GetParticipant(me)
	// A deleted channel is no one's to read (task 055); a blocked one
	// neither.
	if ch.GetDeleted() || ch.GetBlocked() {
		return mtproto.ErrChannelPrivate
	}
	if !p.Member() && !(ch.Public() && mc.ToChat(me, int32(c.now)).GetPredicateName() == mtproto.Predicate_channel) {
		return mtproto.ErrChannelPrivate
	}
	return nil
}

// mayView checks the viewer's access to the statistics of mc.
func (c *StatisticsCore) mayView(mc *channelpb.MutableChannel, kind int) error {
	if err := c.readable(mc); err != nil {
		return err
	}
	ch := mc.GetChannel()
	p, _ := mc.GetParticipant(c.MD.GetUserId())
	switch {
	case kind == broadcastOnly && !ch.GetBroadcast():
		return mtproto.ErrBroadcastRequired
	case kind == megagroupOnly && ch.GetBroadcast():
		return mtproto.ErrMegagroupRequired
	}
	if !channelperm.CanViewStats(ch.Perm(), p.Perm()) {
		return errChatAdminRequired
	}
	return nil
}
