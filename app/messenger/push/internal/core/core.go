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

// Package core decides who gets which push (task 009, ADR 0017): the
// devices of the recipient, except the authorization that acted and those
// with a live session, as the recipient's notification settings allow.
package core

import (
	"context"
	"sync"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/push/internal/notify"
	"github.com/teamgram/teamgram-server/pkg/pushcrypt"
	"github.com/teamgram/teamgram-server/pkg/pushdevices"
	"github.com/teamgram/teamgram-server/pkg/pushprovider"

	"github.com/zeromicro/go-zero/core/logx"
)

// Online tells which authorizations of a user have a live session.
type Online interface {
	OnlineAuthorizations(ctx context.Context, userId int64) (map[int64]bool, error)
}

// Authorizations tells whose an auth key is and when it was last active.
type Authorizations interface {
	// UserOf returns the user of a permanent key, 0 if none.
	UserOf(ctx context.Context, authKeyId int64) (int64, error)
	// LastActive returns when the authorization was last active.
	LastActive(ctx context.Context, authKeyId int64) (time.Time, error)
}

// Settings are the notification settings of a user.
type Settings interface {
	// NotifySettings of userId for a peer (PEER_USER, PEER_CHAT) or a peer
	// type (PEER_USERS, PEER_CHATS, peerId 0).
	NotifySettings(ctx context.Context, userId int64, peerType int32, peerId int64) (*mtproto.PeerNotifySettings, error)
}

// Messages reads a message of a user's box.
type Messages interface {
	UserMessage(ctx context.Context, userId int64, id int32) (*mtproto.Message, error)
}

// APNs and FCM send pushes (pushprovider.APNs, pushprovider.FCM).
type APNs interface {
	Send(ctx context.Context, p *pushprovider.APNsPush) (pushprovider.Outcome, error)
}

type FCM interface {
	Send(ctx context.Context, token string, data map[string]string, validateOnly bool) (pushprovider.Outcome, error)
}

// Deps are what the core works with.
type Deps struct {
	Devices        pushdevices.Store
	Online         Online
	Authorizations Authorizations
	Settings       Settings
	Messages       Messages
	APNs           APNs
	FCM            FCM
}

// Timing.
const (
	// EventDeadline bounds the pushes of one event: a late push is worth
	// less than the next one.
	EventDeadline = 10 * time.Second
	// MutedEvery: one MESSAGE_MUTED per device and dialog at most this
	// often (Telegram: about every 15 seconds in private chats).
	MutedEvery = 15 * time.Second
	// Push expiration at the provider for an offline device.
	alertExpiration      = 24 * time.Hour
	backgroundExpiration = time.Hour
)

// Core sends the pushes of events.
type Core struct {
	Deps
	now   func() time.Time
	muted *limiter
}

// New returns the core on deps.
func New(deps Deps) *Core {
	return &Core{Deps: deps, now: time.Now, muted: newLimiter(MutedEvery)}
}

// Event is a push of Sync-T for one user.
type Event struct {
	UserId int64
	// Except is the authorization that made the change (sync.updatesNotMe):
	// it knows.
	Except int64
	// Only is the one authorization the updates are for (sync.updatesMe).
	Only    int64
	Updates *mtproto.Updates
}

// Handle sends the pushes of ev; failures are logged and counted: the
// devices catch up with getDifference.
func (c *Core) Handle(ctx context.Context, ev *Event) {
	ctx, cancel := context.WithTimeout(ctx, EventDeadline)
	defer cancel()
	if ev.Only != 0 {
		if isReset(ev.Updates) {
			c.revoke(ctx, ev.UserId, ev.Only)
		}
		return
	}
	ns := notify.FromUpdates(ev.UserId, ev.Updates)
	if len(ns) == 0 {
		return
	}
	targets := c.targets(ctx, ev)
	if len(targets) == 0 {
		return
	}
	var wg sync.WaitGroup
	for _, n := range ns {
		c.prepare(ctx, n)
		for _, d := range targets {
			wg.Add(1)
			go func(n *notify.Notification, d *pushdevices.Device) {
				defer wg.Done()
				c.notify(ctx, n, d)
			}(n, d)
		}
	}
	wg.Wait()
}

// isReset tells whether updates terminate the authorization.
func isReset(ups *mtproto.Updates) bool {
	return ups.GetPredicateName() == mtproto.Predicate_updateAccountResetAuthorization
}

