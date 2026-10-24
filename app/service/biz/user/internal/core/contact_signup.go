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
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/svc"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/metric"
	"google.golang.org/protobuf/proto"
)

// Delivery of the sign-ups of imported numbers (task 011, ADR 0015): when
// a number someone imported gets an account, each importer gets the new
// user as a contact, with the name of the phone book, and the service
// message messageActionContactSignUp in the chat with them; Telegram marks
// it silent for an importer who turned these notifications off
// (account.setContactSignUpNotification), the clients then do not notify
// (Android NotificationsController, tdlib disable_contact_registered_
// notifications). The sign-up is queued with the account (contact_signups)
// and delivered by one box transaction of the new user, whose events write
// the messages into the importers' boxes and push the new contact.

const (
	// signUpSweep is how often the worker looks for sign-ups a failure or
	// a restart left undelivered.
	signUpSweep = 30 * time.Second
	// signUpBatch is the most sign-ups one sweep delivers.
	signUpBatch = 100
	// signUpRounds bounds the box transactions of one delivery: a round
	// leaves the importers who came during it to the next one.
	signUpRounds = 3
	// signUpTimeout bounds one delivery: the look-ups and the ids (idgen
	// answers within 2 s) before the box operation, the box operation.
	signUpTimeout = 4*time.Second + dao.MaxBoxOpTime
	// SignUpStopWait is how long Stop waits for the delivery in progress:
	// the server's Destroy waits for it (stop_grace_period of biz).
	SignUpStopWait = signUpTimeout
)

var metricSignUpErrors = metric.NewCounterVec(&metric.CounterVecOpts{
	Namespace: "user",
	Name:      "contact_signup_errors_total",
	Help:      "Failed deliveries of the sign-up of an imported number; the worker retries them.",
	Labels:    []string{},
})

// SignUpWorker delivers the sign-ups of imported numbers: right after an
// account is created (Notify) and every signUpSweep. Several workers (biz
// processes) may run: a delivery is one box transaction of the new user,
// which the box lock serializes, and a delivered sign-up is gone.
type SignUpWorker struct {
	svcCtx *svc.ServiceContext
	wake   chan struct{}
	stop   chan struct{}
	done   chan struct{}
	start  sync.Once
	halt   sync.Once
}

