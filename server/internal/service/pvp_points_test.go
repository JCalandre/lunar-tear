package service

import "testing"

func TestPointDelta_winLossFloor(t *testing.T) {
	if d := pointDelta(1000, 1000, true); d != 20 {
		t.Fatalf("even win want 20 got %d", d)
	}
	if d := pointDelta(1000, 2000, true); d <= 20 {
		t.Fatalf("beating stronger opp should exceed base, got %d", d)
	}
	if d := pointDelta(1000, 5000, true); d > 50 {
		t.Fatalf("win bonus must cap at 50, got %d", d)
	}
	if d := pointDelta(1000, 1000, false); d != -10 {
		t.Fatalf("loss want -10 got %d", d)
	}
	if v := applyPointDelta(5, -10); v != 0 {
		t.Fatalf("points must floor at 0, got %d", v)
	}
}