// targets returns the devices of ev.UserId that get pushes: not the
// authorization that acted, not one online, still the user's.
func (c *Core) targets(ctx context.Context, ev *Event) []*pushdevices.Device {
	log := logx.WithContext(ctx)
	devices, err := c.Devices.List(ctx, ev.UserId)
	if err != nil {
		log.Errorf("push: devices of user %d: %v", ev.UserId, err)
		metricDropped.Inc("error")
		return nil
	}
	if len(devices) == 0 {
		return nil
	}
	online, err := c.Online.OnlineAuthorizations(ctx, ev.UserId)
	if err != nil {
		// Better a push to a device that has the update than none.
		log.Errorf("push: online sessions of user %d: %v", ev.UserId, err)
	}
	owners := map[int64]int64{}
	var out []*pushdevices.Device
	for _, d := range devices {
		switch {
		case d.TokenType != pushdevices.TokenAPNs && d.TokenType != pushdevices.TokenFCM:
			continue // VoIP is for calls, web push is not sent yet
		case d.AuthKeyId == ev.Except:
			metricDropped.Inc("initiator")
			continue
		case online[d.AuthKeyId]:
			metricDropped.Inc("online")
			continue
		}
		owner, ok := owners[d.AuthKeyId]
		if !ok {
			if owner, err = c.Authorizations.UserOf(ctx, d.AuthKeyId); err != nil {
				log.Errorf("push: user of key %d: %v", d.AuthKeyId, err)
				owner = ev.UserId // unknown: send
			}
			owners[d.AuthKeyId] = owner
		}
		if owner != ev.UserId {
			// Logged out on a path that did not remove the device.
			log.Infof("push: user %d: the authorization of device %d is gone, device removed", ev.UserId, d.Id)
			if err = c.Devices.Delete(ctx, d.Id); err != nil {
				log.Errorf("push: remove device %d: %v", d.Id, err)
			}
			metricDropped.Inc("unbound")
			continue
		}
		out = append(out, d)
	}
	return out
}

// prepare completes n before it is sent: the settings of the dialog, the
// pinned message.
func (c *Core) prepare(ctx context.Context, n *notify.Notification) {
	if n.NeedsPinned() {
		if m, err := c.Messages.UserMessage(ctx, n.UserId, n.PinnedId); err != nil {
			logx.WithContext(ctx).Errorf("push: pinned message %d of user %d: %v", n.PinnedId, n.UserId, err)
		} else if m != nil {
			n.SetPinned(m)
		}
	}
}

// notify sends n to d, as the settings of the recipient allow.
func (c *Core) notify(ctx context.Context, n *notify.Notification, d *pushdevices.Device) {
	if n.Kind == notify.KindReadHistory {
		c.deliver(ctx, n, d, false)
		return
	}
	s := c.settings(ctx, n)
	if s.muted && !n.Mention {
		if d.NoMuted || !c.muted.allow(d.Id, n.PeerId, n.Chat) {
			metricDropped.Inc("muted")
			return
		}
		c.deliver(ctx, &notify.Notification{Kind: notify.KindMuted, UserId: n.UserId, Chat: n.Chat, PeerId: n.PeerId, MsgId: n.MsgId}, d, false)
		return
	}
	if s.silent && !n.Silent {
		cp := *n
		cp.Silent = true
		n = &cp
	}
	c.deliver(ctx, n, d, !s.previews)
}

type dialogSettings struct {
	muted, silent, previews bool
}

// settings of the dialog of n: the dialog's own, else those of its type
// (private chats, groups), as Telegram combines them.
func (c *Core) settings(ctx context.Context, n *notify.Notification) dialogSettings {
	peerType, typeOf := int32(mtproto.PEER_USER), int32(mtproto.PEER_USERS)
	if n.Chat {
		peerType, typeOf = mtproto.PEER_CHAT, mtproto.PEER_CHATS
	}
	s := dialogSettings{previews: true}
	var own, def *mtproto.PeerNotifySettings
	var err error
	if own, err = c.Settings.NotifySettings(ctx, n.UserId, peerType, n.PeerId); err != nil {
		logx.WithContext(ctx).Errorf("push: notify settings of user %d: %v", n.UserId, err)
	}
	if def, err = c.Settings.NotifySettings(ctx, n.UserId, typeOf, 0); err != nil {
		logx.WithContext(ctx).Errorf("push: notify settings of user %d: %v", n.UserId, err)
	}
	now := int32(c.now().Unix())
	switch {
	case own.GetMuteUntil() != nil:
		s.muted = own.GetMuteUntil().GetValue() > now
	case def.GetMuteUntil() != nil:
		s.muted = def.GetMuteUntil().GetValue() > now
	}
	switch {
	case own.GetShowPreviews() != nil:
		s.previews = mtproto.FromBool(own.GetShowPreviews())
	case def.GetShowPreviews() != nil:
		s.previews = mtproto.FromBool(def.GetShowPreviews())
	}
	switch {
	case own.GetSilent() != nil:
		s.silent = mtproto.FromBool(own.GetSilent())
	case def.GetSilent() != nil:
		s.silent = mtproto.FromBool(def.GetSilent())
	}
	return s
}

