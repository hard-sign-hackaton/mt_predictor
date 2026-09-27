UPDATE incidents SET reason_code = '' WHERE reason_code IS NULL;
UPDATE incidents SET evidence = '{}'::jsonb WHERE evidence IS NULL;
UPDATE incidents SET scenario_id = '' WHERE scenario_id IS NULL;

ALTER TABLE incidents ALTER COLUMN reason_code SET DEFAULT '';
ALTER TABLE incidents ALTER COLUMN reason_code SET NOT NULL;
ALTER TABLE incidents ALTER COLUMN evidence SET DEFAULT '{}'::jsonb;
ALTER TABLE incidents ALTER COLUMN evidence SET NOT NULL;
ALTER TABLE incidents ALTER COLUMN scenario_id SET DEFAULT '';
ALTER TABLE incidents ALTER COLUMN scenario_id SET NOT NULL;
