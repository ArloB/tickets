package domain

import "testing"

func TestTailPosition(t *testing.T) {
	if got := TailPosition(0); got != PositionGap {
		t.Errorf("TailPosition(0) = %d, want %d", got, PositionGap)
	}
	if got := TailPosition(1000); got != 2000 {
		t.Errorf("TailPosition(1000) = %d, want 2000", got)
	}
}

func TestHeadPosition(t *testing.T) {
	if got := HeadPosition(0); got != -PositionGap {
		t.Errorf("HeadPosition(0) = %d, want %d", got, -PositionGap)
	}
	if got := HeadPosition(1000); got != 0 {
		t.Errorf("HeadPosition(1000) = %d, want 0", got)
	}
}

func TestMidpointPosition(t *testing.T) {
	if pos, ok := MidpointPosition(1000, 2000); !ok || pos != 1500 {
		t.Errorf("MidpointPosition(1000, 2000) = (%d, %v), want (1500, true)", pos, ok)
	}
	if pos, ok := MidpointPosition(1000, 1002); !ok || pos != 1001 {
		t.Errorf("MidpointPosition(1000, 1002) = (%d, %v), want (1001, true)", pos, ok)
	}
	// Gap exhausted: nothing strictly between adjacent integers.
	if _, ok := MidpointPosition(1000, 1001); ok {
		t.Error("MidpointPosition(1000, 1001) ok = true, want false (gap exhausted)")
	}
	if _, ok := MidpointPosition(1000, 1000); ok {
		t.Error("MidpointPosition(1000, 1000) ok = true, want false")
	}
}

func TestRenumberPositions(t *testing.T) {
	got := RenumberPositions(5)
	if len(got) != 5 {
		t.Fatalf("RenumberPositions(5) returned %d positions, want 5", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Fatalf("RenumberPositions is not strictly increasing: %v", got)
		}
		if got[i]-got[i-1] < 2 {
			t.Errorf("adjacent renumbered positions %d, %d leave no room for a future MidpointPosition insert", got[i-1], got[i])
		}
	}
}

func TestRenumberPositionsZero(t *testing.T) {
	if got := RenumberPositions(0); len(got) != 0 {
		t.Errorf("RenumberPositions(0) = %v, want empty", got)
	}
}
