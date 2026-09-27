package incidentstore

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mt_predictor/models"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct{ pool *pgxpool.Pool }

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("создать пул PostgreSQL: %w", err)
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("подключиться к PostgreSQL: %w", err)
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		pool.Close()
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, readErr := migrations.ReadFile("migrations/" + entry.Name())
		if readErr != nil {
			pool.Close()
			return nil, readErr
		}
		if _, err = pool.Exec(ctx, string(data)); err != nil {
			pool.Close()
			return nil, fmt.Errorf("применить миграцию %s: %w", entry.Name(), err)
		}
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

func (s *Store) Save(ctx context.Context, i models.Incident) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO incidents (
id,unit_id,tr_id,route_pattern_id,occurrence_id,prediction_id,target_action_item_id,target_stop_id,target_stop_address,target_planned_at,
first_predicted_delay_seconds,current_delay_seconds,predicted_delay_seconds,prediction_time,status,actual_arrival_at,actual_delay_seconds,outcome,created_at,updated_at,
reason_code,reason,evidence,scenario_id)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)
ON CONFLICT (id) DO UPDATE SET prediction_id=EXCLUDED.prediction_id,predicted_delay_seconds=EXCLUDED.predicted_delay_seconds,
current_delay_seconds=EXCLUDED.current_delay_seconds,prediction_time=EXCLUDED.prediction_time,status=EXCLUDED.status,actual_arrival_at=EXCLUDED.actual_arrival_at,
actual_delay_seconds=EXCLUDED.actual_delay_seconds,outcome=EXCLUDED.outcome,updated_at=EXCLUDED.updated_at,
reason_code=EXCLUDED.reason_code,reason=EXCLUDED.reason,evidence=EXCLUDED.evidence,scenario_id=EXCLUDED.scenario_id`,
		i.ID, i.UnitID, i.TRID, i.RoutePatternID, i.OccurrenceID, i.PredictionID, i.TargetActionItemID, i.TargetStop.ID, i.TargetStop.Address,
		i.TargetPlannedAt, i.FirstPredictedDelaySeconds, i.CurrentDelaySeconds, i.PredictedDelaySeconds, i.PredictionTime, i.Status, i.ActualArrivalAt, i.ActualDelaySeconds, i.Outcome, i.CreatedAt, i.UpdatedAt,
		i.ReasonCode, i.Reason, nonNilEvidence(i.Evidence), i.ScenarioID)
	return err
}

func nonNilEvidence(value map[string]float64) map[string]float64 {
	if value == nil {
		return map[string]float64{}
	}
	return value
}

const columns = `id::text,unit_id,tr_id,route_pattern_id,occurrence_id,prediction_id,target_action_item_id,target_stop_id,target_stop_address,target_planned_at,first_predicted_delay_seconds,current_delay_seconds,predicted_delay_seconds,prediction_time,status,actual_arrival_at,actual_delay_seconds,outcome,created_at,updated_at,reason_code,reason,evidence,scenario_id`

type scanner interface{ Scan(...any) error }

func scanIncident(row scanner) (models.Incident, error) {
	var i models.Incident
	err := row.Scan(&i.ID, &i.UnitID, &i.TRID, &i.RoutePatternID, &i.OccurrenceID, &i.PredictionID, &i.TargetActionItemID, &i.TargetStop.ID, &i.TargetStop.Address,
		&i.TargetPlannedAt, &i.FirstPredictedDelaySeconds, &i.CurrentDelaySeconds, &i.PredictedDelaySeconds, &i.PredictionTime, &i.Status, &i.ActualArrivalAt, &i.ActualDelaySeconds, &i.Outcome, &i.CreatedAt, &i.UpdatedAt,
		&i.ReasonCode, &i.Reason, &i.Evidence, &i.ScenarioID)
	i.EventType = models.IncidentUpdated
	return i, err
}

func (s *Store) LoadUnresolved(ctx context.Context) ([]models.Incident, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+columns+` FROM incidents WHERE status IN ('active','awaiting_result') ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []models.Incident{}
	for rows.Next() {
		i, scanErr := scanIncident(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, i)
	}
	return result, rows.Err()
}

func (s *Store) Get(ctx context.Context, id string) (models.Incident, bool, error) {
	i, err := scanIncident(s.pool.QueryRow(ctx, `SELECT `+columns+` FROM incidents WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Incident{}, false, nil
	}
	return i, err == nil, err
}

func (s *Store) History(ctx context.Context, outcome *models.IncidentOutcome, limit, offset int) (models.IncidentHistoryPage, error) {
	where, args := "status='resolved'", []any{}
	if outcome != nil {
		where += " AND outcome=$1"
		args = append(args, *outcome)
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM incidents WHERE `+where, args...).Scan(&total); err != nil {
		return models.IncidentHistoryPage{}, err
	}
	args = append(args, limit, offset)
	limitPos, offsetPos := len(args)-1, len(args)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`SELECT %s FROM incidents WHERE %s ORDER BY updated_at DESC LIMIT $%d OFFSET $%d`, columns, where, limitPos, offsetPos), args...)
	if err != nil {
		return models.IncidentHistoryPage{}, err
	}
	defer rows.Close()
	items := []models.Incident{}
	for rows.Next() {
		i, scanErr := scanIncident(rows)
		if scanErr != nil {
			return models.IncidentHistoryPage{}, scanErr
		}
		items = append(items, i)
	}
	return models.IncidentHistoryPage{Items: items, Total: total, Limit: limit, Offset: offset}, rows.Err()
}
