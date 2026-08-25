// Package fixtures generates deterministic, seeded test/benchmark data
// directly through internal/store's typed statements — never through
// internal/service. Routing hundreds of thousands of rows through the
// service layer (idempotency checks, mention rescanning, an audit
// event per call inside its own resolved transaction) would take
// hours for the Full scale; this package writes rows in large batched
// transactions instead, while still producing real audit_events rows
// so audit-trail-heavy queries are benchmarked against realistic
// volume (Phase 1 plan, Step 6).
//
// Same seed, same Scale ⇒ same project keys, same ticket/feature
// references, same timestamps, and the same resulting sort order —
// every source of randomness here is a math/rand.Rand seeded by the
// caller, not time.Now() or crypto/rand. The one exception is entity
// UUIDs (store.InsertEntity calls uuid.NewV7(), which uses
// crypto/rand internally) — the plan's determinism requirement is
// scoped to "references, timestamps, and ordering," none of which
// depend on the UUID value, so this doesn't need fixing.
package fixtures

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"time"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// Scale controls how much data Generate creates.
type Scale struct {
	Projects            int
	FeaturesPerProject  int // includes the mandatory General feature
	TicketsPerProject   int
	CommentsPerTicket   int
	DecisionsPerProject int
	PlansPerProject     int
	DocumentsPerProject int
}

// Small is the default scale — small enough to build in well under a
// second, large enough to exercise pagination and priority-group
// variety. Used by this package's own tests, which run as part of the
// normal `go test ./...` suite (the Phase 1 plan's "small scale in
// the normal test suite" requirement). DecisionsPerProject/
// PlansPerProject/DocumentsPerProject are small but non-zero so the
// normal test suite exercises that code path too, not just Full.
var Small = Scale{
	Projects: 3, FeaturesPerProject: 4, TicketsPerProject: 60, CommentsPerTicket: 1,
	DecisionsPerProject: 2, PlansPerProject: 2, DocumentsPerProject: 2,
}

// Full is product spec §11's reference performance dataset: 25
// projects, 100,000 tickets, 500,000 comments, and 10,000
// decisions/plans/documents combined (Projects*TicketsPerProject and
// *CommentsPerTicket multiply out exactly; 25 * (134+133+133) = 10,000
// decisions/plans/documents). Only `task bench` builds this scale.
var Full = Scale{
	Projects: 25, FeaturesPerProject: 20, TicketsPerProject: 4000, CommentsPerTicket: 5,
	DecisionsPerProject: 134, PlansPerProject: 133, DocumentsPerProject: 133,
}

// batchSize is how many tickets' worth of work (the ticket plus its
// comments and audit events) lands in one transaction. Large enough
// to amortize transaction overhead across Full's 100k tickets, small
// enough that a single transaction doesn't hold SQLite's write lock
// for the entire multi-hundred-thousand-row run.
const batchSize = 500

// baseTime anchors every generated row's created_at — fixed, not
// time.Now(), so two runs with the same seed produce identical
// timestamps. Rows are stamped in generation order with a fixed
// stride, which is also their true creation order, so every ordering
// invariant (cursor pagination's (created_at, id) tie-break included)
// holds without needing wall-clock precision.
var baseTime = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

const timeStride = 137 * time.Millisecond

var (
	ticketTypes = []domain.TicketType{domain.TicketTypeTask, domain.TicketTypeBug, domain.TicketTypeSecurity, domain.TicketTypeChore}
	priorities  = []domain.Priority{domain.PriorityCritical, domain.PriorityHigh, domain.PriorityMedium, domain.PriorityLow}
	severities  = []domain.Severity{domain.SeverityCritical, domain.SeverityHigh, domain.SeverityMedium, domain.SeverityLow}
	statuses    = []domain.WorkflowStatus{
		domain.WorkflowStatusBacklog, domain.WorkflowStatusReady, domain.WorkflowStatusInProgress,
		domain.WorkflowStatusBlocked, domain.WorkflowStatusReview, domain.WorkflowStatusDone, domain.WorkflowStatusCancelled,
	}
)

