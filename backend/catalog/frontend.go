package catalog

import "mt_predictor/models"

// DashboardInit преобразует внутренний сгенерированный каталог в статические
// данные для frontend. Метод не вычисляет маршруты заново, поэтому его можно
// вызывать один раз при установлении соединения dashboard с backend.
func (c *Catalog) DashboardInit(catalogVersion string, thresholds models.RiskThresholds) models.DashboardInit {
	result := models.DashboardInit{
		SchemaVersion:   "1",
		CatalogVersion:  catalogVersion,
		Stops:           make([]models.Stop, 0, len(c.Stops)),
		Routes:          make([]models.RoutePattern, 0, len(c.RoutePatterns)),
		Occurrences:     make([]models.RouteOccurrence, 0, len(c.Assignments)),
		VehicleBindings: make([]models.VehicleBinding, 0, len(c.VehicleBindings)),
		RiskThresholds:  thresholds,
	}

	for _, stop := range c.Stops {
		result.Stops = append(result.Stops, models.Stop{
			ID: stop.StopID, Position: models.GeoPoint{Lon: stop.Lon, Lat: stop.Lat}, Address: stop.Address,
		})
	}
	for _, pattern := range c.RoutePatterns {
		polyline := make([]models.GeoPoint, 0, len(pattern.Polyline))
		for _, point := range pattern.Polyline {
			polyline = append(polyline, models.GeoPoint{Lon: point[0], Lat: point[1]})
		}
		frequentPoints := make([]models.FrequentGeoPoint, 0, len(pattern.FrequentPoints))
		for _, point := range pattern.FrequentPoints {
			frequentPoints = append(frequentPoints, models.FrequentGeoPoint{
				Position: models.GeoPoint{Lon: point.Lon, Lat: point.Lat}, OccurrenceCount: point.OccurrenceCount,
			})
		}
		result.Routes = append(result.Routes, models.RoutePattern{
			ID:              pattern.RoutePatternID,
			StopIDs:         append([]string(nil), pattern.StopIDs...),
			Polyline:        polyline,
			FrequentPoints:  frequentPoints,
			GeometryQuality: models.GeometryQuality(pattern.GeometryQuality),
			Quality: models.RouteGeometryQuality{
				OccurrenceCount:        pattern.Quality.OccurrenceCount,
				GoodGPSOccurrenceCount: pattern.Quality.GoodGPSOccurrenceCount,
				BestStopCoverage:       pattern.Quality.BestStopCoverage,
				MedianStopToGPSMeters:  pattern.Quality.MedianStopToGPSMeters,
				MaxGPSJumpMeters:       pattern.Quality.MaxGPSJumpMeters,
			},
		})
	}
	for _, assignment := range c.Assignments {
		calls := make([]models.StopCall, 0, len(assignment.Events))
		for _, event := range assignment.Events {
			calls = append(calls, models.StopCall{
				ActionItemID: event.ActionItemID, StopID: event.StopID, PlannedAt: event.PlannedAt,
			})
		}
		result.Occurrences = append(result.Occurrences, models.RouteOccurrence{
			ID: assignment.OccurrenceID, TRID: assignment.TRID, RoutePatternID: assignment.RoutePatternID,
			StartsAt: assignment.ValidFrom, EndsAt: assignment.ValidTo, Calls: calls,
		})
	}
	for _, binding := range c.VehicleBindings {
		result.VehicleBindings = append(result.VehicleBindings, models.VehicleBinding{
			UnitID: binding.UnitID, TRID: binding.TRID, HasSchedule: binding.HasSchedule, Synthetic: binding.Synthetic,
		})
	}
	return result
}

// VehicleState преобразует один декодированный пакет телеметрии и результат
// сопоставления в потоковую модель frontend. Метод не добавляет сведений,
// которых нет в NDTP или сгенерированном каталоге.
func VehicleState(telemetry Telemetry, match MatchResult, freshness models.TelemetryFreshness) models.VehicleState {
	result := models.VehicleState{
		UnitID:         telemetry.UnitID,
		SpeedKmh:       telemetry.Speed,
		HeadingDegrees: telemetry.Heading,
		EventTime:      telemetry.EventTime,
		ReceivedAt:     telemetry.ReceiveTime,
		Historical:     telemetry.Historical,
		Freshness:      freshness,
		MatchStatus:    models.MatchStatus(match.Status),
	}
	if telemetry.Valid {
		result.Position = &models.GeoPoint{Lon: telemetry.Lon, Lat: telemetry.Lat}
	}
	if match.Status != MatchUnmappedUnit {
		result.TRID = pointer(match.TRID)
	}
	if match.RoutePatternID != "" {
		result.RoutePatternID = pointer(match.RoutePatternID)
		result.DistanceToRouteMeters = pointer(match.DistanceToRouteM)
	}
	if match.OccurrenceID != "" {
		result.OccurrenceID = pointer(match.OccurrenceID)
	}
	if match.PreviousStopID != "" {
		result.PreviousStop = &models.StopReference{ID: match.PreviousStopID, Address: match.PreviousStopAddress}
	}
	if match.NextStopID != "" {
		result.NextStop = &models.StopReference{ID: match.NextStopID, Address: match.NextStopAddress}
		result.NextActionItemID = pointer(match.NextActionItemID)
	}
	if match.GeometryQuality != "" {
		result.GeometryQuality = pointer(models.GeometryQuality(match.GeometryQuality))
	}
	return result
}

func pointer[T any](value T) *T { return &value }
