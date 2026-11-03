// Package presence keeps whether a user is online across their devices and
// decides when the change is worth telling other users (ADR 0030).
//
// Every authorization keeps its own "seen until" moment: the end of the
// online window account.updateStatus(offline=false) opened, or the moment of
// account.updateStatus(offline=true). The user is online while any of them is
// ahead, so a phone going to the background does not take a working Desktop
// offline. The user is seen until the latest of them.
//
// Viewers' clients keep the status they were last told and turn
// userStatusOnline into "last seen" on their own once its expires passes
// (Telegram-iOS PeerPresenceStatusManager.swift, tdesktop
// api_updates.cpp). So a push is due when the user comes online, when they go
// offline before the told window is over, and, while they stay online,
// before that window runs out. Clients repeat account.updateStatus every
// 30 s (iOS) to 210 s (tdesktop with the server's online_update_period_ms),
// so an online user is told again once less than Refresh of the told window
// is left: every request of a client with a period over Refresh pushes, and
// one with a shorter period is sure to come again before the window ends.
package presence

import (
	"context"
	"fmt"
	"strconv"

	"github.com/zeromicro/go-zero/core/stores/kv"
)

const (
	// Window is how long one account.updateStatus(offline=false) keeps the
	// user online. It is longer than the longest request period of the
	// clients (tdesktop, 210 s with teamgramd/bin/config.json).
	Window = 300

	// Refresh is the part of the told window under which an online user is
	// told again.
	Refresh = Window / 2

	// keyTTL keeps the state of a user who stopped using Pluma from staying
	// in Redis for ever. A user back after it is told to everyone again.
	keyTTL = 30 * 24 * 60 * 60

	keyPrefix = "presence_"
)

func key(userId int64) string { return keyPrefix + strconv.FormatInt(userId, 10) }

func deviceField(authKeyId int64) string { return "d" + strconv.FormatInt(authKeyId, 10) }

// The hash of a user holds the seen until of each device ("d<auth key id>")
// and "told": the moment viewers were last told the user is online until, or
// 0 once they were told the user went offline.
//
// KEYS[1] presence of the user; ARGV device field, the device's seen until,
// now, refresh, key ttl. Returns {seen until, online (0/1), push (0/1)}.
//
// The devices whose moment has passed are dropped, but for the calling one:
// the user's seen until is the latest moment of the devices left, so it is
// never earlier than the caller's. All of it runs in one script, so two
// devices updating at once cannot both decide to push the same change.
const updateScript = `
local now = tonumber(ARGV[3])
redis.call('HSET', KEYS[1], ARGV[1], ARGV[2])
local seen = tonumber(ARGV[2])
local told = 0
local all = redis.call('HGETALL', KEYS[1])
for i = 1, #all, 2 do
  local f, v = all[i], tonumber(all[i + 1]) or 0
  if f == 'told' then
    told = v
  elseif f ~= ARGV[1] then
    if v <= now then
      redis.call('HDEL', KEYS[1], f)
    elseif v > seen then
      seen = v
    end
  end
end
local online = 0
local push = 0
if seen > now then
  online = 1
  if told - now < tonumber(ARGV[4]) then
    push = 1
    redis.call('HSET', KEYS[1], 'told', seen)
  end
elseif told ~= 0 then
  push = 1
  redis.call('HSET', KEYS[1], 'told', 0)
end
redis.call('EXPIRE', KEYS[1], ARGV[5])
return {seen, online, push}`

// Change is the user's status after an account.updateStatus.
type Change struct {
	// SeenUntil is the end of the online window when Online, and the moment
	// the user went offline otherwise.
	SeenUntil int64
	Online    bool
	// Push says the other users have to be told.
	Push bool
}

// Store keeps the presence of users in Redis.
type Store struct {
	kv kv.Store
}

// NewStore returns a Store on kv.
func NewStore(kv kv.Store) *Store {
	return &Store{kv: kv}
}

// Update records account.updateStatus of one authorization of the user at
// now (unix seconds) and returns the user's status.
func (s *Store) Update(ctx context.Context, userId, authKeyId int64, online bool, now int64) (Change, error) {
	until := now
	if online {
		until = now + Window
	}

	r, err := s.kv.EvalCtx(ctx, updateScript, key(userId), deviceField(authKeyId), until, now, Refresh, keyTTL)
	if err != nil {
		return Change{}, fmt.Errorf("presence: update of user %d: %w", userId, err)
	}
	v, ok := r.([]any)
	if !ok || len(v) != 3 {
		return Change{}, fmt.Errorf("presence: update of user %d: unexpected reply %T", userId, r)
	}
	var n [3]int64
	for i := range n {
		if n[i], ok = v[i].(int64); !ok {
			return Change{}, fmt.Errorf("presence: update of user %d: unexpected reply element %T", userId, v[i])
		}
	}

	return Change{SeenUntil: n[0], Online: n[1] == 1, Push: n[2] == 1}, nil
}
