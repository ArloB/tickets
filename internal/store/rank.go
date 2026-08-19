package store

import "github.com/ArloB/tickets/internal/domain"

// priorityRank and severityRank are the single place tickets.priority_rank
// / severity_rank / features.priority_rank get computed (migration
// 0002_core_domain.sql's comment on why: priority/severity are TEXT and
// sort alphabetically to critical, high, low, medium, which is wrong for
// the priority queue and issue register). Every INSERT or UPDATE that
// writes priority or severity must also write the matching rank through
// these functions — never recomputed ad hoc in a query.
//
// Order matches docs/contracts/enums.md's spec order (critical, high,
// medium, low); 4 is the sentinel for a NULL or unrecognized value, and
// sorts after every real one.
func priorityRank(p string) int {
	switch domain.Priority(p) {
	case domain.PriorityCritical:
		return 0
	case domain.PriorityHigh:
		return 1
	case domain.PriorityMedium:
		return 2
	case domain.PriorityLow:
		return 3
	default:
		return 4
	}
}

func severityRank(s *string) int {
	if s == nil {
		return 4
	}
	switch domain.Severity(*s) {
	case domain.SeverityCritical:
		return 0
	case domain.SeverityHigh:
		return 1
	case domain.SeverityMedium:
		return 2
	case domain.SeverityLow:
		return 3
	default:
		return 4
	}
}
