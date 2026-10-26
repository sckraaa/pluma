// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package dao

import (
	"context"
	"fmt"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	userPeerSettingsKeyPrefix = "user_peer_settings"
)

func genUserPeerSettingsCacheKey(id int64, peerType int32, peerId int64) string {
	return fmt.Sprintf("%s_%d_%d_%d", userPeerSettingsKeyPrefix, id, peerType, peerId)
}

func (d *Dao) GetUserPeerSettings(ctx context.Context, id int64, peerType int32, peerId int64) (*mtproto.PeerSettings, error) {
	settings := makePeerSettings(nil)
	err := d.CachedConn.QueryRow(
		ctx,
		settings,
		genUserPeerSettingsCacheKey(id, peerType, peerId),
		func(ctx context.Context, conn *sqlx.DB, v interface{}) error {
			settingsDO, err := d.UserPeerSettingsDAO.Select(ctx, id, peerType, peerId)
			if err != nil {
				return err
			}
			setPeerSettings(v.(*mtproto.PeerSettings), settingsDO)
			return nil
		})
	if err != nil {
		return nil, err
	}

	return settings, nil
}

// makePeerSettings is the settings of a peer a row gives: what
// user.getPeerSettings answers. Without a row, or for a hidden one, there
// is no bar.
func makePeerSettings(do *dataobject.UserPeerSettingsDO) *mtproto.PeerSettings {
	settings := mtproto.MakeTLPeerSettings(&mtproto.PeerSettings{}).To_PeerSettings()
	setPeerSettings(settings, do)
	return settings
}

// setPeerSettings sets the bar of settings from a row (makePeerSettings).
func setPeerSettings(settings *mtproto.PeerSettings, do *dataobject.UserPeerSettingsDO) {
	if do == nil || do.Hide {
		do = &dataobject.UserPeerSettingsDO{}
	}
	settings.ReportSpam = do.ReportSpam
	settings.AddContact = do.AddContact
	settings.BlockContact = do.BlockContact
	settings.ShareContact = do.ShareContact
	settings.NeedContactsException = do.NeedContactsException
	settings.ReportGeo = do.ReportGeo
	settings.Autoarchived = do.Autoarchived
	settings.GeoDistance = nil
	if do.GeoDistance != 0 {
		settings.GeoDistance = &wrapperspb.Int32Value{Value: do.GeoDistance}
	}
}

// Peer settings with seq (ADR 0020): a change of the settings, or of the
// contact a peer is, and updatePeerSettings for every device of the account
// are one box transaction of the account. The settings of the update are
// the ones stored after the change, as getPeerSettings gives them.

const peerSettingsColumns = "user_id, peer_type, peer_id, hide, report_spam, add_contact, block_contact, share_contact, need_contacts_exception, report_geo, autoarchived, invite_members, geo_distance"

// PeerSettingsOf returns the box owner's settings of the peers of peerType
// with peerIds, as stored, by peer id; a peer without them has none.
func PeerSettingsOf(b *userbox.Box, peerType int32, peerIds []int64) (map[int64]*mtproto.PeerSettings, error) {
	rows, err := peerSettingsRows(b, peerType, peerIds)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]*mtproto.PeerSettings, len(peerIds))
	for _, id := range peerIds {
		var do *dataobject.UserPeerSettingsDO
		if r, ok := rows[id]; ok {
			do = &r
		}
		out[id] = makePeerSettings(do)
	}
	return out, nil
}

// peerSettingsRows returns the box owner's rows of the peers of peerType
// with peerIds, hidden ones too, by peer id.
func peerSettingsRows(b *userbox.Box, peerType int32, peerIds []int64) (map[int64]dataobject.UserPeerSettingsDO, error) {
	rows := make(map[int64]dataobject.UserPeerSettingsDO, len(peerIds))
	if len(peerIds) == 0 {
		return rows, nil
	}
	var list []dataobject.UserPeerSettingsDO
	err := b.Tx().QueryRowsPartial(&list,
		"SELECT "+peerSettingsColumns+" FROM user_peer_settings WHERE user_id = ? AND peer_type = ? AND peer_id IN ("+inList(len(peerIds))+")",
		append([]any{b.UserId(), peerType}, int64Args(peerIds)...)...)
	for _, r := range list {
		rows[r.PeerId] = r
	}
	return rows, err
}

