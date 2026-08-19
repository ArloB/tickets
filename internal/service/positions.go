package service

import (
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// placement is the outcome of planning a reorder: either a single
// head/tail/midpoint position was found (Renumber is nil), or the
// whole group needs fresh positions (Renumber holds the full ordered
// entity id list — movedEntityID spliced into its target slot — for
// the caller to run domain.RenumberPositions against).
type placement struct {
	Position int64
	Renumber []int64
}

// planPlacement decides where movedEntityID lands among others
// (already ordered by position ascending, movedEntityID excluded) when
// asked to move to insertIdx (0 == head, len(others) == tail). It
// tries the cheap path — a head/tail append, or domain.MidpointPosition
// between the two neighbors at the target slot — before falling back
// to a full renumber (ADR 0011).
func planPlacement(movedEntityID int64, others []store.GroupMember, insertIdx int) placement {
	if len(others) == 0 {
		return placement{Position: domain.TailPosition(0)}
	}
	if insertIdx == 0 {
		return placement{Position: domain.HeadPosition(others[0].Position)}
	}
	if insertIdx == len(others) {
		return placement{Position: domain.TailPosition(others[len(others)-1].Position)}
	}
	before := others[insertIdx-1].Position
	after := others[insertIdx].Position
	if pos, ok := domain.MidpointPosition(before, after); ok {
		return placement{Position: pos}
	}

	full := make([]int64, 0, len(others)+1)
	for i := 0; i < insertIdx; i++ {
		full = append(full, others[i].EntityID)
	}
	full = append(full, movedEntityID)
	for i := insertIdx; i < len(others); i++ {
		full = append(full, others[i].EntityID)
	}
	return placement{Renumber: full}
}
