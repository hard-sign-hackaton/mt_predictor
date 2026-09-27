CREATE TABLE IF NOT EXISTS operator_actions (
    id uuid PRIMARY KEY,
    incident_id uuid NOT NULL REFERENCES incidents(id),
    unit_id bigint NOT NULL,
    route_pattern_id text NOT NULL,
    action_code text NOT NULL,
    label text NOT NULL,
    recipient text NOT NULL CHECK (recipient IN ('driver', 'dispatch_hq')),
    message text NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'consumed')),
    created_at timestamptz NOT NULL,
    consumed_at timestamptz
);

CREATE INDEX IF NOT EXISTS operator_actions_incident_idx
    ON operator_actions (incident_id, created_at DESC);

CREATE INDEX IF NOT EXISTS operator_actions_outbox_idx
    ON operator_actions (created_at)
    WHERE status = 'pending';
