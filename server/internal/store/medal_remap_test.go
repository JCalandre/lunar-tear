package store

import (
	"testing"

	"lunar-tear/server/internal/model"
)

func TestCanonicalConsumableMedalId(t *testing.T) {
	// Tier variants remap to their spendable base medal.
	remaps := map[int32]int32{53: 22, 54: 22, 63: 29, 64: 29, 65: 29}
	for from, want := range remaps {
		if got := CanonicalConsumableMedalId(from); got != want {
			t.Errorf("%d -> %d, want %d", from, got, want)
		}
	}
	// Base medals and unrelated consumables are untouched.
	for _, id := range []int32{22, 29, 1, 99, 8002} {
		if got := CanonicalConsumableMedalId(id); got != id {
			t.Errorf("%d remapped to %d, want unchanged", id, got)
		}
	}
}

func TestGrantPossessionRemapsTierMedal(t *testing.T) {
	u := &UserState{ConsumableItems: map[int32]int32{}}

	// Copper + Silver Coffin of Repose Medals both land on the base id 22.
	GrantPossession(u, model.PossessionTypeConsumableItem, 53, 5)
	GrantPossession(u, model.PossessionTypeConsumableItem, 54, 3)
	if u.ConsumableItems[53] != 0 || u.ConsumableItems[54] != 0 {
		t.Errorf("tier variants should not be granted directly (53=%d 54=%d)", u.ConsumableItems[53], u.ConsumableItems[54])
	}
	if u.ConsumableItems[22] != 8 {
		t.Errorf("Coffin of Repose tiers should accumulate on base id 22, got %d (want 8)", u.ConsumableItems[22])
	}

	// Gold Rhythm's Citadel Medal lands on base id 29.
	GrantPossession(u, model.PossessionTypeConsumableItem, 65, 2)
	if u.ConsumableItems[29] != 2 {
		t.Errorf("id-65 grant should land on base id 29, got %d", u.ConsumableItems[29])
	}

	// A non-remapped consumable is unaffected.
	GrantPossession(u, model.PossessionTypeConsumableItem, 99, 3)
	if u.ConsumableItems[99] != 3 {
		t.Errorf("id 99 grant = %d, want 3", u.ConsumableItems[99])
	}
}