// Summary describes what Generate built — enough for a caller (a
// benchmark, a test) to address specific records without re-deriving
// the generator's internal numbering scheme.
type Summary struct {
	ProjectKeys []string
	// SampleProjectKey/SampleTicketRef point at a record roughly in
	// the middle of the dataset, not the very first row — an indexed
	// lookup benchmark shouldn't accidentally be measuring a
	// first-page/first-row special case.
	SampleProjectKey string
	SampleTicketRef  string
	TicketCount      int
	CommentCount     int
	DecisionCount    int
	PlanCount        int
	DocumentCount    int
}

// clock is a monotonically advancing, deterministic timestamp source
// shared across one Generate call.
type clock struct{ t time.Time }

func newClock() *clock { return &clock{t: baseTime} }

func (c *clock) next() string {
	s := c.t.UTC().Format(store.TimeLayout)
	c.t = c.t.Add(timeStride)
	return s
}

// groupPositions tracks the next tail position for each (project
// index, priority) group, so Generate can compute
// domain.TailPosition without a database round trip per row — it
// already knows every group's current max because it wrote the
// previous member itself.
type groupPositions map[[2]any]int64

func (g groupPositions) tail(projectIdx int, priority domain.Priority) int64 {
	key := [2]any{projectIdx, priority}
	pos := domain.TailPosition(g[key])
	g[key] = pos
	return pos
}

// Generate builds scale's dataset inside st, attributed to the seeded
// system actor (ADR 0012), and returns a Summary describing it.
// Projects/features are created first in one small transaction (cheap
// even at Full scale — a few hundred rows); tickets and comments are
// then created in batchSize-ticket chunks, each its own transaction.
func Generate(ctx context.Context, st *store.Store, seed int64, scale Scale) (Summary, error) {
	rng := rand.New(rand.NewSource(seed))
	clk := newClock()
	db := st.DB()

	sysID, err := store.GetActorIDByRef(ctx, db, domain.ActorSystem, "system")
	if err != nil {
		return Summary{}, fmt.Errorf("fixtures: resolve system actor: %w", err)
	}
	localID, err := store.GetActorIDByRef(ctx, db, domain.ActorHuman, "local")
	if err != nil {
		return Summary{}, fmt.Errorf("fixtures: resolve local actor: %w", err)
	}

	projects, err := createProjects(ctx, db, sysID, clk, scale)
	if err != nil {
		return Summary{}, err
	}

	positions := make(groupPositions)
	summary := Summary{TicketCount: scale.Projects * scale.TicketsPerProject}

	var (
		batch      []ticketPlan
		flushCount int
	)
	for projectIdx, p := range projects {
		summary.ProjectKeys = append(summary.ProjectKeys, p.key)
		for i := 0; i < scale.TicketsPerProject; i++ {
			batch = append(batch, planTicket(rng, p, projectIdx, i))
			if len(batch) >= batchSize {
				if err := flushTicketBatch(ctx, db, sysID, localID, clk, positions, batch, scale.CommentsPerTicket, rng); err != nil {
					return Summary{}, err
				}
				flushCount += len(batch)
				batch = batch[:0]
			}
		}
	}
	if len(batch) > 0 {
		if err := flushTicketBatch(ctx, db, sysID, localID, clk, positions, batch, scale.CommentsPerTicket, rng); err != nil {
			return Summary{}, err
		}
		flushCount += len(batch)
	}
	summary.CommentCount = flushCount * scale.CommentsPerTicket

	if err := generateContentItems(ctx, db, sysID, clk, projects, scale, &summary); err != nil {
		return Summary{}, err
	}

	if len(projects) > 0 {
		mid := projects[len(projects)/2]
		summary.SampleProjectKey = mid.key
		midSeq := scale.TicketsPerProject/2 + 1
		ref, err := domain.Format(domain.Reference{ProjectKey: mid.key, Kind: domain.KindTicket, Seq: int64(midSeq)})
		if err != nil {
			return Summary{}, fmt.Errorf("fixtures: format sample ticket ref: %w", err)
		}
		summary.SampleTicketRef = ref
	}

	return summary, nil
}

