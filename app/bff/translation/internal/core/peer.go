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
	"time"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	dialogpb "github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/pkg/translate"
)

// errUnreachable is a peer that is not the caller's to translate in: no
// dialog with the user or the chat, a channel it cannot read. The handlers
// answer it with PEER_ID_INVALID.
var errUnreachable = errors.New("translation: the peer is not reachable")

// chatPeer is the dialog a request names, as peer_translation_settings
// and the copies of messages name it: Saved Messages are the dialog with
// oneself.
func (c *TranslationCore) chatPeer(in *mtproto.InputPeer) (translate.TranslationPeer, error) {
	if in == nil {
		return translate.TranslationPeer{}, errUnreachable
	}
	p := mtproto.FromInputPeer2(c.MD.UserId, in)
	switch p.PeerType {
	case mtproto.PEER_SELF:
		return translate.TranslationPeer{Type: mtproto.PEER_USER, Id: c.MD.UserId}, nil
	case mtproto.PEER_USER, mtproto.PEER_CHAT, mtproto.PEER_CHANNEL:
		if p.PeerId <= 0 {
			return translate.TranslationPeer{}, errUnreachable
		}
		return translate.TranslationPeer{Type: p.PeerType, Id: p.PeerId}, nil
	}
	// An inputPeerUserFromMessage or an empty peer: the clients name a
	// chat they show by its own peer.
	return translate.TranslationPeer{}, errUnreachable
}

// checkDialog tells whether the caller has a dialog with the user or the
// chat peer; Saved Messages are always there.
func (c *TranslationCore) checkDialog(peer translate.TranslationPeer) error {
	if peer.Type == mtproto.PEER_USER && peer.Id == c.MD.UserId {
		return nil
	}
	_, err := c.svcCtx.Dialogs.DialogGetDialogById(c.ctx, &dialogpb.TLDialogGetDialogById{UserId: c.MD.UserId, PeerType: peer.Type, PeerId: peer.Id})
	if isError(err, mtproto.ErrPeerIdInvalid) {
		return errUnreachable
	}
	return err
}

// readableChannel returns the channel of in when the caller may read it: the
// access hash is the caller's, the channel is not deleted, and the caller is
// a member, or anyone not kicked from a public channel, or a reader of the
// discussion group through its channel (the rule of the channels area,
// app/bff/channels/internal/core/channels.go, readable).
func (c *TranslationCore) readableChannel(in *mtproto.InputPeer) (*channelpb.MutableChannel, error) {
	mc, err := c.svcCtx.Channels.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: in.GetChannelId(), Id: []int64{c.MD.UserId}})
	if isError(err, mtproto.ErrChannelInvalid) || isError(err, mtproto.ErrChannelPrivate) {
		return nil, errUnreachable
	}
	if err != nil {
		return nil, err
	}
	if mc.GetChannel().GetAccessHash() != in.GetAccessHash() || mc.GetChannel().GetDeleted() {
		return nil, errUnreachable
	}
	p, _ := mc.GetParticipant(c.MD.UserId)
	if p.Member() {
		return mc, nil
	}
	if (mc.GetChannel().Public() || p.GetState() == channelpb.StateLinked) && mc.ToChat(c.MD.UserId, int32(time.Now().Unix())).GetPredicateName() == mtproto.Predicate_channel {
		return mc, nil
	}
	return nil, errUnreachable
}
