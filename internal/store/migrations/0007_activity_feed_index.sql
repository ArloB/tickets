-- Phase 5 Step 1: activity feed (product spec §5.10, §6.5). The feed
-- reads audit_events newest-first across a whole project rather than
-- one entity's own trail (idx_audit_events_entity/idx_audit_events_actor
-- don't cover that access pattern), so add the plain ordering index the
-- feed's ORDER BY created_at DESC, id DESC needs. Added proactively
-- given the product spec §11 reference dataset (100,000 tickets /
-- 500,000 comments implies a comparable audit_events volume), not after
-- a benchmark complains.
CREATE INDEX idx_audit_events_created_at ON audit_events(created_at, id);
