// Команда mt-predictor-backend запускает единый контур рабочей карты:
// TCP-приёмник NDTP, map matching, HTTP-инициализацию и SSE live-поток.
package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"mt_predictor/catalog"
	"mt_predictor/dashboard"
	"mt_predictor/internal/incidentstore"
	"mt_predictor/internal/mlclient"
	"mt_predictor/internal/ndtp"
	"mt_predictor/models"
)

func main() {
	var (
		ndtpAddr            = flag.String("ndtp-addr", ":9201", "TCP-адрес приёмника NDTP")
		httpAddr            = flag.String("http-addr", ":8080", "HTTP-адрес API карты")
		catalogPath         = flag.String("catalog", "data/generated/route_catalog.json", "путь к каталогу маршрутов")
		telemetryTTL        = flag.Duration("telemetry-ttl", 30*time.Second, "время до перехода ТС в stale")
		mlAddr              = flag.String("ml-addr", "ml:50051", "адрес gRPC ML; пустое значение отключает прогнозы")
		mlTimeout           = flag.Duration("ml-timeout", 3*time.Second, "таймаут одного PredictBatch")
		predictEvery        = flag.Duration("prediction-interval", 5*time.Minute, "минимальный шаг прогнозов по event_time одного ТС")
		incidentAt          = flag.Float64("incident-delay-threshold", 120, "строгий порог создания инцидента, секунды")
		enableMockScenarios = flag.Bool("enable-mock-scenarios", false, "включить загрузку контролируемых demo-сценариев")
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(*ndtpAddr, *httpAddr, *catalogPath, *telemetryTTL, *mlAddr, *mlTimeout, *predictEvery, *incidentAt, *enableMockScenarios, logger); err != nil {
		logger.Error("backend остановлен", "error", err)
		os.Exit(1)
	}
}

func run(ndtpAddr, httpAddr, catalogPath string, telemetryTTL time.Duration, mlAddr string, mlTimeout, predictEvery time.Duration, incidentAt float64, enableMockScenarios bool, logger *slog.Logger) error {
	routeCatalog, err := catalog.Load(catalogPath)
	if err != nil {
		return err
	}
	catalogVersion, err := fileVersion(catalogPath)
	if err != nil {
		return err
	}
	initPayload := routeCatalog.DashboardInit(catalogVersion, models.RiskThresholds{
		WatchDelaySeconds: 180, HighDelaySeconds: 420,
	})
	runtime := dashboard.NewRuntime(catalog.NewMatcher(routeCatalog), telemetryTTL)
	if databaseURL := os.Getenv("DATABASE_URL"); databaseURL != "" {
		storeContext, cancelStore := context.WithTimeout(context.Background(), 10*time.Second)
		store, storeErr := incidentstore.Open(storeContext, databaseURL)
		cancelStore()
		if storeErr != nil {
			return storeErr
		}
		defer store.Close()
		if err := runtime.SetIncidentRepository(context.Background(), store); err != nil {
			return fmt.Errorf("восстановить инциденты: %w", err)
		}
	}
	var predictor mlclient.Predictor
	var mlConnection *mlclient.Client
	if mlAddr != "" {
		mlConnection, err = mlclient.New(mlAddr)
		if err != nil {
			return err
		}
		defer mlConnection.Close()
		predictor = mlConnection
	}
	processor := dashboard.NewProcessor(routeCatalog, runtime, predictor, dashboard.ProcessorOptions{
		PredictionEvery: predictEvery, PredictionTimeout: mlTimeout,
		IncidentThreshold: incidentAt, Logger: logger,
	})

	receiver := ndtp.NewServer(ndtp.Options{
		Addr: ndtpAddr,
		OnTelemetry: func(point ndtp.TelemetryPoint) {
			vehicle := processor.Apply(point)
			logger.Debug("телеметрия обработана", "unit_id", vehicle.UnitID, "match", vehicle.MatchStatus)
		},
		Logger: logger,
	})
	if err := receiver.Listen(); err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr: httpAddr, Handler: dashboard.NewAPI(initPayload, runtime, dashboard.APIOptions{EnableMockScenarios: enableMockScenarios, IncidentThreshold: incidentAt}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go processor.Run(ctx)

	errorsChannel := make(chan error, 2)
	go func() { errorsChannel <- receiver.Serve(ctx) }()
	go func() {
		logger.Info("HTTP API карты запущен", "addr", httpAddr, "catalog_version", catalogVersion)
		err := httpServer.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errorsChannel <- err
	}()

	select {
	case <-ctx.Done():
	case err := <-errorsChannel:
		if err != nil {
			stop()
			_ = receiver.Close()
			return err
		}
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownContext)
	_ = receiver.Close()
	return nil
}

func fileVersion(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("прочитать каталог для вычисления версии: %w", err)
	}
	referencePath := filepath.Join(filepath.Dir(path), "public_transport_reference.json")
	if reference, referenceErr := os.ReadFile(referencePath); referenceErr == nil {
		data = append(data, reference...)
	} else if !os.IsNotExist(referenceErr) {
		return "", fmt.Errorf("прочитать справочник транспорта для вычисления версии: %w", referenceErr)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum[:8]), nil
}
