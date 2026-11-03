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

package dao

import (
	"context"
	"slices"
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/zeromicro/go-zero/core/logx"
)

// resolvePrivacyChats prepares users for the viewers who asked for them
// (task 034, ADR 0031): every "members of these chats" rule of their
// privacy becomes the list of the viewers, and of the users themselves,
// who are members of a live basic group among the chats (mtproto.ResolvePrivacyChats), so the evaluator in
// the services that show the users needs no membership of its own. The
// rules of the cache are not changed: users get copies.
//
// Without viewers nothing is resolved, and when membership cannot be read
// the rules stay as they are; the evaluator then reads an unresolved chat
// rule as hiding.
func (d *Dao) resolvePrivacyChats(ctx context.Context, users []*mtproto.ImmutableUser, viewers []int64) {
	if len(viewers) == 0 {
		return
	}
	var chats []int64
	for _, u := range users {
		for _, k := range u.GetKeysPrivacyRules() {
			for _, r := range k.GetRules() {
				if isChatRule(r) {
					chats = append(chats, r.GetChats()...)
				}
			}
		}
	}
	if len(chats) == 0 {
		return
	}
	chats = uniqueIds(chats)
	// The users shown are subjects too: a viewer's own rules decide whether
	// the viewer sees their exact last seen (ImmutableUser.StatusFor).
	for _, u := range users {
		viewers = append(viewers, u.Id())
	}
	viewers = uniqueIds(viewers)

	type membership struct {
		ChatId int64 `db:"chat_id"`
		UserId int64 `db:"user_id"`
	}
	var rows []membership
	q := "SELECT p.chat_id, p.user_id FROM chat_participants p JOIN chats c ON c.id = p.chat_id AND c.deactivated = 0" +
		" WHERE p.chat_id IN (" + placeholders(len(chats)) + ") AND p.user_id IN (" + placeholders(len(viewers)) + ") AND p.state = ?"
	args := make([]any, 0, len(chats)+len(viewers)+1)
	for _, id := range chats {
		args = append(args, id)
	}
	for _, id := range viewers {
		args = append(args, id)
	}
	args = append(args, mtproto.ChatMemberStateNormal)
	if err := d.DB.QueryRowsPartial(ctx, &rows, q, args...); err != nil {
		logx.WithContext(ctx).Errorf("resolvePrivacyChats - chats %v: %v", chats, err)
		return
	}
	members := make(map[int64][]int64, len(chats))
	for _, r := range rows {
		members[r.ChatId] = append(members[r.ChatId], r.UserId)
	}
	of := func(list []int64) []int64 {
		var out []int64
		for _, c := range list {
			out = append(out, members[c]...)
		}
		return uniqueIds(out)
	}

	for _, u := range users {
		keys := u.GetKeysPrivacyRules()
		var resolved []*mtproto.PrivacyKeyRules
		for i, k := range keys {
			if !hasChatRules(k.GetRules()) {
				if resolved != nil {
					resolved = append(resolved, k)
				}
				continue
			}
			if resolved == nil {
				resolved = append(make([]*mtproto.PrivacyKeyRules, 0, len(keys)), keys[:i]...)
			}
			resolved = append(resolved, mtproto.MakeTLPrivacyKeyRules(&mtproto.PrivacyKeyRules{
				Key:   k.GetKey(),
				Rules: mtproto.ResolvePrivacyChats(k.GetRules(), of),
			}).To_PrivacyKeyRules())
		}
		if resolved != nil {
			u.KeysPrivacyRules = resolved
		}
	}
}

// resolveForViewer returns rules with their chat rules resolved for one
// viewer (resolvePrivacyChats for a single rule list); a failed read of the
// membership is an error.
func (d *Dao) resolveForViewer(ctx context.Context, rules []*mtproto.PrivacyRule, viewer int64) ([]*mtproto.PrivacyRule, error) {
	if !hasChatRules(rules) {
		return rules, nil
	}
	var chats []int64
	for _, r := range rules {
		if isChatRule(r) {
			chats = append(chats, r.GetChats()...)
		}
	}
	chats = uniqueIds(chats)
	var member []int64
	q := "SELECT p.chat_id FROM chat_participants p JOIN chats c ON c.id = p.chat_id AND c.deactivated = 0" +
		" WHERE p.chat_id IN (" + placeholders(len(chats)) + ") AND p.user_id = ? AND p.state = ?"
	args := make([]any, 0, len(chats)+2)
	for _, id := range chats {
		args = append(args, id)
	}
	args = append(args, viewer, mtproto.ChatMemberStateNormal)
	if err := d.DB.QueryRowsPartial(ctx, &member, q, args...); err != nil {
		return nil, err
	}
	return mtproto.ResolvePrivacyChats(rules, func(list []int64) []int64 {
		for _, c := range list {
			if slices.Contains(member, c) {
				return []int64{viewer}
			}
		}
		return nil
	}), nil
}

func isChatRule(r *mtproto.PrivacyRule) bool {
	switch r.GetPredicateName() {
	case mtproto.Predicate_privacyValueAllowChatParticipants,
		mtproto.Predicate_privacyValueDisallowChatParticipants:
		return true
	}
	return false
}

func hasChatRules(rules []*mtproto.PrivacyRule) bool {
	for _, r := range rules {
		if isChatRule(r) {
			return true
		}
	}
	return false
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
