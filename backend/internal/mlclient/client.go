// Package mlclient содержит узкую Go-обёртку над gRPC-контрактом ML.
// Dashboard и feature pipeline не зависят от сгенерированных protobuf-типов.
package mlclient

import (
	"context"
	"fmt"
	"time"

	delayv1 "mt_predictor/internal/mlclient/gen"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TelemetryPoint — одна нормализованная строка NDTP для признаков модели.
type TelemetryPoint struct {
	TRID          int64
	EventTime     time.Time
	LocationValid bool
	Lon           *float64
	Lat           *float64
	Alt           *float64
	Speed         *float64
	Heading       *float64
}

// PredictionPoint описывает ровно одну цель текущего ML-контракта.
type PredictionPoint struct {
	SampleID       string
	TRID           int64
	PredictionTime time.Time
	TargetStopID   int64
	TargetTime     time.Time
	CurrentDelay   float64
}

// Predictor позволяет тестировать pipeline через локальную заглушку.
type Predictor interface {
	PredictBatch(context.Context, []PredictionPoint, []TelemetryPoint) (map[string]float64, error)
}

// Client держит одно переиспользуемое gRPC-соединение.
type Client struct {
	connection *grpc.ClientConn
	service    delayv1.DelayPredictionServiceClient
}

func New(address string) (*Client, error) {
	connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("создать gRPC-клиент ML: %w", err)
	}
	return &Client{connection: connection, service: delayv1.NewDelayPredictionServiceClient(connection)}, nil
}

func (c *Client) Close() error { return c.connection.Close() }

func (c *Client) PredictBatch(ctx context.Context, points []PredictionPoint, telemetry []TelemetryPoint) (map[string]float64, error) {
	request := &delayv1.BatchPredictRequest{
		Points:    make([]*delayv1.PredictionPoint, 0, len(points)),
		Telemetry: make([]*delayv1.TelemetryPoint, 0, len(telemetry)),
	}
	for _, point := range points {
		targetStopID := point.TargetStopID
		request.Points = append(request.Points, &delayv1.PredictionPoint{
			SampleId: point.SampleID, TrId: point.TRID,
			T: timestamppb.New(point.PredictionTime), TargetStopId: &targetStopID,
			TargetTimeBegin: timestamppb.New(point.TargetTime), CurDevS: point.CurrentDelay,
		})
	}
	for _, row := range telemetry {
		trID := row.TRID
		request.Telemetry = append(request.Telemetry, &delayv1.TelemetryPoint{
			EventTime: timestamppb.New(row.EventTime), LocationValid: row.LocationValid,
			Lon: row.Lon, Lat: row.Lat, Alt: row.Alt, Speed: row.Speed, Heading: row.Heading, TrId: &trID,
		})
	}

	response, err := c.service.PredictBatch(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("PredictBatch: %w", err)
	}
	result := make(map[string]float64, len(response.Predictions))
	for _, prediction := range response.Predictions {
		if prediction.Unit != "seconds" {
			return nil, fmt.Errorf("неподдерживаемая единица прогноза %q", prediction.Unit)
		}
		result[prediction.SampleId] = prediction.Prediction
	}
	return result, nil
}
