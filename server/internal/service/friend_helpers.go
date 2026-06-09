package service

import (
	pb "lunar-tear/server/gen/proto"
	"lunar-tear/server/internal/gametime"
	"lunar-tear/server/internal/store"
)

func dayBucket() int64 { return gametime.StartOfDayMillis() }

func userProto(c PlayerCard) *pb.User {
	return &pb.User{
		PlayerId:          c.PlayerId,
		UserName:          c.Name,
		MaxDeckPower:      c.MaxDeckPower,
		FavoriteCostumeId: c.FavoriteCostumeId,
		Level:             c.Level,
	}
}

func friendUserProto(c PlayerCard, e store.FriendEdge) *pb.FriendUser {
	return &pb.FriendUser{
		PlayerId:          c.PlayerId,
		UserName:          c.Name,
		MaxDeckPower:      c.MaxDeckPower,
		FavoriteCostumeId: c.FavoriteCostumeId,
		Level:             c.Level,
		CheerReceived:     e.CheerReceivedPending,
		CheerSent:         e.CheerSentToday,
		StaminaReceived:   e.StaminaReceivedToday,
	}
}

func botCardFromId(pools botPools, playerId int64) PlayerCard {
	return PlayerCard{
		PlayerId:          playerId,
		Name:              botName(playerId),
		Level:             50,
		MaxDeckPower:      30000,
		FavoriteCostumeId: pick(newRand(playerId), pools.costumeIds, 1),
		IsBot:             true,
	}
}

// maybeResetCheerDay clears stale daily cheer flags. A fuller version (with bot cheer
// regeneration) replaces this in a later task.
func maybeResetCheerDay(user *store.UserState) {
	today := dayBucket()
	for pid, e := range user.Friends {
		if e.LastResetDay != today {
			e.CheerSentToday = false
			e.StaminaReceivedToday = false
			e.LastResetDay = today
			user.Friends[pid] = e
		}
	}
}
