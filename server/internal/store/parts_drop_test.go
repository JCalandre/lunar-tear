package store

import "testing"

// TestRollPartsVariantGrantsExact verifies a parts drop always grants the exact
// part id it names, never a random sibling from the same group+rarity (the bug
// where farming "Shard (Automata Crossover)" handed out other shards).
func TestRollPartsVariantGrantsExact(t *testing.T) {
	g := &PossessionGranter{
		PartsById: map[int32]PartsRef{
			8003: {PartsGroupId: 401, RarityType: 10, PartsInitialLotteryId: 3},
		},
		// A full 5-variant set is present; the old code would pick one at random.
		PartsVariantsByGroupRarity: map[int32]map[int32][]int32{
			401: {10: {8001, 8002, 8003, 8004, 8005}},
		},
	}

	for i := 0; i < 100; i++ {
		id, ref, ok := g.rollPartsVariant(8003)
		if !ok {
			t.Fatalf("expected ok for known part")
		}
		if id != 8003 {
			t.Fatalf("got wrong part id %d, want exactly 8003", id)
		}
		if ref.PartsGroupId != 401 || ref.PartsInitialLotteryId != 3 {
			t.Fatalf("ref mismatch for granted part: %+v", ref)
		}
	}

	// Unknown part: returns the requested id and ok=false (granted bare).
	if id, _, ok := g.rollPartsVariant(99999); ok || id != 99999 {
		t.Errorf("unknown part should return (99999, false), got (%d, %v)", id, ok)
	}
}
