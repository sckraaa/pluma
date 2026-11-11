package core

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func TestCanEditChatParticipantRank(t *testing.T) {
	member := func(id int64) *mtproto.ImmutableChatParticipant {
		return &mtproto.ImmutableChatParticipant{UserId: id, State: mtproto.ChatMemberStateNormal, ParticipantType: mtproto.ChatMemberNormal}
	}
	admin := func(id, inviter int64, manage bool) *mtproto.ImmutableChatParticipant {
		return &mtproto.ImmutableChatParticipant{
			UserId: id, InviterUserId: inviter, State: mtproto.ChatMemberStateNormal, ParticipantType: mtproto.ChatMemberAdmin,
			AdminRights: mtproto.MakeTLChatAdminRights(&mtproto.ChatAdminRights{ManageRanks: manage}).To_ChatAdminRights(),
		}
	}
	creator := &mtproto.ImmutableChatParticipant{UserId: 1, State: mtproto.ChatMemberStateNormal, ParticipantType: mtproto.ChatMemberCreator}
	banned := &mtproto.ChatBannedRights{EditRank: true}
	tests := []struct {
		name             string
		operator, target *mtproto.ImmutableChatParticipant
		rights           *mtproto.ChatBannedRights
		want             bool
	}{
		{"creator, a member", creator, member(2), banned, true},
		{"member, its own", member(2), member(2), nil, true},
		{"member, its own, forbidden", member(2), member(2), banned, false},
		{"member, another", member(2), member(3), nil, false},
		{"admin who manages, its own, forbidden", admin(2, 1, true), admin(2, 1, true), banned, true},
		{"admin who does not manage, its own, forbidden", admin(2, 1, false), admin(2, 1, false), banned, false},
		{"admin who manages, a member", admin(2, 1, true), member(3), nil, true},
		{"admin who does not manage, a member", admin(2, 1, false), member(3), nil, false},
		{"admin who manages, an admin it promoted", admin(2, 1, true), admin(3, 2, false), nil, true},
		{"admin who manages, another admin", admin(2, 1, true), admin(3, 1, false), nil, false},
		{"admin who manages, the creator", admin(2, 1, true), creator, nil, false},
		{"not a member", nil, member(3), nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canEditChatParticipantRank(tt.operator, tt.target, tt.rights); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
