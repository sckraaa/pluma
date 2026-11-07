package chat

import "testing"

// The keys are biz chat's: a change of their form would leave the cache
// of another service's writes in place.
func TestCacheKeys(t *testing.T) {
	if got := CacheKey(42); got != "chat#42" {
		t.Errorf("CacheKey %q", got)
	}
	if got := ParticipantCacheKey(42, 7); got != "chat_participant.3#42_7" {
		t.Errorf("ParticipantCacheKey %q", got)
	}
}
