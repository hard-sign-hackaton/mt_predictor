package dashboard

import (
	"context"

	"mt_predictor/models"
)

type IncidentRepository interface {
	Save(context.Context, models.Incident) error
	LoadUnresolved(context.Context) ([]models.Incident, error)
	Get(context.Context, string) (models.Incident, bool, error)
	History(context.Context, *models.IncidentOutcome, int, int) (models.IncidentHistoryPage, error)
}
