package dashboard

import (
	"context"

	"mt_predictor/models"
)

// IncidentRepository — хранилище инцидентов и действий оператора.
// Контракт намеренно повторяет операции incidentstore.Store, чтобы dashboard
// не зависел от PostgreSQL и проверялся на подставном хранилище.
type IncidentRepository interface {
	Save(context.Context, models.Incident) error
	LoadUnresolved(context.Context) ([]models.Incident, error)
	Get(context.Context, string) (models.Incident, bool, error)
	History(context.Context, *models.IncidentOutcome, int, int) (models.IncidentHistoryPage, error)
	SaveAction(context.Context, models.OperatorAction) error
	Actions(context.Context, string) ([]models.OperatorAction, error)
}
