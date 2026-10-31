// Copyright 2022 Teamgram Authors
//  All rights reserved.
//
// Author: Benqi (wubenqi@gmail.com)
//

package dialog

import (
	"github.com/teamgram/proto/mtproto"
)

type DialogsDataHelper struct {
	Dialogs  []*mtproto.Dialog  `json:"dialogs"`
	Messages []*mtproto.Message `json:"messages"`
	Chats    []*mtproto.Chat    `json:"chats"`
	Users    []*mtproto.User    `json:"users"`
}

// fix drops the dialogs whose top message or peer did not load. The users
// and chats stay as loaded: besides the peers they hold the account itself
// and the senders of the top messages of groups, which a client needs to name
// them (pluma, task 020: they were cut down to the peers of private chats,
// and Android showed a group's last message from "DELETED" after a fresh
// login).
func (m *DialogsDataHelper) fix() {
	var (
		dialogs  = make([]*mtproto.Dialog, 0, len(m.Dialogs))
		messages = make([]*mtproto.Message, 0, len(m.Messages))
	)

	for _, dlg := range m.Dialogs {
		var (
			topMessage *mtproto.Message
		)

		if dlg.TopMessage == 0 {
			continue
		}

		p := mtproto.FromPeer(dlg.Peer)
		for _, msg := range m.Messages {
			to := mtproto.FromPeer(msg.PeerId)
			if to.PeerType == p.PeerType && to.PeerId == p.PeerId && dlg.TopMessage == msg.Id {
				topMessage = msg
				break
			}
		}

		if topMessage != nil && m.hasPeer(p) {
			dialogs = append(dialogs, dlg)
			messages = append(messages, topMessage)
		}
	}

	m.Dialogs = dialogs
	m.Messages = messages
}

// hasPeer tells whether the user or chat of a dialog's peer loaded.
func (m *DialogsDataHelper) hasPeer(p *mtproto.PeerUtil) bool {
	switch p.PeerType {
	case mtproto.PEER_USER:
		for _, v := range m.Users {
			if v.Id == p.PeerId {
				return true
			}
		}
	case mtproto.PEER_CHAT, mtproto.PEER_CHANNEL:
		// chatForbidden and channelForbidden count as loaded, as before.
		for _, v := range m.Chats {
			if v.Id == p.PeerId {
				return true
			}
		}
	}
	return false
}

func (m *DialogsDataHelper) ToMessagesDialogs(count int32) *mtproto.Messages_Dialogs {
	m.fix()
	return mtproto.MakeTLMessagesDialogsSlice(&mtproto.Messages_Dialogs{
		Dialogs:  m.Dialogs,
		Messages: m.Messages,
		Chats:    m.Chats,
		Users:    m.Users,
		Count:    count,
	}).To_Messages_Dialogs()
}

// ToMessagesDialogsAll is the answer that holds the whole list of dialogs:
// messages.dialogs, the "full list of chats", while messages.dialogsSlice is
// an incomplete one (core.telegram.org/constructor/messages.dialogs). On a
// slice tdesktop keeps paging and treats the list as loaded only after an
// empty page it may not ask for (ApiWrap::requestMoreDialogs), and it shows
// the suggestions above the chat list only once the list is loaded.
func (m *DialogsDataHelper) ToMessagesDialogsAll() *mtproto.Messages_Dialogs {
	m.fix()
	return mtproto.MakeTLMessagesDialogs(&mtproto.Messages_Dialogs{
		Dialogs:  m.Dialogs,
		Messages: m.Messages,
		Chats:    m.Chats,
		Users:    m.Users,
	}).To_Messages_Dialogs()
}

func (m *DialogsDataHelper) ToMessagesPeerDialogs(state *mtproto.Updates_State) *mtproto.Messages_PeerDialogs {
	// m.fix()
	return mtproto.MakeTLMessagesPeerDialogs(&mtproto.Messages_PeerDialogs{
		Dialogs:  m.Dialogs,
		Messages: m.Messages,
		Users:    m.Users,
		Chats:    m.Chats,
		State:    state,
	}).To_Messages_PeerDialogs()
}
