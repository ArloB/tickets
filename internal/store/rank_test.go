package store

import "testing"

// TestPriorityRankOrder pins priorityRank against
// docs/contracts/enums.md's spec order (critical, high, medium, low) —
// the whole point of this column is that ORDER BY priority_rank ASC
// produces that order, which a TEXT-sorted priority column cannot.
func TestPriorityRankOrder(t *testing.T) {
	cases := []struct {
		priority string
		want     int
	}{
		{"critical", 0},
		{"high", 1},
		{"medium", 2},
		{"low", 3},
		{"bogus", 4},
		{"", 4},
	}
	for _, tc := range cases {
		if got := priorityRank(tc.priority); got != tc.want {
			t.Errorf("priorityRank(%q) = %d, want %d", tc.priority, got, tc.want)
		}
	}

	ranks := []int{priorityRank("critical"), priorityRank("high"), priorityRank("medium"), priorityRank("low")}
	for i := 1; i < len(ranks); i++ {
		if ranks[i-1] >= ranks[i] {
			t.Fatalf("priorityRank is not strictly increasing in spec order: %v", ranks)
		}
	}
}

func TestSeverityRankOrder(t *testing.T) {
	crit, high, med, low := "critical", "high", "medium", "low"
	cases := []struct {
		name     string
		severity *string
		want     int
	}{
		{"critical", &crit, 0},
		{"high", &high, 1},
		{"medium", &med, 2},
		{"low", &low, 3},
		{"nil", nil, 4},
	}
	for _, tc := range cases {
		if got := severityRank(tc.severity); got != tc.want {
			t.Errorf("severityRank(%q) = %d, want %d", tc.name, got, tc.want)
		}
	}
}
