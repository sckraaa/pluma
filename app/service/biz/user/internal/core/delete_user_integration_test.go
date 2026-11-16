//go:build integration

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
	"testing"
)

// An account deleted takes with it its rows in the tables of the other
// packages (dao.UserRows): of every table that has one row of the deleted
// account and one of another, only the other's stays. game_scores keeps
// both: a score belongs to the high score table of a chat.
func TestDeleteUserRows(t *testing.T) {
	ctx := context.Background()
	gone, phone := newUser(t, "Gone")
	stays, _ := newUser(t, "Stays")
	n := itSeq.Add(1000)

	rows := []struct {
		table  string
		insert string
		args   func(user int64, i int64) []any
	}{
		{"message_read_metrics", "(user_id, peer_type, peer_id, msg_id, view_id, time_in_view_ms, active_time_in_view_ms, height_to_viewport_ratio_permille, seen_range_ratio_permille, date) VALUES (?, 4, ?, 1, 1, 1, 1, 1, 1, 1)",
			func(u, i int64) []any { return []any{u, i} }},
		{"music_listens", "(user_id, document_id, minute, listened_duration, date) VALUES (?, ?, 1, 1, 1)",
			func(u, i int64) []any { return []any{u, i} }},
		{"business_profiles", "(user_id, updated_at) VALUES (?, 1)",
			func(u, _ int64) []any { return []any{u} }},
		{"business_auto_reply_settings", "(user_id, kind, shortcut_id, recipients, enabled_at) VALUES (?, 1, 1, '{}', 1)",
			func(u, _ int64) []any { return []any{u} }},
		{"business_auto_replies", "(user_id, peer_id, kind, trigger_box_id, shortcut_id, created_at) VALUES (?, ?, 1, 1, 1, 1)",
			func(u, i int64) []any { return []any{u, i} }},
		{"business_auto_reply_log", "(user_id, peer_id, kind, sent_at) VALUES (?, ?, 1, 1)",
			func(u, i int64) []any { return []any{u, i} }},
		{"business_chat_links", "(slug, user_id, message, entities, created_at) VALUES (?, ?, 'hello', '', 1)",
			func(u, i int64) []any { return []any{fmt.Sprintf("s%d", i), u} }},
		{"business_bot_connections", "(user_id, bot_id, connection_id, rights, recipients, created_at, updated_at) VALUES (?, ?, ?, 1, '{}', 1, 1)",
			func(u, i int64) []any { return []any{u, i, fmt.Sprintf("c%d", i)} }},
		{"business_bot_paused_peers", "(user_id, peer_id, paused_at) VALUES (?, ?, 1)",
			func(u, i int64) []any { return []any{u, i} }},
		{"quick_reply_shortcuts", "(user_id, shortcut, shortcut_key, sort, created_at) VALUES (?, 'hi', ?, 1, 1)",
			func(u, i int64) []any { return []any{u, fmt.Sprintf("hi%d", i)} }},
		{"quick_reply_messages", "(user_id, shortcut_id, message_data, date) VALUES (?, 1, '{}', 1)",
			func(u, _ int64) []any { return []any{u} }},
		{"user_web_browser_settings", "(user_id, open_external_browser, display_close_button, updated_at) VALUES (?, 1, 1, 1)",
			func(u, _ int64) []any { return []any{u} }},
		{"user_web_browser_exceptions", "(user_id, domain, url, open_external, created_at) VALUES (?, 'example.com', 'https://example.com/', 1, 1)",
			func(u, _ int64) []any { return []any{u} }},
		{"peer_translation_settings", "(user_id, peer_type, peer_id, disabled, updated_at) VALUES (?, 2, ?, 1, 1)",
			func(u, i int64) []any { return []any{u, i} }},
		{"web_authorizations", "(hash, user_id, bot_id, domain, browser, platform, ip, region, date_created, date_active) VALUES (?, ?, ?, 'example.com', 'Firefox', 'Linux', '192.0.2.1', 'Nowhere', 1, 1)",
			func(u, i int64) []any { return []any{i, u, i} }},
		{"bot_write_allowed", "(user_id, bot_id, source, allowed_at) VALUES (?, ?, 1, 1)",
			func(u, i int64) []any { return []any{u, i} }},
		{"bot_app_launches", "(user_id, app_id, launched_at) VALUES (?, ?, 1)",
			func(u, i int64) []any { return []any{u, i} }},
		{"bot_attach_menu_installs", "(user_id, bot_id, installed_at) VALUES (?, ?, 1)",
			func(u, i int64) []any { return []any{u, i} }},
		{"bot_prepared", "(id, kind, bot_id, user_id, payload, expire_at) VALUES (?, 1, ?, ?, 'x', 1)",
			func(u, i int64) []any { return []any{fmt.Sprintf("p%d", i), i, u} }},
		{"game_scores", "(game_id, chat_type, chat_id, user_id, score, updated_at) VALUES (?, 2, 1, ?, 1, 1)",
			func(u, i int64) []any { return []any{n, u} }},
	}
	for i, r := range rows {
		for j, u := range []int64{gone, stays} {
			if _, err := itRaw.ExecContext(ctx, "INSERT INTO "+r.table+" "+r.insert, r.args(u, n*100+int64(i*2+j))...); err != nil {
				t.Fatalf("%s: %v", r.table, err)
			}
		}
	}

	if !itSvc.Dao.DeleteUser(ctx, gone, phone, "test") {
		t.Fatal("the account was not deleted")
	}
	count := func(table string, user int64) int {
		var c int
		if err := itRaw.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE user_id = ?", user).Scan(&c); err != nil {
			t.Fatalf("%s: %v", table, err)
		}
		return c
	}
	for _, r := range rows {
		want := 0
		if r.table == "game_scores" {
			want = 1
		}
		if c := count(r.table, gone); c != want {
			t.Errorf("%s: %d rows of the deleted account, want %d", r.table, c, want)
		}
		if c := count(r.table, stays); c != 1 {
			t.Errorf("%s: %d rows of another account, want 1", r.table, c)
		}
	}
}
