// Модели обмена для согласованного MVP карты и минимальных инцидентов. Типы
// повторяют backend/models и намеренно не включают health, действия диспетчера,
// операторский workflow инцидентов и What-if.

export interface GeoPoint {
  /** Долгота WGS84 в диапазоне -180..180. */
  lon: number
  /** Широта WGS84 в диапазоне -90..90. */
  lat: number
}

export type GeometryQuality = 'gps_repeated' | 'gps_single' | 'stops_only'

export interface Stop {
  /** Стабильный ID, вычисленный из округлённых координат. */
  id: string
  /** Координата отдельного маркера остановки. */
  position: GeoPoint
  /** Исходный читаемый адрес; может быть пустым. */
  address: string
}

export interface FrequentGeoPoint {
  /** Медианная координата повторяющейся GPS-ячейки. */
  position: GeoPoint
  /** Число различных проходов через ячейку. */
  occurrenceCount: number
}

export interface RouteGeometryQuality {
  /** Число рейсов расписания, отнесённых к паттерну. */
  occurrenceCount: number
  /** Число рейсов с пригодным GPS-покрытием. */
  goodGpsOccurrenceCount: number
  /** Лучшее покрытие остановок в диапазоне от 0 до 1. */
  bestStopCoverage: number
  /** Медианное расстояние остановок до трека; отсутствует для stops_only. */
  medianStopToGpsMeters?: number
  /** Максимальный разрыв между соседними точками выбранного GPS-прохода. */
  maxGpsJumpMeters: number
}

export interface RoutePattern {
  /** ID вычисленного паттерна, не публичный номер маршрута. */
  id: string
  /** Публичный ID маршрута после подключения внешнего справочника. */
  officialRouteId?: string
  /** Человекочитаемое направление из открытого GTFS-справочника. */
  officialRouteName?: string
  /** confirmed — уверенное сопоставление, probable — наиболее вероятное. */
  officialMatchQuality?: 'confirmed' | 'probable'
  /** Канонический порядок остановок одного круга. */
  stopIds: string[]
  /** Линия маршрута из GPS либо fallback только по остановкам. */
  polyline: GeoPoint[]
  /** Повторяющиеся опорные точки для диагностики. */
  frequentPoints: FrequentGeoPoint[]
  /** Степень достоверности отображаемой линии. */
  geometryQuality: GeometryQuality
  /** Числовые показатели, обосновывающие geometryQuality. */
  quality: RouteGeometryQuality
}

export interface StopCall {
  /** Идентификатор остановочного события расписания/ML. */
  actionItemId: number
  /** ID физической остановки из DashboardInit.stops. */
  stopId: string
  /** Плановое прибытие в формате ISO timestamp. */
  plannedAt: string
}

export interface RouteOccurrence {
  /** Уникальный ID рейса с датой. */
  id: string
  /** ID сущности расписания/ML; не публичный номер маршрута. */
  trId: number
  /** Вычисленная линия и канонический порядок остановок. */
  routePatternId: string
  /** Начало окна расписания в формате ISO timestamp. */
  startsAt: string
  /** Конец окна расписания в формате ISO timestamp. */
  endsAt: string
  /** Плановые события остановок в порядке движения. */
  calls: StopCall[]
}

export interface VehicleBinding {
  /** Идентификатор конкретного ТС из NDTP. */
  unitId: number
  /** ID, связывающий телеметрию с расписанием и ML. */
  trId: number
  /** Есть ли расписание для этой связи в переданном каталоге. */
  hasSchedule: boolean
  /** Сгенерированная train-строка, которую нужно скрывать в live UI. */
  synthetic: boolean
}

export interface RiskThresholds {
  /** Включительная граница WATCH в секундах задержки. */
  watchDelaySeconds: number
  /** Включительная граница HIGH в секундах задержки. */
  highDelaySeconds: number
}

export interface DashboardInit {
  /** Версия схемы статических данных. */
  schemaVersion: string
  /** Меняется при изменении вычисленных маршрутов или расписаний. */
  catalogVersion: string
  stops: Stop[]
  routes: RoutePattern[]
  occurrences: RouteOccurrence[]
  vehicleBindings: VehicleBinding[]
  /** Пороги backend, по которым frontend вычисляет критичность. */
  riskThresholds: RiskThresholds
}

export type TelemetryFreshness = 'live' | 'stale'
export type MatchStatus =
  | 'matched'
  | 'matched_spatial'
  | 'unmapped_unit'
  | 'invalid_location'
  | 'no_schedule'
  | 'no_active_pattern'
  | 'off_route'

