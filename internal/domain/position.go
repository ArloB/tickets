package domain

// Position arithmetic for product spec §5.6's manual ordering within a
// (project, priority) group. These are pure functions with no I/O and
// no knowledge of which rows currently exist — internal/service reads
// a group's current positions, calls these, and writes the result back
// inside a transaction (ADR 0011). Keeping the arithmetic here makes
// the renumber-on-exhaustion path unit-testable without a database.

// PositionGap is the default spacing between adjacent positions when
// allocating fresh slots (a tail/head append, or a full renumber).
// Wide enough that ordinary drag-and-drop reordering finds room via
// MidpointPosition many times before a group needs renumbering.
const PositionGap = 1000

// TailPosition returns the position for a record appended to the end
// of its group. lastPosition is the highest existing position in the
// group, or 0 if the group is currently empty.
func TailPosition(lastPosition int64) int64 {
	return lastPosition + PositionGap
}

// HeadPosition returns the position for a record moved to the front of
// its group. firstPosition is the lowest existing position in the
// group, or 0 if the group is currently empty. The result can go
// negative under repeated head-inserts; that's fine — position only
// has to sort correctly, not stay positive.
func HeadPosition(firstPosition int64) int64 {
	return firstPosition - PositionGap
}

// MidpointPosition returns a position strictly between before and
// after (before < after), for moving a record between two existing
// ones. ok is false when there is no integer strictly between them —
// the gap between these two neighbors is exhausted, and the caller
// must renumber the whole group (RenumberPositions) before retrying.
func MidpointPosition(before, after int64) (pos int64, ok bool) {
	if after-before < 2 {
		return 0, false
	}
	return before + (after-before)/2, true
}

// RenumberPositions returns n evenly spaced fresh positions, in order,
// for a group being renumbered from scratch after MidpointPosition ran
// out of room. The caller writes these back to the group's n rows in
// their existing relative order — renumbering must never change which
// record is before which, only widen the gaps between them.
func RenumberPositions(n int) []int64 {
	positions := make([]int64, n)
	for i := range positions {
		positions[i] = int64(i+1) * PositionGap
	}
	return positions
}
