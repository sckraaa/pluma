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
	"github.com/teamgram/teamgram-server/pkg/channelstats"
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
//
// A change of the muting in effect of a channel is recorded for its
// statistics in the same transaction (muteChanged).
func (d *Dao) SetUserPeerNotifySettings(ctx context.Context, id int64, peerType int32, peerId int64, settings *mtproto.PeerNotifySettings, users []*mtproto.User, chats []*mtproto.Chat) error {
	_, err := d.RunBox(ctx, id, []string{genUserNotifySettingsCacheKey(id, peerType, peerId)}, func(b *userbox.Box) error {
		before := int32(noMuteUntil)
		if peerType == mtproto.PEER_CHANNEL {
			var err error
			if before, err = storedMuteUntil(b.Tx(), id, peerType, peerId); err != nil {
				return err
			}
		}
		if _, _, err := d.UserNotifySettingsDAO.InsertOrUpdateExtTx(b.Tx(), id, peerType, peerId, makeDOByPeerNotifySettings(settings)); err != nil {
			return err
		}
		if peerType == mtproto.PEER_CHANNEL {
			after := int32(noMuteUntil)
			if settings.GetMuteUntil() != nil {
				after = settings.GetMuteUntil().GetValue()
			}
			if err := muteChanged(b, id, map[int64][2]int32{peerId: {before, after}}); err != nil {
				return err
			}
		}
		return d.EmitSeqUpdate(b, notifySettingsUpdate(peerType, peerId, settings), users, chats)
	})
	return err
}

// noMuteUntil is mute_until of settings that set none: the peer follows its
// class, and a class nobody muted is not muted (GetUserNotifySettings).
const noMuteUntil = -1

// storedMuteUntil is id's mute_until of the peer as stored, noMuteUntil
// when it has no settings.
func storedMuteUntil(tx *sqlx.Tx, id int64, peerType int32, peerId int64) (int32, error) {
	var muteUntil []int32
	if err := tx.QueryRowsPartial(&muteUntil,
		"SELECT mute_until FROM user_notify_settings WHERE user_id = ? AND peer_type = ? AND peer_id = ? AND deleted = 0", id, peerType, peerId); err != nil {
		return 0, err
	}
	if len(muteUntil) == 0 {
		return noMuteUntil, nil
	}
	return muteUntil[0], nil
}

// muteInEffect is what the mute_until of a peer comes to at now, with the
// mute_until of its class for a peer that has none: 0 when its
// notifications are on, the time it is muted until otherwise. A mute_until
// that passed mutes nothing.
func muteInEffect(peer, class int32, now int64) int32 {
	m := peer
	if m == noMuteUntil {
		m = class
	}
	if int64(m) <= now {
		return 0
	}
	return m
}

// muteChanged records, for the statistics of the channels (task 103,
// pkg/channelstats MuteChanged), the change of id's muting of each channel
// of changes from its mute_until before to the one after ([2]int32), when
// the muting in effect changed: muted, unmuted, or muted until another
// time; a muting that has run out is unmuted already. A channel without a
// mute_until of its own follows id's settings of all channels, or of all
// groups for a supergroup (tdesktop data_notify_settings.cpp:70-76). A
// change of those settings is no change of any one channel and is not
// recorded. A channel that is gone records nothing.
func muteChanged(b *userbox.Box, id int64, changes map[int64][2]int32) error {
	ids := make([]int64, 0, len(changes))
	for channelId := range changes {
		ids = append(ids, channelId)
	}
	slices.Sort(ids)
	var channels []struct {
		Id        int64 `db:"id"`
		Broadcast bool  `db:"broadcast"`
	}
	if err := b.Tx().QueryRowsPartial(&channels,
		"SELECT id, broadcast FROM channels WHERE deleted = 0 AND id IN ("+placeholders(len(ids))+")", int64Args(ids)...); err != nil {
		return err
	}
	classes := map[int32]int32{}
	for _, class := range []int32{mtproto.PEER_BROADCASTS, mtproto.PEER_CHATS} {
		m, err := storedMuteUntil(b.Tx(), id, class, 0)
		if err != nil {
			return err
		}
		classes[class] = m
	}
	for _, ch := range channels {
		class := classes[mtproto.PEER_CHATS]
		if ch.Broadcast {
			class = classes[mtproto.PEER_BROADCASTS]
		}
		c := changes[ch.Id]
		before, after := muteInEffect(c[0], class, b.Date()), muteInEffect(c[1], class, b.Date())
		if before == after {
			continue
		}
		if err := channelstats.MuteChanged(b.Tx(), ch.Id, id, b.Date(), after); err != nil {
			return err
		}
	}
	return nil
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
			"SELECT peer_type, peer_id, mute_until FROM user_notify_settings WHERE user_id = ? AND deleted = 0 FOR UPDATE", id); err != nil {
			return err
		}
		// Every channel comes to be unmuted, as the classes do: what was in
		// effect is read while the settings are there.
		unmuted := map[int64][2]int32{}
		for _, r := range rows {
			if r.PeerType == mtproto.PEER_CHANNEL {
				unmuted[r.PeerId] = [2]int32{r.MuteUntil, 0}
			}
		}
		if len(unmuted) > 0 {
			if err := muteChanged(b, id, unmuted); err != nil {
				return err
			}
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
		return d.resetTopicNotifySettings(b, id)
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
