package service

import (
	"encoding/json"

	"lunar-tear/server/internal/gametime"
	"lunar-tear/server/internal/store"
)

// RefreshSnapshot writes the user's public face to the snapshot table.
// Best-effort: returns an error for logging but must not fail the caller's RPC.
// Accounts without a display name (onboarding not completed) are skipped so they
// don't appear as blank rows in other players' friend/arena lists.
func RefreshSnapshot(snaps store.SnapshotRepository, user *store.UserState) error {
	if user.Profile.Name == "" {
		return nil
	}
	dt, dn := PickDefenseDeck(user)
	deck := BuildPvpDeckCharacters(user, dt, dn)
	deckJSON, err := json.Marshal(deck)
	if err != nil {
		deckJSON = []byte("[]")
	}
	snap := store.PlayerSnapshot{
		PlayerId:          user.PlayerId,
		UserName:          user.Profile.Name,
		Level:             playerLevel(user),
		MaxDeckPower:      maxDeckPower(user),
		FavoriteCostumeId: favoriteCostumeId(user),
		PvpPoint:          user.Pvp.PvpPoint,
		LastLoginDatetime: gametime.NowMillis(),
		DefenseDeckJson:   string(deckJSON),
		UpdatedAt:         gametime.NowMillis(),
	}
	return snaps.UpsertSnapshot(snap)
}

func playerLevel(user *store.UserState) int32 {
	return user.Status.Level
}

func maxDeckPower(user *store.UserState) int32 {
	var max int32
	for _, note := range user.DeckTypeNotes {
		if note.MaxDeckPower > max {
			max = note.MaxDeckPower
		}
	}
	return max
}

func favoriteCostumeId(user *store.UserState) int32 {
	return user.Profile.FavoriteCostumeId
}
