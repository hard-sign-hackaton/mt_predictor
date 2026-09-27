package models

import "time"

// GeoPoint — географическая координата WGS84. Экранные x/y не передаются:
// проекцию выполняет картографический компонент frontend.
type GeoPoint struct {
	// Lon — долгота в десятичных градусах, допустимый диапазон [-180, 180].
	Lon float64 `json:"lon"`
	// Lat — широта в десятичных градусах, допустимый диапазон [-90, 90].
	Lat float64 `json:"lat"`
}

// GeometryQuality показывает, как была получена линия маршрута.
type GeometryQuality string

const (
	// GeometryGPSRepeated — линия подтверждена несколькими полными GPS-проходами.
	GeometryGPSRepeated GeometryQuality = "gps_repeated"
	// GeometryGPSSingle — линия подтверждена одним достаточно полным GPS-проходом.
	GeometryGPSSingle GeometryQuality = "gps_single"
	// GeometryStopsOnly — линия построена только по упорядоченным остановкам.
	GeometryStopsOnly GeometryQuality = "stops_only"
)

// DashboardInit — статические справочные данные. Они загружаются при запуске и
// повторно запрашиваются только при изменении CatalogVersion.
type DashboardInit struct {
	// SchemaVersion определяет версию JSON-контракта.
	SchemaVersion string `json:"schemaVersion"`
	// CatalogVersion меняется при изменении расписаний или вычисленной геометрии.
	CatalogVersion string `json:"catalogVersion"`
	// Stops содержит дедуплицированные физические остановочные точки.
	Stops []Stop `json:"stops"`
	// Routes содержит вычисленные паттерны, а не официальные номера маршрутов.
	Routes []RoutePattern `json:"routes"`
	// Occurrences содержит рейсы с датами для выбора активного паттерна.
	Occurrences []RouteOccurrence `json:"occurrences"`
	// VehicleBindings связывает NDTP unit_id с tr_id расписания и ML.
	VehicleBindings []VehicleBinding `json:"vehicleBindings"`
	// RiskThresholds задаёт пороги перевода задержки в критичность на frontend.
	RiskThresholds RiskThresholds `json:"riskThresholds"`
}

// Stop — стабильная справочная остановка. Она отделена от StopCall, поскольку
// одной остановке соответствует много плановых событий прибытия.
type Stop struct {
	// ID детерминированно вычисляется из округлённых координат WGS84.
	ID string `json:"id"`
	// Position — координата остановки, отображаемая на карте.
	Position GeoPoint `json:"position"`
	// Address берётся из building_address и может быть пустым.
	Address string `json:"address"`
}

// FrequentGeoPoint — повторяющаяся GPS-ячейка размером 25 метров.
type FrequentGeoPoint struct {
	// Position — медианная наблюдаемая координата внутри ячейки.
	Position GeoPoint `json:"position"`
	// OccurrenceCount — число различных проходов через ячейку.
	OccurrenceCount int `json:"occurrenceCount"`
}

// RouteGeometryQuality содержит показатели качества вычисленной линии маршрута.
type RouteGeometryQuality struct {
	// OccurrenceCount — число рейсов расписания, отнесённых к паттерну.
	OccurrenceCount int `json:"occurrenceCount"`
	// GoodGPSOccurrenceCount — число рейсов с пригодным GPS-покрытием.
	GoodGPSOccurrenceCount int `json:"goodGpsOccurrenceCount"`
	// BestStopCoverage — доля остановок лучшего прохода в диапазоне 0..1.
	BestStopCoverage float64 `json:"bestStopCoverage"`
	// MedianStopToGPSMeters отсутствует, если пригодного GPS-прохода нет.
	MedianStopToGPSMeters *float64 `json:"medianStopToGpsMeters,omitempty"`
	// MaxGPSJumpMeters — максимальный разрыв между соседними точками выбранного прохода.
	MaxGPSJumpMeters float64 `json:"maxGpsJumpMeters"`
}

// RoutePattern — повторяющийся упорядоченный путь, вычисленный по расписанию и
// GPS. Его ID нельзя показывать как официальный номер московского маршрута.
type RoutePattern struct {
	// ID — детерминированный хеш канонической последовательности остановок.
	ID string `json:"id"`
	// OfficialRouteID отсутствует до подключения внешнего справочника маршрутов.
	OfficialRouteID *string `json:"officialRouteId,omitempty"`
	// StopIDs описывает один канонический круг без повторных дневных проходов.
	StopIDs []string `json:"stopIds"`
	// Polyline — упрощённый GPS-проход либо fallback-линия только по остановкам.
	Polyline []GeoPoint `json:"polyline"`
	// FrequentPoints содержит повторяющиеся GPS-точки для диагностики.
	FrequentPoints []FrequentGeoPoint `json:"frequentPoints"`
	// GeometryQuality сообщает UI степень достоверности линии.
	GeometryQuality GeometryQuality `json:"geometryQuality"`
	// Quality содержит числовые показатели качества геометрии.
	Quality RouteGeometryQuality `json:"quality"`
}

// StopCall — одно плановое прибытие. ActionItemID соответствует target_stop_id
// в предоставленных точках прогнозирования.
type StopCall struct {
	// ActionItemID берётся из schedule*.csv.tt_action_item_id.
	ActionItemID int64 `json:"actionItemId"`
	// StopID связывает событие со справочником DashboardInit.Stops.
	StopID string `json:"stopId"`
	// PlannedAt — schedule*.csv.time_begin, интерпретированный как UTC.
	PlannedAt time.Time `json:"plannedAt"`
}

// RouteOccurrence — один ограниченный временем рейс. У одного tr_id может быть
// несколько паттернов и много рейсов в течение дня.
type RouteOccurrence struct {
	// ID однозначно идентифицирует вычисленный рейс с датой.
	ID string `json:"id"`
	// TRID идентифицирует сущность из файлов расписания и ML.
	TRID int64 `json:"trId"`
	// RoutePatternID выбирает статическую геометрию и порядок остановок.
	RoutePatternID string `json:"routePatternId"`
	// StartsAt — время первого планового прибытия в рейсе.
	StartsAt time.Time `json:"startsAt"`
	// EndsAt — время последнего планового прибытия в рейсе.
	EndsAt time.Time `json:"endsAt"`
	// Calls содержит события остановок в порядке движения.
	Calls []StopCall `json:"calls"`
}

// VehicleBinding разделяет протокольный и расписанийный идентификаторы, даже
// если в предоставленных данных они связаны один к одному.
type VehicleBinding struct {
	// UnitID — NPL.peerAddress или traffic.csv.unit_id конкретного ТС.
	UnitID uint32 `json:"unitId"`
	// TRID связывает телеметрию с расписанием и ML.
	TRID int64 `json:"trId"`
	// HasSchedule=false означает, что для ТС нет расписания и route match невозможен.
	HasSchedule bool `json:"hasSchedule"`
	// Synthetic отмечает сгенерированные train-примеры, которые live UI должен скрывать.
	Synthetic bool `json:"synthetic"`
}

// RiskThresholds позволяет frontend вычислять OK/WATCH/HIGH по задержке.
type RiskThresholds struct {
	// WatchDelaySeconds — включительная нижняя граница WATCH в секундах.
	WatchDelaySeconds float64 `json:"watchDelaySeconds"`
	// HighDelaySeconds — включительная нижняя граница HIGH в секундах.
	HighDelaySeconds float64 `json:"highDelaySeconds"`
}