// revoke tells the devices of a terminated authorization to log out
// (SESSION_REVOKE) and forgets them.
func (c *Core) revoke(ctx context.Context, userId, authKeyId int64) {
	log := logx.WithContext(ctx)
	devices, err := c.Devices.List(ctx, userId)
	if err != nil {
		log.Errorf("push: devices of user %d: %v", userId, err)
	}
	var wg sync.WaitGroup
	for _, d := range devices {
		if d.AuthKeyId != authKeyId || d.TokenType != pushdevices.TokenAPNs && d.TokenType != pushdevices.TokenFCM {
			continue
		}
		wg.Add(1)
		go func(d *pushdevices.Device) {
			defer wg.Done()
			c.deliver(ctx, &notify.Notification{Kind: notify.KindSessionRevoke, UserId: userId}, d, false)
		}(d)
	}
	wg.Wait()
	if _, err = c.Devices.DeleteAuthorizations(ctx, userId, authKeyId); err != nil {
		log.Errorf("push: remove the devices of user %d, key %d: %v", userId, authKeyId, err)
	}
}

// deliver encrypts n for d and sends it.
func (c *Core) deliver(ctx context.Context, n *notify.Notification, d *pushdevices.Device, hideText bool) {
	log := logx.WithContext(ctx)
	var (
		name    string
		outcome pushprovider.Outcome
		err     error
	)
	switch d.TokenType {
	case pushdevices.TokenFCM:
		name = "fcm"
		outcome, err = c.sendFCM(ctx, n, d, hideText)
	case pushdevices.TokenAPNs:
		name = "apns"
		outcome, err = c.sendAPNs(ctx, n, d, hideText)
	default:
		return
	}
	if outcome == "" {
		return // nothing for this device
	}
	metricSent.Inc(name, string(outcome))
	switch outcome {
	case pushprovider.OutcomeSent, pushprovider.OutcomeDisabled:
	case pushprovider.OutcomeTokenInvalid:
		log.Infof("push: %s device %d of user %d: the token is invalid (%v)", name, d.Id, d.UserId, err)
		if d.TokenType == pushdevices.TokenAPNs {
			// The next registration of the token gets TOKEN_WAS_INVALIDATED.
			err = c.Devices.Invalidate(ctx, d.Id)
		} else {
			err = c.Devices.Delete(ctx, d.Id)
		}
		if err != nil {
			log.Errorf("push: forget device %d: %v", d.Id, err)
		}
	default:
		log.Errorf("push: %s device %d of user %d: %s: %v", name, d.Id, d.UserId, string(outcome), err)
	}
}

func (c *Core) sendFCM(ctx context.Context, n *notify.Notification, d *pushdevices.Device, hideText bool) (pushprovider.Outcome, error) {
	payload, err := notify.Android(n, hideText)
	if err != nil {
		return pushprovider.OutcomeRejected, err
	}
	p, err := pushcrypt.EncodeP(d.Secret, payload)
	if err != nil {
		return pushprovider.OutcomeRejected, err
	}
	return c.FCM.Send(ctx, d.Token, map[string]string{"p": p}, false)
}

func (c *Core) sendAPNs(ctx context.Context, n *notify.Notification, d *pushdevices.Device, hideText bool) (pushprovider.Outcome, error) {
	hide := notify.HideNone
	if hideText {
		hide = notify.HideText
	}
	if d.LockedPeriod > 0 && c.lockedOut(ctx, d) {
		hide = notify.HideAll
	}
	push, err := notify.IOS(n, d.LangCode, hide)
	if err != nil {
		return pushprovider.OutcomeRejected, err
	}
	if push == nil {
		return "", nil
	}
	p, err := pushcrypt.EncodeP(d.Secret, push.Payload)
	if err != nil {
		return pushprovider.OutcomeRejected, err
	}
	body, req := apnsBody(push, p), &pushprovider.APNsPush{Token: d.Token, Sandbox: d.AppSandbox}
	if push.Alert {
		req.Type, req.Expiration = pushprovider.APNsAlert, c.now().Add(alertExpiration)
	} else {
		req.Type, req.Expiration = pushprovider.APNsBackground, c.now().Add(backgroundExpiration)
	}
	req.Body = body
	return c.APNs.Send(ctx, req)
}

// lockedOut tells whether the authorization of d has been inactive longer
// than its locked period (account.updateDeviceLocked); unknown is locked.
func (c *Core) lockedOut(ctx context.Context, d *pushdevices.Device) bool {
	last, err := c.Authorizations.LastActive(ctx, d.AuthKeyId)
	if err != nil {
		logx.WithContext(ctx).Errorf("push: last activity of key %d: %v", d.AuthKeyId, err)
		return true
	}
	return c.now().Sub(last) > time.Duration(d.LockedPeriod)*time.Second
}
