package service

import (
	"encoding/json"

	pb "lunar-tear/server/gen/proto"
	"lunar-tear/server/internal/runtime"
	"lunar-tear/server/internal/store"
)

// PlayerCard is the common currency for friend/arena list rows.
type PlayerCard struct {
	PlayerId          int64
	Name              string
	Level             int32
	MaxDeckPower      int32
	FavoriteCostumeId int32
	PvpPoint          int32
	LastLoginDatetime int64 // millis; 0 for bots (proto mapper substitutes now)
	IsBot             bool
}

// PlayerDirectory answers "who else is out there?" from real snapshots + synthesized bots.
type PlayerDirectory struct {
	snaps  store.SnapshotRepository
	holder *runtime.Holder
}

func NewPlayerDirectory(snaps store.SnapshotRepository, holder *runtime.Holder) *PlayerDirectory {
	return &PlayerDirectory{snaps: snaps, holder: holder}
}

func (d *PlayerDirectory) pools() botPools {
	cat := d.holder.Get()
	p := botPools{}
	if cat != nil {
		if cat.Costume != nil {
			p.costumeIds = sortedInt32Keys(cat.Costume.Costumes)
		}
		if cat.Weapon != nil {
			p.weaponIds = sortedInt32Keys(cat.Weapon.Weapons)
		}
		if cat.Companion != nil {
			p.companionIds = sortedInt32Keys(cat.Companion.CompanionById)
		}
	}
	return p
}

func cardFromSnapshot(s store.PlayerSnapshot) PlayerCard {
	return PlayerCard{
		PlayerId:          s.PlayerId,
		Name:              s.UserName,
		Level:             s.Level,
		MaxDeckPower:      s.MaxDeckPower,
		FavoriteCostumeId: s.FavoriteCostumeId,
		PvpPoint:          s.PvpPoint,
		LastLoginDatetime: s.LastLoginDatetime,
		IsBot:             false,
	}
}

// RealPlayersNear returns real snapshots (excluding the viewer) closest to nearPoint.
func (d *PlayerDirectory) RealPlayersNear(viewerId int64, nearPoint int32, limit int) []PlayerCard {
	snaps, err := d.snaps.ListSnapshotsNear(viewerId, nearPoint, limit)
	if err != nil {
		return nil
	}
	out := make([]PlayerCard, 0, len(snaps))
	for _, s := range snaps {
		out = append(out, cardFromSnapshot(s))
	}
	return out
}

// FillWithBots tops the list up to targetCount with deterministic bots seeded by viewer+day.
func (d *PlayerDirectory) FillWithBots(existing []PlayerCard, targetCount int, viewerId int64, dayBucket int64, nearPoint int32) []PlayerCard {
	if len(existing) >= targetCount {
		return existing
	}
	pools := d.pools()
	out := existing
	for slot := 0; len(out) < targetCount; slot++ {
		out = append(out, synthBot(pools, viewerId, slot, dayBucket, nearPoint))
	}
	return out
}

// DefenseDeckOf returns the opponent deck for a card: deserialized snapshot deck for a real
// player, or a synthesized deck for a bot.
func (d *PlayerDirectory) DefenseDeckOf(card PlayerCard) []*pb.PvpDeckCharacter {
	if card.IsBot {
		return synthBotDeck(d.pools(), card.PlayerId)
	}
	snap, err := d.snaps.GetSnapshot(card.PlayerId)
	if err != nil {
		return nil
	}
	var deck []*pb.PvpDeckCharacter
	if snap.DefenseDeckJson != "" {
		_ = json.Unmarshal([]byte(snap.DefenseDeckJson), &deck)
	}
	return deck
}

func (d *PlayerDirectory) IsBot(playerId int64) bool { return IsBotId(playerId) }

// CardFor resolves a single player/opponent by id: a bot id → a synthesized card;
// a real id → the player's snapshot card. Returns (zero, false) if a real player has no snapshot.
func (d *PlayerDirectory) CardFor(playerId int64) (PlayerCard, bool) {
	if IsBotId(playerId) {
		return botCardFromId(d.pools(), playerId), true
	}
	snap, err := d.snaps.GetSnapshot(playerId)
	if err != nil {
		return PlayerCard{}, false
	}
	return cardFromSnapshot(snap), true
}
