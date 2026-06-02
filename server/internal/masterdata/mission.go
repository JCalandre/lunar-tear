package masterdata

import (
	"log"
	"sort"

	"lunar-tear/server/internal/utils"
)

// MissionCatalog is an immutable snapshot of every mission-related master-data
// table, indexed for fast lookup by the mission service and population engine.
type MissionCatalog struct {
	Missions         map[int32]EntityMMission         // by MissionId
	RewardsByGroupId map[int32][]EntityMMissionReward // by MissionRewardId
	TermById         map[int32]EntityMMissionTerm     // by MissionTermId
	GroupById        map[int32]EntityMMissionGroup    // by MissionGroupId

	PassById         map[int32]EntityMMissionPass               // by MissionPassId
	PassLevelGroup   map[int32][]EntityMMissionPassLevelGroup   // by MissionPassLevelGroupId (sorted by Level)
	PassRewardGroup  map[int32][]EntityMMissionPassRewardGroup  // by MissionPassRewardGroupId
	PassMissionGroup map[int32][]EntityMMissionPassMissionGroup // by MissionPassId
}

func LoadMissionCatalog() *MissionCatalog {
	cat := &MissionCatalog{
		Missions:         map[int32]EntityMMission{},
		RewardsByGroupId: map[int32][]EntityMMissionReward{},
		TermById:         map[int32]EntityMMissionTerm{},
		GroupById:        map[int32]EntityMMissionGroup{},
		PassById:         map[int32]EntityMMissionPass{},
		PassLevelGroup:   map[int32][]EntityMMissionPassLevelGroup{},
		PassRewardGroup:  map[int32][]EntityMMissionPassRewardGroup{},
		PassMissionGroup: map[int32][]EntityMMissionPassMissionGroup{},
	}

	if rows, err := utils.ReadTable[EntityMMission]("m_mission"); err == nil {
		for _, r := range rows {
			cat.Missions[r.MissionId] = r
		}
	} else {
		log.Printf("[MissionCatalog] load m_mission: %v", err)
	}

	if rows, err := utils.ReadTable[EntityMMissionReward]("m_mission_reward"); err == nil {
		for _, r := range rows {
			cat.RewardsByGroupId[r.MissionRewardId] = append(cat.RewardsByGroupId[r.MissionRewardId], r)
		}
	} else {
		log.Printf("[MissionCatalog] load m_mission_reward: %v", err)
	}

	if rows, err := utils.ReadTable[EntityMMissionTerm]("m_mission_term"); err == nil {
		for _, r := range rows {
			cat.TermById[r.MissionTermId] = r
		}
	} else {
		log.Printf("[MissionCatalog] load m_mission_term: %v", err)
	}

	if rows, err := utils.ReadTable[EntityMMissionGroup]("m_mission_group"); err == nil {
		for _, r := range rows {
			cat.GroupById[r.MissionGroupId] = r
		}
	} else {
		log.Printf("[MissionCatalog] load m_mission_group: %v", err)
	}

	if rows, err := utils.ReadTable[EntityMMissionPass]("m_mission_pass"); err == nil {
		for _, r := range rows {
			cat.PassById[r.MissionPassId] = r
		}
	}
	if rows, err := utils.ReadTable[EntityMMissionPassLevelGroup]("m_mission_pass_level_group"); err == nil {
		for _, r := range rows {
			cat.PassLevelGroup[r.MissionPassLevelGroupId] = append(cat.PassLevelGroup[r.MissionPassLevelGroupId], r)
		}
		for k := range cat.PassLevelGroup {
			levels := cat.PassLevelGroup[k]
			sort.SliceStable(levels, func(i, j int) bool { return levels[i].Level < levels[j].Level })
		}
	}
	if rows, err := utils.ReadTable[EntityMMissionPassRewardGroup]("m_mission_pass_reward_group"); err == nil {
		for _, r := range rows {
			cat.PassRewardGroup[r.MissionPassRewardGroupId] = append(cat.PassRewardGroup[r.MissionPassRewardGroupId], r)
		}
	}
	if rows, err := utils.ReadTable[EntityMMissionPassMissionGroup]("m_mission_pass_mission_group"); err == nil {
		for _, r := range rows {
			cat.PassMissionGroup[r.MissionPassId] = append(cat.PassMissionGroup[r.MissionPassId], r)
		}
	}

	log.Printf("mission catalog loaded: %d missions, %d reward groups, %d terms, %d groups, %d passes",
		len(cat.Missions), len(cat.RewardsByGroupId), len(cat.TermById), len(cat.GroupById), len(cat.PassById))

	return cat
}

// CategoryTypeOf returns the MissionCategoryType of the group a mission belongs
// to, or 0 if the group is unknown.
func (c *MissionCatalog) CategoryTypeOf(m EntityMMission) int32 {
	if g, ok := c.GroupById[m.MissionGroupId]; ok {
		return g.MissionCategoryType
	}
	return 0
}

// IsActiveAt reports whether a mission's term window contains nowMillis. A
// mission whose MissionTermId references an unknown term is treated as active
// (fail-open) so a missing term row never silently hides content.
func (c *MissionCatalog) IsActiveAt(m EntityMMission, nowMillis int64) bool {
	term, ok := c.TermById[m.MissionTermId]
	if !ok {
		return true
	}
	return nowMillis >= term.StartDatetime && nowMillis <= term.EndDatetime
}

// ActiveMissionsAt returns every mission whose term window contains nowMillis,
// sorted by MissionId.
func (c *MissionCatalog) ActiveMissionsAt(nowMillis int64) []EntityMMission {
	out := make([]EntityMMission, 0, len(c.Missions))
	for _, m := range c.Missions {
		if c.IsActiveAt(m, nowMillis) {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MissionId < out[j].MissionId })
	return out
}