type projectPlan struct {
	key              string
	entityID         int64
	generalFeatureID int64
	// featureIDs holds every feature (including General, first) so
	// tickets can be scattered across all of a project's features.
	featureIDs []int64
}

func createProjects(ctx context.Context, db *sql.DB, sysID int64, clk *clock, scale Scale) ([]projectPlan, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("fixtures: begin projects tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	plans := make([]projectPlan, 0, scale.Projects)
	for i := 0; i < scale.Projects; i++ {
		now := clk.next()
		key := fmt.Sprintf("FX%03d", i+1)
		title := fmt.Sprintf("Fixture Project %d", i+1)

		projEntityID, _, err := store.InsertEntity(ctx, tx, nil, domain.KindProject, sysID, now)
		if err != nil {
			return nil, fmt.Errorf("fixtures: insert project entity: %w", err)
		}
		if err := store.InsertProject(ctx, tx, projEntityID, key, title, "Generated by internal/fixtures for benchmarking."); err != nil {
			return nil, fmt.Errorf("fixtures: insert project: %w", err)
		}
		if err := store.InsertAuditEvent(ctx, tx, projEntityID, sysID, "project_created", "fixtures", nil, `{"fixtures":true}`, now); err != nil {
			return nil, fmt.Errorf("fixtures: insert project audit event: %w", err)
		}

		plan := projectPlan{key: key, entityID: projEntityID}
		for f := 0; f < scale.FeaturesPerProject; f++ {
			fnow := clk.next()
			title := "General"
			priority := domain.PriorityMedium
			if f > 0 {
				title = fmt.Sprintf("Fixture Feature %d", f+1)
				priority = priorities[f%len(priorities)]
			}
			featureEntityID, _, err := store.InsertEntity(ctx, tx, &projEntityID, domain.KindFeature, sysID, fnow)
			if err != nil {
				return nil, fmt.Errorf("fixtures: insert feature entity: %w", err)
			}
			seq, err := store.AllocateReference(ctx, tx, projEntityID, domain.KindFeature)
			if err != nil {
				return nil, fmt.Errorf("fixtures: allocate feature reference: %w", err)
			}
			if err := store.InsertFeature(ctx, tx, featureEntityID, projEntityID, seq, title, "", string(priority), domain.TailPosition(0)); err != nil {
				return nil, fmt.Errorf("fixtures: insert feature: %w", err)
			}
			if f == 0 {
				plan.generalFeatureID = featureEntityID
				if err := store.SetProjectGeneralFeature(ctx, tx, projEntityID, featureEntityID); err != nil {
					return nil, fmt.Errorf("fixtures: set general feature: %w", err)
				}
			}
			plan.featureIDs = append(plan.featureIDs, featureEntityID)
		}
		plans = append(plans, plan)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("fixtures: commit projects tx: %w", err)
	}
	committed = true
	return plans, nil
}

// ticketPlan is one ticket's chosen shape, decided before any I/O so
// flushTicketBatch's transaction body is pure writes.
type ticketPlan struct {
	project    projectPlan
	projectIdx int
	seq        int
	ticket     domain.TicketType
	priority   domain.Priority
	severity   *domain.Severity
	status     domain.WorkflowStatus
	featureI   int
	assigned   bool
}

func planTicket(rng *rand.Rand, p projectPlan, projectIdx, i int) ticketPlan {
	tp := ticketPlan{
		project:    p,
		projectIdx: projectIdx,
		seq:        i,
		ticket:     ticketTypes[rng.Intn(len(ticketTypes))],
		priority:   priorities[rng.Intn(len(priorities))],
		status:     statuses[rng.Intn(len(statuses))],
		featureI:   rng.Intn(len(p.featureIDs)),
		assigned:   rng.Intn(4) == 0, // ~25% assigned, matching a realistic minority
	}
	if tp.ticket == domain.TicketTypeBug || tp.ticket == domain.TicketTypeSecurity {
		sev := severities[rng.Intn(len(severities))]
		tp.severity = &sev
	}
	return tp
}

func flushTicketBatch(ctx context.Context, db *sql.DB, sysID, localID int64, clk *clock, positions groupPositions, batch []ticketPlan, commentsPerTicket int, rng *rand.Rand) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("fixtures: begin ticket batch tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	for batchIdx, tp := range batch {
		now := clk.next()
		seq, err := store.AllocateReference(ctx, tx, tp.project.entityID, domain.KindTicket)
		if err != nil {
			return fmt.Errorf("fixtures: allocate ticket reference: %w", err)
		}
		ticketEntityID, _, err := store.InsertEntity(ctx, tx, &tp.project.entityID, domain.KindTicket, sysID, now)
		if err != nil {
			return fmt.Errorf("fixtures: insert ticket entity: %w", err)
		}

		var severityStr *string
		if tp.severity != nil {
			v := string(*tp.severity)
			severityStr = &v
		}
		pos := positions.tail(tp.projectIdx, tp.priority)
		title := fmt.Sprintf("Fixture ticket %d for %s", tp.seq+1, tp.project.key)
		if err := store.InsertTicket(ctx, tx, ticketEntityID, tp.project.entityID, tp.project.featureIDs[tp.featureI], seq,
			string(tp.ticket), title, fixtureDescription(tp.seq), string(tp.status), string(tp.priority), severityStr, pos); err != nil {
			return fmt.Errorf("fixtures: insert ticket: %w", err)
		}
		if tp.assigned {
			if _, err := tx.ExecContext(ctx, `UPDATE tickets SET assignee_id = ? WHERE id = ?`, localID, ticketEntityID); err != nil {
				return fmt.Errorf("fixtures: assign ticket: %w", err)
			}
		}
		if err := store.InsertAuditEvent(ctx, tx, ticketEntityID, sysID, "ticket_created", "fixtures", nil, `{"fixtures":true}`, now); err != nil {
			return fmt.Errorf("fixtures: insert ticket audit event: %w", err)
		}

		for c := 0; c < commentsPerTicket; c++ {
			cnow := clk.next()
			authorID := sysID
			if (batchIdx+c)%3 == 0 {
				authorID = localID
			}
			body := fmt.Sprintf("Fixture comment %d on ticket %d: %s", c+1, tp.seq+1, fixtureCommentBody(rng))
			commentID, err := store.InsertComment(ctx, tx, ticketEntityID, authorID, body, cnow)
			if err != nil {
				return fmt.Errorf("fixtures: insert comment: %w", err)
			}
			cid := commentID
			if err := store.InsertAuditEvent(ctx, tx, ticketEntityID, authorID, "comment_added", "fixtures", &cid, `{"fixtures":true}`, cnow); err != nil {
				return fmt.Errorf("fixtures: insert comment audit event: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("fixtures: commit ticket batch tx: %w", err)
	}
	committed = true
	return nil
}

// contentPlan is one decision/plan/document's chosen shape, decided
// before any I/O — mirrors ticketPlan's split between planning and
// writing.
type contentPlan struct {
	project projectPlan
	kind    domain.EntityKind // KindDecision, KindPlan, or KindDocument
	seq     int               // 1-based position within this project+kind, for title text only
}

// contentBatchSize mirrors batchSize but is smaller — decisions/plans/
// documents have no per-row comments, so each row is cheaper and a
// larger chunk doesn't risk holding the write lock disproportionately
// long relative to a ticket batch.
const contentBatchSize = 1000

// generateContentItems builds every project's decisions, plans, and
// documents (product spec §11's reference dataset also names these) in
// contentBatchSize-row transactions, mirroring the ticket batch loop
// above. Decisions alternate accepted/proposed so a fixture-backed
// benchmark or manual check has both statuses to filter on.
func generateContentItems(ctx context.Context, db *sql.DB, sysID int64, clk *clock, projects []projectPlan, scale Scale, summary *Summary) error {
	var batch []contentPlan
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := flushContentBatch(ctx, db, sysID, clk, batch); err != nil {
			return err
		}
		for _, cp := range batch {
			switch cp.kind {
			case domain.KindDecision:
				summary.DecisionCount++
			case domain.KindPlan:
				summary.PlanCount++
			case domain.KindDocument:
				summary.DocumentCount++
			}
		}
		batch = batch[:0]
		return nil
	}

	for _, p := range projects {
		for i := 0; i < scale.DecisionsPerProject; i++ {
			batch = append(batch, contentPlan{project: p, kind: domain.KindDecision, seq: i + 1})
			if len(batch) >= contentBatchSize {
				if err := flush(); err != nil {
					return err
				}
			}
		}
		for i := 0; i < scale.PlansPerProject; i++ {
			batch = append(batch, contentPlan{project: p, kind: domain.KindPlan, seq: i + 1})
			if len(batch) >= contentBatchSize {
				if err := flush(); err != nil {
					return err
				}
			}
		}
		for i := 0; i < scale.DocumentsPerProject; i++ {
			batch = append(batch, contentPlan{project: p, kind: domain.KindDocument, seq: i + 1})
			if len(batch) >= contentBatchSize {
				if err := flush(); err != nil {
					return err
				}
			}
		}
	}
	return flush()
}

func flushContentBatch(ctx context.Context, db *sql.DB, sysID int64, clk *clock, batch []contentPlan) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("fixtures: begin content batch tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	for _, cp := range batch {
		now := clk.next()
		entityID, _, err := store.InsertEntity(ctx, tx, &cp.project.entityID, cp.kind, sysID, now)
		if err != nil {
			return fmt.Errorf("fixtures: insert %s entity: %w", cp.kind, err)
		}
		seq, err := store.AllocateReference(ctx, tx, cp.project.entityID, cp.kind)
		if err != nil {
			return fmt.Errorf("fixtures: allocate %s reference: %w", cp.kind, err)
		}

		if cp.kind == domain.KindDecision {
			status := "accepted"
			if cp.seq%2 == 0 {
				status = "proposed"
			}
			title := fmt.Sprintf("Fixture decision %d for %s", cp.seq, cp.project.key)
			if err := store.InsertDecision(ctx, tx, entityID, cp.project.entityID, seq, title,
				fixtureDescription(cp.seq), "do the thing", "because fixtures", "generated by internal/fixtures", status); err != nil {
				return fmt.Errorf("fixtures: insert decision: %w", err)
			}
			continue
		}

		title := fmt.Sprintf("Fixture %s %d for %s", cp.kind, cp.seq, cp.project.key)
		if err := store.InsertContentItem(ctx, tx, entityID, cp.project.entityID, cp.kind, seq, title, store.ContentItemFields{
			Representation: domain.ContentRepresentationMarkdown,
			Body:           fixtureDescription(cp.seq),
		}); err != nil {
			return fmt.Errorf("fixtures: insert %s: %w", cp.kind, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("fixtures: commit content batch tx: %w", err)
	}
	committed = true
	return nil
}

func fixtureDescription(seq int) string {
	return fmt.Sprintf("Generated description for fixture ticket %d. Lorem ipsum dolor sit amet, consectetur adipiscing elit.", seq+1)
}

var commentSnippets = []string{
	"Looks good to me.",
	"Can we get a repro on this?",
	"Pushed a fix, please verify.",
	"Still investigating.",
	"Closing as duplicate.",
	"Needs more information from the reporter.",
	"This is blocked on another team.",
	"Verified in staging.",
}

func fixtureCommentBody(rng *rand.Rand) string {
	return commentSnippets[rng.Intn(len(commentSnippets))]
}
