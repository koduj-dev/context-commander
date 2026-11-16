-- What internal/changes asks on every poll of an agent's inbox hook: the
-- step moves and handoffs of one project after a moment. Without these, each
-- poll scans the project's whole transition log and handoff list.
CREATE INDEX plan_item_transitions_project_at_idx ON plan_item_transitions (project_id, at DESC);
CREATE INDEX handoffs_project_created_idx ON handoffs (project_id, created_at DESC);
