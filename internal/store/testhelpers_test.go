package store

import (
	"context"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

// mustSystemActorID resolves the seeded 'system' actor's internal id
// (migration 0002_core_domain.sql) for tests that call InsertEntity
// directly and need a createdBy value, without exercising the full
// withTx actor-resolution path internal/service's tests cover.
func mustSystemActorID(t *testing.T, q Querier) int64 {
	t.Helper()
	id, err := GetActorIDByRef(context.Background(), q, domain.ActorSystem, "system")
	if err != nil {
		t.Fatalf("resolve system actor id: %v", err)
	}
	return id
}
