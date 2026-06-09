package service

import (
	"testing"

	"lunar-tear/server/internal/store"
)

func TestMaybeResetCheerDay_clearsStaleFlags(t *testing.T) {
	u := &store.UserState{Friends: map[int64]store.FriendEdge{
		1: {PlayerId: 1, CheerSentToday: true, StaminaReceivedToday: true, LastResetDay: 0},
	}}
	maybeResetCheerDay(u)
	e := u.Friends[1]
	if e.CheerSentToday || e.StaminaReceivedToday {
		t.Fatalf("stale daily flags not cleared: %+v", e)
	}
	if e.LastResetDay != dayBucket() {
		t.Fatalf("reset day not stamped")
	}
}
