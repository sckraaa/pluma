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

// Package effects carries out what a decision does outside the database,
// through the internal RPCs of the services that own the data (task 025,
// ADR 0040). Each step is done and recorded before the next one, so that a
// restart or a failed service continues where it stopped.
package effects

import (
	"context"
	"fmt"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/authorization/servicemsg"
	"github.com/teamgram/teamgram-server/app/bff/authorization/servicetext"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/moderation"
	"github.com/teamgram/teamgram-server/pkg/stories"

	"github.com/zeromicro/go-zero/core/logx"
)

// Msg is what the steps need of the msg service.
type Msg interface {
	servicemsg.Msg
	MsgDeleteMessages(ctx context.Context, in *msgpb.TLMsgDeleteMessages) (*mtproto.Messages_AffectedMessages, error)
}

// Channel is what the steps need of the channel service.
type Channel interface {
	ChannelDeleteMessages(ctx context.Context, in *channel.TLChannelDeleteMessages) (*mtproto.Messages_AffectedMessages, error)
}

// Authsession is what the steps need of the authsession service.
type Authsession interface {
	AuthsessionResetAuthorization(ctx context.Context, in *authsession.TLAuthsessionResetAuthorization) (*authsession.Vector_Long, error)
	AuthsessionGetRecentLangCode(ctx context.Context, in *authsession.TLAuthsessionGetRecentLangCode) (*mtproto.String, error)
}

// Sync pushes updates to the sessions of a user.
type Sync interface {
	SyncUpdatesMe(ctx context.Context, in *sync.TLSyncUpdatesMe) (*mtproto.Void, error)
}

// Stories is what the steps need of the stories' tables (pkg/stories.Store).
type Stories interface {
	Delete(ctx context.Context, peer stories.Peer, ids []int32, at int32) ([]*stories.Story, error)
}

// Runner carries out the steps of decisions.
type Runner struct {
	DAO         *moderation.DAO
	Msg         Msg
	Channel     Channel
	Authsession Authsession
	Sync        Sync
	// Stories deletes reported stories (moderation.KindStory).
	Stories Stories
}

// Run does the steps of the report's decision that are left. It returns
// nil also when another runner holds them.
func (r *Runner) Run(ctx context.Context, reportId int64) error {
	claimed, err := r.DAO.ClaimEffects(ctx, reportId)
	if err != nil || !claimed {
		return err
	}
	report, err := r.DAO.Report(ctx, reportId)
	if err != nil {
		return r.release(ctx, reportId, err)
	}
	if report == nil {
		return fmt.Errorf("report %d is gone", reportId)
	}
	steps, err := moderation.StepsOf(report.Decision)
	if err != nil {
		return r.release(ctx, reportId, err)
	}
	if int(report.EffectsTotal) > len(steps) {
		return r.release(ctx, reportId, fmt.Errorf("decision %q has %d steps", report.Decision, report.EffectsTotal))
	}
	snapshot, err := report.Snapshot()
	if err != nil {
		return r.release(ctx, reportId, err)
	}
	for stage := report.EffectsStage; stage < report.EffectsTotal; stage++ {
		if err := r.step(ctx, report, snapshot, steps[stage]); err != nil {
			return r.release(ctx, reportId, fmt.Errorf("step %s: %w", steps[stage], err))
		}
		if err := r.DAO.AdvanceEffects(ctx, reportId, stage); err != nil {
			return r.release(ctx, reportId, err)
		}
	}
	return r.DAO.ReleaseEffects(ctx, reportId, "")
}

// release records a failure of a run and returns it.
func (r *Runner) release(ctx context.Context, reportId int64, cause error) error {
	if err := r.DAO.ReleaseEffects(ctx, reportId, cause.Error()); err != nil {
		logx.WithContext(ctx).Errorf("moderation effects: report %d, release: %v", reportId, err)
	}
	return cause
}

func (r *Runner) step(ctx context.Context, report *moderation.Report, snapshot *moderation.Snapshot, step string) error {
	switch step {
	case moderation.StepDelete:
		return r.deleteMessages(ctx, report, snapshot)
	case moderation.StepNotify:
		kind := moderation.RestrictLimited
		if report.Decision == moderation.ActionBan {
			kind = moderation.RestrictBanned
		}
		return r.Notify(ctx, report.TargetUserId, kind, report.DecisionUntil)
	case moderation.StepRevoke:
		return r.Revoke(ctx, report.TargetUserId)
	}
	return fmt.Errorf("unknown step %q", step)
}

