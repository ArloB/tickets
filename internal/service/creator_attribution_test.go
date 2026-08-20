package service

import (
	"context"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

// TestCreatorAttributionAcrossActors is Step 9's gate test (see the
// Phase 2 plan's Step 9 section): two different actors creating
// tickets in the same project each get their own Creator recorded on
// the response, proving the entities.created_by join added to
// queries.go's ticketSelectColumns resolves per-row rather than
// picking up whatever actor happened to be in scope elsewhere.
func TestCreatorAttributionAcrossActors(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	agent, err := s.CreateAgent(ctx, CreateAgentRequest{Name: "codex"}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}

	humanTicket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "Created by a human"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateTicket(human): %v", err)
	}
	if humanTicket.Creator == nil || *humanTicket.Creator != testActor {
		t.Errorf("human-created ticket Creator = %v, want %v", humanTicket.Creator, testActor)
	}

	agentTicket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "Created by an agent"}, agent.Ref, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateTicket(agent): %v", err)
	}
	if agentTicket.Creator == nil || *agentTicket.Creator != agent.Ref {
		t.Errorf("agent-created ticket Creator = %v, want %v", agentTicket.Creator, agent.Ref)
	}

	// GetTicket re-fetches from the store on a plain read path (not the
	// create response's own reload) — confirms the join isn't an
	// artifact of the row still being fresh in the same transaction.
	ref, err := domain.Parse(humanTicket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}
	reGot, err := s.GetTicket(ctx, ref)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if reGot.Creator == nil || *reGot.Creator != testActor {
		t.Errorf("GetTicket Creator = %v, want %v", reGot.Creator, testActor)
	}
}

// TestCreatorAttributionForFeatureAndProject confirms the Creator
// field Step 9 added to Feature and Project (alongside Ticket's, "for
// consistency" per the plan) is actually wired through the same
// entities.created_by join, not just declared on the struct with
// nothing populating it.
func TestCreatorAttributionForFeatureAndProject(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	proj, err := s.CreateProject(ctx, CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if proj.Creator == nil || *proj.Creator != testActor {
		t.Errorf("CreateProject Creator = %v, want %v", proj.Creator, testActor)
	}

	agent, err := s.CreateAgent(ctx, CreateAgentRequest{Name: "codex"}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	feature, err := s.CreateFeature(ctx, CreateFeatureRequest{ProjectKey: "ABC", Title: "Payments", Priority: domain.PriorityMedium}, agent.Ref, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	if feature.Creator == nil || *feature.Creator != agent.Ref {
		t.Errorf("CreateFeature Creator = %v, want %v", feature.Creator, agent.Ref)
	}

	// The project's own mandatory General feature (ADR 0001) is created
	// in the same transaction as the project, by the same actor — not
	// the system actor, and not whoever creates a later feature in the
	// project.
	generalRef := domain.Reference{ProjectKey: "ABC", Kind: domain.KindFeature, Seq: 1}
	general, err := s.GetFeature(ctx, generalRef)
	if err != nil {
		t.Fatalf("GetFeature(General): %v", err)
	}
	if general.Creator == nil || *general.Creator != testActor {
		t.Errorf("General feature Creator = %v, want %v (the project's creator)", general.Creator, testActor)
	}
}