export interface StopReference {
  /** ID физической остановки из DashboardInit.stops. */
  id: string
  /** Продублированный адрес для отображения; может быть пустым. */
  address: string
}

export interface VehicleState {
  /** ID конкретного ТС и его маркера. */
  unitId: number
  /** ID расписания/ML; отсутствует для несопоставленного unit. */
  trId?: number
  /** Текущая координата WGS84; отсутствует при невалидной геопозиции. */
  position?: GeoPoint
  /** Средняя скорость из декодированного навигационного пакета. */
  speedKmh: number
  /** Курс из декодированного пакета, 0..360 градусов. */
  headingDegrees: number
  /** Время события устройства, используемое для упорядочивания. */
  eventTime: string
  /** Время приёма backend, используемое для диагностики. */
  receivedAt: string
  /** Является ли пакет запоздалой/исторической записью. */
  historical: boolean
  freshness: TelemetryFreshness
  matchStatus: MatchStatus
  routePatternId?: string
  occurrenceId?: string
  previousStop?: StopReference
  nextStop?: StopReference
  nextActionItemId?: number
  distanceToRouteMeters?: number
  geometryQuality?: GeometryQuality
  /** Текущая задержка; отсутствие поля означает, что backend её не рассчитал. */
  currentDelaySeconds?: number
}

export interface DelayPrediction {
  /** Стабильный идентификатор прогноза/sample. */
  id: string
  unitId: number
  trId: number
  routePatternId: string
  occurrenceId: string
  targetActionItemId: number
  targetStop: StopReference
  /** Момент расчёта T; входы модели обязаны иметь event_time <= T. */
  predictionTime: string
  /** Плановое прибытие на целевую остановку в горизонте прогноза. */
  targetPlannedAt: string
  currentDelaySeconds?: number
  /** Положительное значение — опоздание, отрицательное — опережение. */
  predictedDelaySeconds: number
  /** Необязательно, пока ML фактически не передаёт значение. */
  confidence?: number
  /** Необязательное объяснение backend/ML; frontend не должен его придумывать. */
  reason?: string
  reasonCode?: string
  evidence?: Record<string, number>
  scenarioId?: string
  /** Необязательно, пока ML фактически не передаёт значение. */
  modelVersion?: string
}

export type IncidentEventType = 'new' | 'updated' | 'closed'
export type IncidentStatus = 'active' | 'awaiting_result' | 'resolved' | 'cancelled'
export type IncidentOutcome = 'occurred' | 'not_occurred'

export interface Incident {
  /** Стабильный ID всех событий одного непрерывного инцидента. */
  id: string
  /** Команда добавить, заменить или удалить элемент активной очереди. */
  eventType: IncidentEventType
  status: IncidentStatus
  unitId: number
  trId: number
  routePatternId: string
  occurrenceId: string
  predictionId: string
  targetActionItemId: number
  targetStop: StopReference
  /** Входное значение для расчёта критичности на frontend. */
  currentDelaySeconds?: number
  predictedDelaySeconds: number
  predictionTime: string
  targetPlannedAt: string
  firstPredictedDelaySeconds: number
  actualArrivalAt?: string
  actualDelaySeconds?: number
  outcome?: IncidentOutcome
  reasonCode?: string
  reason?: string
  evidence?: Record<string, number>
  scenarioId?: string
  createdAt: string
  updatedAt: string
}

export interface IncidentHistoryPage {
  items: Incident[]
  total: number
  limit: number
  offset: number
}

export interface DashboardSnapshot {
  /** Монотонная позиция live-потока, представленная этим snapshot. */
  version: number
  createdAt: string
  vehicles: VehicleState[]
  predictions: DelayPrediction[]
  incidents: Incident[]
}

export interface MapSnapshot {
  /** Позиция потока событий, которой соответствует снимок. */
  version: number
  /** Время формирования снимка на backend. */
  createdAt: string
  /** Последнее известное состояние каждого принятого ТС. */
  vehicles: VehicleState[]
}

export type LiveEvent =
  | (LiveEventEnvelope<'vehicle_updated'> & { vehicle: VehicleState })
  | (LiveEventEnvelope<'prediction_updated'> & { prediction: DelayPrediction })
  | (LiveEventEnvelope<'incident_updated'> & { incident: Incident })

interface LiveEventEnvelope<TType extends string> {
  /** Глобально уникальный ID события для дедупликации. */
  eventId: string
  /** Монотонная последовательность для поиска пропусков и запроса snapshot. */
  sequence: number
  type: TType
  occurredAt: string
}
