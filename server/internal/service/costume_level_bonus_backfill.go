package service

import (
	"log"

	"lunar-tear/server/internal/gametime"
	"lunar-tear/server/internal/runtime"
	"lunar-tear/server/internal/store"
)

// BackfillCostumeLevelBonuses confirms every already-earned costume level bonus for all
// existing accounts on startup, so costumes leveled before this feature shipped grant their
// permanent per-character ("Karma") stats immediately, without the player tapping through the
// in-game confirm popups. New thresholds crossed in-game are still confirmed via the
// RegisterLevelBonusConfirmed RPC.
//
// Idempotent: each costume's ConfirmedBonusLevel gates re-application, so only thresholds in
// (ConfirmedBonusLevel, currentLevel] are added on any given run. Reuses applyCostumeLevelBonus.
func BackfillCostumeLevelBonuses(users store.UserRepository, snaps store.SnapshotRepository, holder *runtime.Holder) {
	cat := holder.Get()
	if cat == nil || cat.Costume == nil {
		return
	}
	catalog := cat.Costume

	ids, err := snaps.AllUserIds()
	if err != nil {
		log.Printf("[costumebonus] backfill: list users failed: %v", err)
		return
	}
	now := gametime.NowMillis()
	confirmed := 0
	for _, id := range ids {
		users.UpdateUser(id, func(u *store.UserState) {
			for _, costume := range u.Costumes {
				cm, ok := catalog.Costumes[costume.CostumeId]
				if !ok || cm.CostumeLevelBonusId == 0 {
					continue // costume has no level-bonus group
				}
				release := u.CostumeLevelBonusReleaseStatuses[costume.CostumeId]
				if costume.Level <= release.ConfirmedBonusLevel {
					continue // already confirmed up to (or beyond) current level
				}
				applyCostumeLevelBonus(catalog, u, cm.CharacterId, costume.CostumeId, release.ConfirmedBonusLevel, costume.Level, now)
				release.CostumeId = costume.CostumeId
				if costume.Level > release.LastReleasedBonusLevel {
					release.LastReleasedBonusLevel = costume.Level
				}
				release.ConfirmedBonusLevel = costume.Level
				release.LatestVersion = now
				u.CostumeLevelBonusReleaseStatuses[costume.CostumeId] = release
				confirmed++
			}
		})
	}
	log.Printf("[costumebonus] backfill complete: confirmed level bonuses for %d costume(s) across %d account(s)", confirmed, len(ids))
}
