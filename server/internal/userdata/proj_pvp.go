package userdata

import (
	"lunar-tear/server/internal/gametime"
	"lunar-tear/server/internal/store"
	"lunar-tear/server/internal/utils"
)

// pvpMaxBattlePointMilli is the arena BP cap in milli-units. The client stores
// arena BP in the same "stamina" shape as the main AP (1 unit = 1000 milli) and
// caps it at m_config PVP_MAX_BATTLE_POINT (= 100). Each battle costs
// PVP_BATTLE_CONSUME_BATTLE_POINT (10) and a reroll costs
// PVP_UPDATE_MATCHING_CONSUME_BATTLE_POINT (5); BP regenerates 1 unit every
// USER_BATTLE_POINT_RECOVERY_SECOND (180s).
const pvpMaxBattlePointMilli = 100 * 1000

func init() {
	// IUserPvpStatus carries the player's arena BP (as staminaMilliValue) plus
	// win-streak and reward-receipt bookkeeping. The client reads BP from this
	// record before letting you press Start; if the table is empty the arena
	// throws locally ("fail to reconnect") and shows 0 BP. The server does not
	// yet track/deduct BP, so we report it full on every fetch (effectively
	// unlimited BP, matching the preservation-server stance). Fields and casing
	// match the client's EntityIUserPvpStatus.
	register("IUserPvpStatus", func(user store.UserState) string {
		now := gametime.NowMillis()
		s, _ := utils.EncodeJSONMaps(map[string]any{
			"userId":                              user.UserId,
			"staminaMilliValue":                   pvpMaxBattlePointMilli,
			"staminaUpdateDatetime":               now,
			"latestRewardReceivePvpSeasonId":      0,
			"latestRewardReceivePvpWeeklyVersion": 0,
			"winStreakCount":                      0,
			"winStreakCountUpdateDatetime":        0,
			"latestVersion":                       now,
		})
		return s
	})

	// IUserPvpDefenseDeck (the player's chosen defense deck slot) and
	// IUserPvpWeeklyResult (past weekly standings) have no server state yet;
	// an empty record set is valid (no defense deck configured / no history).
	registerStatic("IUserPvpDefenseDeck", "IUserPvpWeeklyResult")
}
