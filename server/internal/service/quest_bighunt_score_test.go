package service

import (
	"testing"

	"lunar-tear/server/internal/store"
)

func TestBigHuntSurvivalBonusPermil(t *testing.T) {
	cases := map[int32]int32{0: 2000, 1: 1500, 2: 1000, 3: 500, 4: 0, 5: 0, 9: 0}
	for deaths, want := range cases {
		if got := bigHuntSurvivalBonusPermil(deaths); got != want {
			t.Errorf("survival(%d deaths) = %d, want %d", deaths, got, want)
		}
	}
}

func TestBigHuntComboBonusPermil(t *testing.T) {
	cases := map[int32]int32{
		0: 0, 5: 0, 6: 200, 10: 200, 11: 400, 15: 400, 16: 600,
		21: 800, 26: 1000, 31: 1200, 36: 1400, 41: 1400, 42: 1600, 45: 1600, 46: 1800, 100: 1800,
	}
	for combo, want := range cases {
		if got := bigHuntComboBonusPermil(combo); got != want {
			t.Errorf("combo(%d) = %d, want %d", combo, got, want)
		}
	}
}

func TestBigHuntDeathCount(t *testing.T) {
	// Costume 10 dies in wave 0 and is not reported again -> counts as dead.
	// Costume 11 dies in wave 0 but is revived (alive) in wave 1 -> not dead.
	// Costume 12 stays alive throughout -> not dead.
	// Costume 13 alive in wave 0, dead in wave 1 -> dead.
	infos := []store.BigHuntCostumeBattleInfo{
		{WaveIndex: 0, CostumeId: 10, IsAlive: false},
		{WaveIndex: 0, CostumeId: 11, IsAlive: false},
		{WaveIndex: 0, CostumeId: 12, IsAlive: true},
		{WaveIndex: 0, CostumeId: 13, IsAlive: true},
		{WaveIndex: 1, CostumeId: 11, IsAlive: true},
		{WaveIndex: 1, CostumeId: 12, IsAlive: true},
		{WaveIndex: 1, CostumeId: 13, IsAlive: false},
		// Non-player entries (e.g. the defeated boss) resolve to CostumeId 0 and
		// must NOT count as a death.
		{WaveIndex: 1, CostumeId: 0, IsAlive: false},
	}
	if got := bigHuntDeathCount(infos); got != 2 {
		t.Errorf("deathCount = %d, want 2 (costumes 10 and 13; boss/CostumeId 0 excluded)", got)
	}
	if got := bigHuntDeathCount(nil); got != 0 {
		t.Errorf("deathCount(nil) = %d, want 0", got)
	}
	// Whole party survives (only the boss is dead) -> 0 deaths -> +200%.
	allAlive := []store.BigHuntCostumeBattleInfo{
		{WaveIndex: 0, CostumeId: 10, IsAlive: true},
		{WaveIndex: 0, CostumeId: 11, IsAlive: true},
		{WaveIndex: 0, CostumeId: 12, IsAlive: true},
		{WaveIndex: 0, CostumeId: 0, IsAlive: false}, // boss
	}
	if got := bigHuntDeathCount(allAlive); got != 0 {
		t.Errorf("deathCount(all party alive) = %d, want 0", got)
	}
	if got := bigHuntSurvivalBonusPermil(bigHuntDeathCount(allAlive)); got != 2000 {
		t.Errorf("survival permil (no deaths) = %d, want 2000 (+200%%)", got)
	}
}

// TestBigHuntScoreFormula verifies the full score multiplier wiring with the
// accumulated base score: userScore = base * (1000 + difficulty + alive + combo) / 1000.
func TestBigHuntScoreFormula(t *testing.T) {
	base := int64(100000)
	difficulty := int32(3500) // coefficient id 3 from masterdata
	infos := []store.BigHuntCostumeBattleInfo{
		{WaveIndex: 0, CostumeId: 1, IsAlive: true},
		{WaveIndex: 0, CostumeId: 2, IsAlive: false}, // 1 death
	}
	alive := bigHuntSurvivalBonusPermil(bigHuntDeathCount(infos)) // 1 death -> 1500
	combo := bigHuntComboBonusPermil(31)                          // 31 -> 1200
	got := base * int64(1000+difficulty+alive+combo) / 1000
	// 1000 + 3500 + 1500 + 1200 = 7200 -> 100000 * 7200 / 1000 = 720000
	if got != 720000 {
		t.Errorf("expected 720000, got %d", got)
	}
}
