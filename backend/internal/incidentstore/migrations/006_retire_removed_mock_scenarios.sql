UPDATE incidents
SET status = 'cancelled', updated_at = now()
WHERE scenario_id IN ('gps-quality-01', 'normal-control-01')
  AND status IN ('active', 'awaiting_result');