// EmitPeerSettings journals updatePeerSettings of the box owner's peer with
// settings and pushes it to every device of the account. users and chats
// are the peer as the account sees it.
func (d *Dao) EmitPeerSettings(b *userbox.Box, peerType int32, peerId int64, settings *mtproto.PeerSettings, users []*mtproto.User, chats []*mtproto.Chat) error {
	peer := (&mtproto.PeerUtil{SelfId: b.UserId(), PeerType: peerType, PeerId: peerId}).ToPeer()
	if peer == nil {
		return fmt.Errorf("user: peer settings of a peer of type %d", peerType)
	}
	return d.EmitSeqUpdate(b, mtproto.MakeTLUpdatePeerSettings(&mtproto.Update{
		Peer_PEER: peer,
		Settings:  settings,
	}).To_Update(), users, chats)
}

// EmitContactSettings journals updatePeerSettings of each of ids, contacts
// of the box owner the transaction added, changed or removed, with the
// settings stored, and pushes each to every device of the account: the
// devices learn about contacts from these (ADR 0015). users gives the
// contact as the account sees it after the change.
func (d *Dao) EmitContactSettings(b *userbox.Box, ids []int64, users func(id int64) []*mtproto.User) error {
	settings, err := PeerSettingsOf(b, mtproto.PEER_USER, ids)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = d.EmitPeerSettings(b, mtproto.PEER_USER, id, settings[id], users(id), nil); err != nil {
			return err
		}
	}
	return nil
}

// SetUserPeerSettings stores id's settings of a peer, shown again if they
// were hidden, in one box transaction of id with updatePeerSettings for
// every device of the account. Settings that are already the stored ones
// change nothing. users and chats are the peer as id sees it, for the
// push.
func (d *Dao) SetUserPeerSettings(ctx context.Context, id int64, peerType int32, peerId int64, settings *mtproto.PeerSettings, users []*mtproto.User, chats []*mtproto.Chat) error {
	do := &dataobject.UserPeerSettingsDO{
		UserId:                id,
		PeerType:              peerType,
		PeerId:                peerId,
		Hide:                  false,
		ReportSpam:            settings.GetReportSpam(),
		AddContact:            settings.GetAddContact(),
		BlockContact:          settings.GetBlockContact(),
		ShareContact:          settings.GetShareContact(),
		NeedContactsException: settings.GetNeedContactsException(),
		ReportGeo:             settings.GetReportGeo(),
		Autoarchived:          settings.GetAutoarchived(),
		GeoDistance:           settings.GetGeoDistance().GetValue(),
	}
	_, err := d.RunBox(ctx, id, []string{genUserPeerSettingsCacheKey(id, peerType, peerId)}, func(b *userbox.Box) error {
		rows, err := peerSettingsRows(b, peerType, []int64{peerId})
		if err != nil {
			return err
		}
		if old, ok := rows[peerId]; ok && !old.Hide && proto.Equal(makePeerSettings(&old), makePeerSettings(do)) {
			return nil
		}
		if _, _, err = d.UserPeerSettingsDAO.InsertOrUpdateTx(b.Tx(), do); err != nil {
			return err
		}
		return d.EmitPeerSettings(b, peerType, peerId, makePeerSettings(do), users, chats)
	})
	return err
}

// DeleteUserPeerSettings hides id's settings of a peer (the action bar of
// the chat, messages.hidePeerSettingsBar) in one box transaction of id
// with updatePeerSettings, without a bar, for every device of the account.
// Settings hidden or never stored change nothing. users and chats are the
// peer as id sees it, for the push.
func (d *Dao) DeleteUserPeerSettings(ctx context.Context, id int64, peerType int32, peerId int64, users []*mtproto.User, chats []*mtproto.Chat) error {
	_, err := d.RunBox(ctx, id, []string{genUserPeerSettingsCacheKey(id, peerType, peerId)}, func(b *userbox.Box) error {
		rows, err := peerSettingsRows(b, peerType, []int64{peerId})
		if err != nil {
			return err
		}
		if old, ok := rows[peerId]; !ok || old.Hide {
			return nil
		}
		if _, err = d.UserPeerSettingsDAO.DeleteTx(b.Tx(), id, peerType, peerId); err != nil {
			return err
		}
		return d.EmitPeerSettings(b, peerType, peerId, makePeerSettings(nil), users, chats)
	})
	return err
}
