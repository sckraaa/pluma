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
	"slices"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	userNotifySettingsKeyPrefix = "user_notify_settings"
)

func genUserNotifySettingsCacheKey(id int64, peerType int32, peerId int64) string {
	return fmt.Sprintf("%s_%d_%d_%d", userNotifySettingsKeyPrefix, id, peerType, peerId)
}

func (d *Dao) GetUserNotifySettings(ctx context.Context, id int64, peerType int32, peerId int64) (*mtproto.PeerNotifySettings, error) {
	settings := mtproto.MakeTLPeerNotifySettings(nil).To_PeerNotifySettings()

	err := d.CachedConn.QueryRow(
		ctx,
		settings,
		genUserNotifySettingsCacheKey(id, peerType, peerId),
		func(ctx context.Context, conn *sqlx.DB, v interface{}) error {
			do, err := d.UserNotifySettingsDAO.Select(ctx, id, peerType, peerId)
			if err != nil {
				// c.Logger.Errorf("user.getNotifySettings - error: %v", err)
				return err
			}

			var (
				notifySettings = v.(*mtproto.PeerNotifySettings)
			)

			if do == nil {
				switch peerType {
				case mtproto.PEER_USERS,
					mtproto.PEER_CHATS,
					mtproto.PEER_BROADCASTS:

					notifySettings.ShowPreviews = mtproto.BoolTrue
					notifySettings.Silent = mtproto.BoolFalse
					notifySettings.MuteUntil = &wrapperspb.Int32Value{Value: 0}
					notifySettings.Sound = &wrapperspb.StringValue{Value: "default"}
				}
			} else {
				setPeerNotifySettingsByDO(notifySettings, do)
			}

			return nil
		})
	if err != nil {
		return nil, err
	}

	return settings, nil
}

func setPeerNotifySettingsByDO(settings *mtproto.PeerNotifySettings, do *dataobject.UserNotifySettingsDO) {
	if do.ShowPreviews != -1 {
		settings.ShowPreviews = mtproto.ToBool(do.ShowPreviews == 1)
	}
	if do.Silent != -1 {
		settings.Silent = mtproto.ToBool(do.Silent == 1)
	}
	if do.MuteUntil != -1 {
		settings.MuteUntil = &wrapperspb.Int32Value{Value: do.MuteUntil}
	}
	if do.Sound != "-1" {
		settings.Sound = &wrapperspb.StringValue{Value: do.Sound}
	}
}

func makeDOByPeerNotifySettings(settings *mtproto.PeerNotifySettings) (doMap map[string]interface{}) {
	doMap = map[string]interface{}{}

	if settings.ShowPreviews != nil {
		if mtproto.FromBool(settings.ShowPreviews) {
			doMap["show_previews"] = 1
		} else {
			doMap["show_previews"] = 0
		}
	} else {
		doMap["show_previews"] = -1
	}

	if settings.Silent != nil {
		if mtproto.FromBool(settings.Silent) {
			doMap["silent"] = 1
		} else {
			doMap["silent"] = 0
		}
	} else {
		doMap["silent"] = -1
	}

	if settings.MuteUntil != nil {
		doMap["mute_until"] = settings.MuteUntil.Value
	} else {
		doMap["mute_until"] = -1
	}

	if settings.Sound != nil {
		doMap["sound"] = settings.Sound.Value
	} else {
		doMap["sound"] = "-1"
	}

	return
}

// SetUserPeerNotifySettings stores id's notify settings of a peer (or of a
// class: PEER_USERS, PEER_CHATS, PEER_BROADCASTS) in one box transaction of
// id, with updateNotifySettings for every device of the account (ADR 0020).
// users and chats are the peer as id sees it, for the push.
func (d *Dao) SetUserPeerNotifySettings(ctx context.Context, id int64, peerType int32, peerId int64, settings *mtproto.PeerNotifySettings, users []*mtproto.User, chats []*mtproto.Chat) error {
	_, err := d.RunBox(ctx, id, []string{genUserNotifySettingsCacheKey(id, peerType, peerId)}, func(b *userbox.Box) error {
		if _, _, err := d.UserNotifySettingsDAO.InsertOrUpdateExtTx(b.Tx(), id, peerType, peerId, makeDOByPeerNotifySettings(settings)); err != nil {
			return err
		}
		return d.EmitSeqUpdate(b, notifySettingsUpdate(peerType, peerId, settings), users, chats)
	})
	return err
}

// ResetNotifySettings removes all of id's notify settings in one box
// transaction of id. Each device gets updateNotifySettings with the default
// for every peer that had settings and for the three classes: a device that
// missed the push finds them in the difference, peer by peer, where the
// settings of each peer are folded to the last.
func (d *Dao) ResetNotifySettings(ctx context.Context, id int64) error {
	classes := []int32{mtproto.PEER_USERS, mtproto.PEER_CHATS, mtproto.PEER_BROADCASTS}
	keys := make([]string, 0, len(classes))
	for _, t := range classes {
		keys = append(keys, genUserNotifySettingsCacheKey(id, t, 0))
	}
	_, err := d.RunBox(ctx, id, keys, func(b *userbox.Box) error {
		var rows []dataobject.UserNotifySettingsDO
		if err := b.Tx().QueryRowsPartial(&rows,
			"SELECT peer_type, peer_id FROM user_notify_settings WHERE user_id = ? AND deleted = 0 FOR UPDATE", id); err != nil {
			return err
		}
		if _, err := d.UserNotifySettingsDAO.DeleteAllTx(b.Tx(), id); err != nil {
			return err
		}
		for _, r := range rows {
			if slices.Contains(classes, r.PeerType) {
				continue
			}
			b.InvalidateCache(genUserNotifySettingsCacheKey(id, r.PeerType, r.PeerId))
			// No settings: the peer follows its class.
			u := notifySettingsUpdate(r.PeerType, r.PeerId, mtproto.MakeTLPeerNotifySettings(nil).To_PeerNotifySettings())
			if err := d.EmitSeqUpdate(b, u, nil, nil); err != nil {
				return err
			}
		}
		for _, t := range classes {
			if err := d.EmitSeqUpdate(b, notifySettingsUpdate(t, 0, defaultNotifySettings()), nil, nil); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

// defaultNotifySettings are the settings of a class nobody set: what
// GetUserNotifySettings gives for it.
func defaultNotifySettings() *mtproto.PeerNotifySettings {
	return mtproto.MakeTLPeerNotifySettings(&mtproto.PeerNotifySettings{
		ShowPreviews: mtproto.BoolTrue,
		Silent:       mtproto.BoolFalse,
		MuteUntil:    &wrapperspb.Int32Value{Value: 0},
		Sound:        &wrapperspb.StringValue{Value: "default"},
	}).To_PeerNotifySettings()
}

func notifySettingsUpdate(peerType int32, peerId int64, settings *mtproto.PeerNotifySettings) *mtproto.Update {
	peer := &mtproto.PeerUtil{PeerType: peerType, PeerId: peerId}
	return mtproto.MakeTLUpdateNotifySettings(&mtproto.Update{
		Peer_NOTIFYPEER: peer.ToNotifyPeer(),
		NotifySettings:  settings,
	}).To_Update()
}