// NewSignUpWorker returns the worker of the service; the service context
// notifies it of new accounts.
func NewSignUpWorker(svcCtx *svc.ServiceContext) *SignUpWorker {
	w := &SignUpWorker{
		svcCtx: svcCtx,
		wake:   make(chan struct{}, 1),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	svcCtx.SignUps = w
	return w
}

// Start runs the worker until Stop.
func (w *SignUpWorker) Start() {
	w.start.Do(func() {
		go w.run()
	})
}

// Notify wakes the worker: an account was created.
func (w *SignUpWorker) Notify() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Stop stops the worker: it takes no new sign-up and Stop waits for the
// delivery in progress, at most SignUpStopWait. A delivery cut short by
// the end of the process is rolled back and done again by the next start.
func (w *SignUpWorker) Stop() {
	w.halt.Do(func() {
		close(w.stop)
	})
	w.start.Do(func() {
		close(w.done) // never started
	})
	select {
	case <-w.done:
	case <-time.After(SignUpStopWait):
		logx.Errorf("user: contact sign-up delivery still running after %v", SignUpStopWait)
	}
}

func (w *SignUpWorker) stopping() bool {
	select {
	case <-w.stop:
		return true
	default:
		return false
	}
}

func (w *SignUpWorker) run() {
	defer close(w.done)
	ticker := time.NewTicker(signUpSweep)
	defer ticker.Stop()
	for !w.stopping() {
		w.sweep(context.Background())
		select {
		case <-w.stop:
		case <-w.wake:
		case <-ticker.C:
		}
	}
}

// sweep delivers the pending sign-ups, the oldest first.
func (w *SignUpWorker) sweep(ctx context.Context) {
	pending, err := w.svcCtx.Dao.PendingContactSignUps(ctx, signUpBatch)
	if err != nil {
		metricSignUpErrors.Inc()
		logx.WithContext(ctx).Errorf("user: pending contact sign-ups: %v", err)
		return
	}
	for _, s := range pending {
		if w.stopping() {
			return
		}
		if err := DeliverContactSignUp(ctx, w.svcCtx, s); err != nil {
			metricSignUpErrors.Inc()
			logx.WithContext(ctx).Errorf("user: contact sign-up of user %d: %v", s.UserId, err)
		}
	}
}

// DeliverContactSignUp delivers the sign-up s to the users who imported
// its number.
func DeliverContactSignUp(ctx context.Context, svcCtx *svc.ServiceContext, s dao.ContactSignUp) error {
	ctx, cancel := context.WithTimeout(ctx, signUpTimeout)
	defer cancel()
	for round := 1; ; round++ {
		done, err := deliverSignUp(ctx, svcCtx, s)
		if err != nil || done {
			return err
		}
		if round == signUpRounds {
			return fmt.Errorf("importers still coming after %d rounds", round)
		}
	}
}

// deliverSignUp runs one round of a delivery; done is false when
// importers came during it.
func deliverSignUp(ctx context.Context, svcCtx *svc.ServiceContext, s dao.ContactSignUp) (bool, error) {
	d := svcCtx.Dao
	importers, err := d.Importers(ctx, s.Phone)
	if err != nil {
		return false, err
	}
	ids := make([]int64, 0, len(importers))
	names := make(map[int64]dao.Importer, len(importers))
	for _, im := range importers {
		if im.UserId != s.UserId {
			ids = append(ids, im.UserId)
			names[im.UserId] = im
		}
	}

	// The new user and the importers, as they are now; the users who are
	// gone lose the number from their phone books.
	var (
		users  *user.Vector_ImmutableUser
		live   []int64
		gone   = make(map[int64]bool)
		msgIds = make(map[int64]int64)
	)
	users, err = New(ctx, svcCtx).UserGetMutableUsers(&user.TLUserGetMutableUsers{Id: append([]int64{s.UserId}, ids...)})
	if err != nil {
		return false, err
	}
	newUser, ok := users.GetImmutableUser(s.UserId)
	if !ok {
		return false, fmt.Errorf("the new user is not loaded")
	}
	for _, id := range ids {
		it, ok := users.GetImmutableUser(id)
		switch {
		case !ok:
			return false, fmt.Errorf("importer %d is not loaded", id)
		case it.Deleted() || it.GetUser().GetUserType() != user.UserTypeRegular:
			gone[id] = true
		default:
			live = append(live, id)
		}
	}
	silenced, err := d.SignUpNotificationsSilenced(ctx, live)
	if err != nil {
		return false, err
	}
	if len(live) > 0 && !newUser.Deleted() {
		list, err := d.Ids.NextIds(ctx, len(live))
		if err != nil {
			return false, err
		}
		if len(list) != len(live) {
			return false, fmt.Errorf("idgen returned %d ids of %d", len(list), len(live))
		}
		for i, id := range live {
			msgIds[id] = list[i]
		}
	}

	done := false
	_, err = d.RunBox(ctx, s.UserId, []string{dao.GenCacheUserDataCacheKey(s.UserId)}, func(b *userbox.Box) error {
		done = false
		pending, err := dao.TakeContactSignUp(b)
		if err != nil || !pending {
			done = err == nil
			return err
		}
		if newUser.Deleted() {
			// The account is gone: the number is free again, and the
			// phone books keep it for whoever registers it next.
			done = true
			return dao.FinishContactSignUp(b)
		}

		current, err := dao.LockImporters(b, s.Phone)
		if err != nil {
			return err
		}
		var deliver, drop []int64
		later := false
		for _, id := range current {
			switch {
			case msgIds[id] != 0:
				deliver = append(deliver, id)
			case gone[id] || id == s.UserId:
				drop = append(drop, id)
			default:
				later = true // imported during this round
			}
		}
		backs, err := dao.OwnContacts(b, deliver)
		if err != nil {
			return err
		}
		contacts := make([]dao.SignUpContact, 0, len(deliver))
		for _, id := range deliver {
			contacts = append(contacts, dao.SignUpContact{
				OwnerId:   id,
				Phone:     s.Phone,
				FirstName: names[id].FirstName,
				LastName:  names[id].LastName,
				Mutual:    backs[id],
			})
		}
		if err = dao.AddSignUpContacts(b, contacts); err != nil {
			return err
		}
		if err = dao.DropImporters(b, s.Phone, append(deliver, drop...)); err != nil {
			return err
		}
		for _, c := range contacts {
			importer, _ := users.GetImmutableUser(c.OwnerId)
			seen := newUser.ToUnsafeUser(withContact(importer, newUser.Id(), c))
			message, err := d.SignUpMessage(b, c.OwnerId, msgIds[c.OwnerId], silenced[c.OwnerId], []*mtproto.User{seen})
			if err != nil {
				return err
			}
			b.Emit(message)
			push, err := d.PushUpdates(c.OwnerId, mtproto.MakeUpdatesByUpdatesUsers(
				[]*mtproto.User{seen},
				mtproto.MakeTLUpdatePeerSettings(&mtproto.Update{
					Peer_PEER: mtproto.MakePeerUser(newUser.Id()),
					Settings:  mtproto.MakeTLPeerSettings(&mtproto.PeerSettings{}).To_PeerSettings(),
				}).To_Update()))
			if err != nil {
				return err
			}
			b.Emit(push)
			b.InvalidateCache(dao.ContactCacheKeys(c.OwnerId, newUser.Id())...)
		}
		if later {
			return nil
		}
		done = true
		return dao.FinishContactSignUp(b)
	})
	return done, err
}

// withContact returns importer as it is once it has the user contactId
// among its contacts as c says: the view the new contact is shown in.
func withContact(importer *mtproto.ImmutableUser, contactId int64, c dao.SignUpContact) *mtproto.ImmutableUser {
	view := proto.Clone(importer).(*mtproto.ImmutableUser)
	view.Contacts = append(view.Contacts, mtproto.MakeTLContactData(&mtproto.ContactData{
		UserId:        c.OwnerId,
		ContactUserId: contactId,
		FirstName:     mtproto.MakeFlagsString(c.FirstName),
		LastName:      mtproto.MakeFlagsString(c.LastName),
		MutualContact: c.Mutual,
		Phone:         mtproto.MakeFlagsString(c.Phone),
	}).To_ContactData())
	return view
}
