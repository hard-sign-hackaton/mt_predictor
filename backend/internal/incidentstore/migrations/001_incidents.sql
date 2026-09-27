CREATE TABLE IF NOT EXISTS incidents (
    id uuid PRIMARY KEY,
    unit_id bigint NOT NULL,
    tr_id bigint NOT NULL,
    route_pattern_id text NOT NULL,
    occurrence_id text NOT NULL,
    prediction_id text NOT NULL,
    target_action_item_id bigint NOT NULL,
    target_stop_id text NOT NULL,
    target_stop_address text NOT NULL,
    target_planned_at timestamptz NOT NULL,
    first_predicted_delay_seconds double precision NOT NULL,
    predicted_delay_seconds double precision NOT NULL,
    prediction_time timestamptz NOT NULL,
    status text NOT NULL CHECK (status IN ('active', 'awaiting_result', 'resolved', 'cancelled')),
    actual_arrival_at timestamptz,
    actual_delay_seconds double precision,
    outcome text CHECK (outcome IN ('occurred', 'not_occurred')),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS incidents_unresolved_idx
    ON incidents (unit_id, target_action_item_id)
    WHERE status IN ('active', 'awaiting_result');

CREATE INDEX IF NOT EXISTS incidents_history_idx
    ON incidents (updated_at DESC)
    WHERE status = 'resolved';
