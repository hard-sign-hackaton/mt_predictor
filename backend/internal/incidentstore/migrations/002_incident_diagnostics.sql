ALTER TABLE incidents ADD COLUMN IF NOT EXISTS reason_code text;
ALTER TABLE incidents ADD COLUMN IF NOT EXISTS reason text;
ALTER TABLE incidents ADD COLUMN IF NOT EXISTS evidence jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE incidents ADD COLUMN IF NOT EXISTS scenario_id text;
