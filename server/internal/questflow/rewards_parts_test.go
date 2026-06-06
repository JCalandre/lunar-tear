package questflow

import "testing"

func TestGrantPartsDropN(t *testing.T) {
	cases := []struct {
		count     int32
		wantCalls int
	}{
		{count: 3, wantCalls: 3}, // a Variation quest's 3 memoirs in one row
		{count: 1, wantCalls: 1},
		{count: 0, wantCalls: 1},  // guard: treat <1 as 1
		{count: -5, wantCalls: 1}, // guard
		{count: 10, wantCalls: 10},
	}
	for _, c := range cases {
		calls := 0
		grantPartsDropN(c.count, func() (int32, bool) {
			calls++
			return 0, false
		})
		if calls != c.wantCalls {
			t.Errorf("count=%d: grantOne called %d times, want %d", c.count, calls, c.wantCalls)
		}
	}
}

func TestGrantPartsDropNReportsLastSold(t *testing.T) {
	// 3 rolls: ids 100 (kept), 200 (sold), 300 (sold) -> reports last sold = 300.
	ids := []int32{100, 200, 300}
	sold := []bool{false, true, true}
	i := 0
	lastSold, anySold := grantPartsDropN(3, func() (int32, bool) {
		id, s := ids[i], sold[i]
		i++
		return id, s
	})
	if !anySold || lastSold != 300 {
		t.Errorf("got lastSold=%d anySold=%v, want 300/true", lastSold, anySold)
	}

	// All kept -> anySold false.
	_, anySold = grantPartsDropN(2, func() (int32, bool) { return 1, false })
	if anySold {
		t.Error("anySold should be false when nothing was sold")
	}
}