// deleteMessages deletes the reported messages for everyone. In a private
// chat or basic group the ids are those of the reporter's own box, which
// is where the report found them. In a channel the channel's creator does
// it, the one member whose rights always allow it (pkg/channelperm).
func (r *Runner) deleteMessages(ctx context.Context, report *moderation.Report, snapshot *moderation.Snapshot) error {
	// The ids of a report of stories are stories of the peer, never
	// messages.
	if report.Kind == moderation.KindStory {
		return r.deleteStories(ctx, report, snapshot)
	}
	ids := idsToDelete(report, snapshot)
	if len(ids) == 0 {
		return nil
	}
	if report.PeerType == mtproto.PEER_CHANNEL {
		creator, err := r.DAO.ChannelCreator(ctx, report.PeerId)
		if err != nil {
			return err
		}
		if creator == 0 {
			return fmt.Errorf("channel %d has no creator", report.PeerId)
		}
		_, err = r.Channel.ChannelDeleteMessages(ctx, &channel.TLChannelDeleteMessages{
			SelfId:    creator,
			ChannelId: report.PeerId,
			Id:        ids,
		})
		return err
	}
	_, err := r.Msg.MsgDeleteMessages(ctx, &msgpb.TLMsgDeleteMessages{
		UserId:   report.ReporterUserId,
		PeerType: mtproto.PEER_EMPTY,
		Revoke:   true,
		Id:       ids,
	})
	return err
}

// deleteStories deletes the reported stories of the report's peer. The
// stories service learns of it by the mark the deletion leaves
// (stories.Store.Dirty) and tells the service of users; the feeds of the
// clients drop the stories at their next read.
func (r *Runner) deleteStories(ctx context.Context, report *moderation.Report, snapshot *moderation.Snapshot) error {
	if r.Stories == nil {
		return fmt.Errorf("report %d: no stories to delete from", report.Id)
	}
	ids := make([]int32, 0, len(snapshot.Messages))
	for _, m := range snapshot.Messages {
		ids = append(ids, m.Id)
	}
	if len(ids) == 0 {
		return nil
	}
	_, err := r.Stories.Delete(ctx, stories.Peer{Type: report.PeerType, Id: report.PeerId}, ids, int32(time.Now().Unix()))
	return err
}

// idsToDelete is what a deletion takes from the snapshot. The reporter's own
// messages stay: a report accuses others, and a spam report keeps the last
// messages of both sides only as the evidence.
func idsToDelete(report *moderation.Report, snapshot *moderation.Snapshot) []int32 {
	ids := make([]int32, 0, len(snapshot.Messages))
	for _, m := range snapshot.Messages {
		if m.FromId != report.ReporterUserId {
			ids = append(ids, m.Id)
		}
	}
	return ids
}

// Notify sends the user the message about the restriction from the service
// notifications account (ADR 0026).
func (r *Runner) Notify(ctx context.Context, userId int64, kind int8, until int32) error {
	recent, err := r.Authsession.AuthsessionGetRecentLangCode(ctx, &authsession.TLAuthsessionGetRecentLangCode{UserId: userId})
	if err != nil {
		return fmt.Errorf("language of user %d: %w", userId, err)
	}
	var (
		lang = servicetext.LangOf(recent.GetV())
		end  time.Time
	)
	if until != 0 {
		end = time.Unix(int64(until), 0)
	}
	text, entities := servicetext.Restricted(lang, kind == moderation.RestrictBanned, end)
	return servicemsg.Send(ctx, r.Msg, userId, time.Now(), text, entities)
}

// Revoke ends every session of the user the way account.resetAuthorization
// does: the keys are unbound, and the sessions hear of it at once.
func (r *Runner) Revoke(ctx context.Context, userId int64) error {
	keys, err := r.Authsession.AuthsessionResetAuthorization(ctx, &authsession.TLAuthsessionResetAuthorization{UserId: userId})
	if err != nil {
		return err
	}
	for _, id := range keys.Datas {
		if _, err := r.Sync.SyncUpdatesMe(ctx, &sync.TLSyncUpdatesMe{
			UserId:        userId,
			PermAuthKeyId: id,
			Updates:       mtproto.MakeTLUpdatesTooLong(nil).To_Updates(),
		}); err != nil {
			return err
		}
		if _, err := r.Sync.SyncUpdatesMe(ctx, &sync.TLSyncUpdatesMe{
			UserId:        userId,
			PermAuthKeyId: id,
			Updates: mtproto.MakeTLUpdateAccountResetAuthorization(&mtproto.Updates{
				UserId:    userId,
				AuthKeyId: id,
			}).To_Updates(),
		}); err != nil {
			return err
		}
	}
	return nil
}

// ApplyDirect restricts or bans a user without a report and carries out
// the steps at once: the emergency access of pluma-admin.
func (r *Runner) ApplyDirect(ctx context.Context, userId int64, ban bool, until int32, reason, actor string) error {
	kind := moderation.RestrictLimited
	if ban {
		kind = moderation.RestrictBanned
	}
	changed, err := r.DAO.ApplyDirect(ctx, userId, kind, until, reason, actor)
	if err != nil || !changed {
		return err
	}
	if err := r.Notify(ctx, userId, kind, until); err != nil {
		return err
	}
	if ban {
		return r.Revoke(ctx, userId)
	}
	return nil
}
