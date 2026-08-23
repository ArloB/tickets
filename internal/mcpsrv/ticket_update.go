package mcpsrv

import (
	"context"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

// UpdateTicketInput is ticket_update's Backend-facing input: a nil
// pointer field means "leave unchanged," matching
// apiclient.UpdateTicketOptions' convention (HTTPBackend forwards to
// it almost directly). ExpectedVersion is always required — unlike
// apiclient.UpdateTicketOptions' optional-with-auto-fetch convenience,
// every caller of this Backend method (tools.go's ticket_update tool)
// already has a version in hand from a prior *_get/*_list/*_create
// call, so there's no ergonomic reason to allow it to be missing here
// the way the underlying HTTP contract never allows a missing If-Match
// (docs/contracts/concurrency.md).
type UpdateTicketInput struct {
	Ref                                          string
	Status                                       *string
	Type, Title, Description, Priority, Severity *string
	ExpectedVersion                              int64
}

// updateTicketInProcess mirrors apiclient.Client.UpdateTicket's merge
// semantics (status via UpdateTicketStatus, fields via
// UpdateTicketFields, a full-representation PUT needing a fetched
// baseline to avoid clobbering fields the caller didn't mention) but
// against *service.Service directly — InProcessBackend's own path,
// since it talks typed Go rather than HTTP+JSON and has no reason to
// round-trip through apiclient. Unlike apiclient's version, there is
// no version-discovery branch: ExpectedVersion is always supplied
// here (see UpdateTicketInput's doc comment), so the only remaining
// question is whether a merge-fetch GetTicket is needed before the
// PUT-equivalent UpdateTicketFields call — reusing UpdateTicketStatus's
// response when one just ran, exactly as apiclient.UpdateTicket does.
func updateTicketInProcess(ctx context.Context, svc *service.Service, ref domain.Reference, in UpdateTicketInput, actor domain.ActorRef, correlationID string) (domain.Ticket, error) {
	ifMatch := in.ExpectedVersion
	var result domain.Ticket
	resultKnown := false

	if in.Status != nil {
		t, err := svc.UpdateTicketStatus(ctx, service.UpdateTicketStatusRequest{
			Ref: ref, NewStatus: domain.WorkflowStatus(*in.Status), ExpectedVersion: ifMatch,
		}, actor, correlationID)
		if err != nil {
			return domain.Ticket{}, err
		}
		result, resultKnown = t, true
		ifMatch = t.Version // the status change bumped the version; a following field update must match the new one
	}

	fieldsRequested := in.Type != nil || in.Title != nil || in.Description != nil || in.Priority != nil || in.Severity != nil
	if fieldsRequested {
		base := result
		if !resultKnown {
			t, err := svc.GetTicket(ctx, ref)
			if err != nil {
				return domain.Ticket{}, err
			}
			base = t
			// ifMatch stays in.ExpectedVersion — the caller's own value,
			// never silently replaced by this read (product spec §8.4:
			// a stale version must surface as a conflict, not be quietly
			// bypassed).
		}
		severity := base.Severity
		if in.Severity != nil {
			s := domain.Severity(*in.Severity)
			severity = &s
		}
		req := service.UpdateTicketFieldsRequest{
			Ref: ref, Type: base.Type, Title: base.Title, Description: base.Description,
			Priority: base.Priority, Severity: severity, ExpectedVersion: ifMatch,
		}
		if in.Type != nil {
			req.Type = domain.TicketType(*in.Type)
		}
		if in.Title != nil {
			req.Title = *in.Title
		}
		if in.Description != nil {
			req.Description = *in.Description
		}
		if in.Priority != nil {
			req.Priority = domain.Priority(*in.Priority)
		}
		t, err := svc.UpdateTicketFields(ctx, req, actor, correlationID)
		if err != nil {
			return domain.Ticket{}, err
		}
		result, resultKnown = t, true
	}

	if !resultKnown {
		// Neither Status nor any field was set — nothing to do but
		// return the current state.
		return svc.GetTicket(ctx, ref)
	}
	return result, nil
}
