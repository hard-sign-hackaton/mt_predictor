export type RiskLevel = 'normal' | 'watch' | 'high' | 'unknown'
export type TelemetryState = 'live' | 'stale' | 'unmapped' | 'invalid_location'
export type IncidentStatus = 'new' | 'in_progress' | 'closed'
export type ActionStatus = 'recommended' | 'registered' | 'completed' | 'cancelled' | 'mock'
export type ServiceState = 'ok' | 'degraded' | 'unavailable'

export interface Position {
  x: number
  y: number
}

export interface StopPrediction {
  stopId: string
  stopName: string
  plannedArrival: string
  predictedArrival: string
  deviationSeconds: number
}

export interface Route {
  id: string
  name: string
  direction: string
  risk: RiskLevel
  activeVehicles: number
  affectedVehicles: number
  plannedIntervalMinutes: number
  actualIntervalMinutes: number
  stops: StopPrediction[]
  path: Position[]
}

export interface Vehicle {
  id: string
  unitId: string
  routeId: string
  runCode: string
  direction: string
  speedKmh: number
  heading: number
  delaySeconds: number
  predictedDelaySeconds: number
  risk: RiskLevel
  telemetry: TelemetryState
  lastTelemetryAt: string
  nearestStop: string
  targetStop: string
  position: Position
  recentTelemetry: Array<{ at: string; speedKmh: number; note: string }>
}

export interface Incident {
  id: string
  routeId: string
  vehicleId: string
  status: IncidentStatus
  risk: RiskLevel
  title: string
  currentStop: string
  targetStop: string
  currentDelaySeconds: number
  predictedDelaySeconds: number
  horizonMinutes: number
  confidence: number
  reason: string
  modelVersion: string
  createdAt: string
  updatedAt: string
  owner: string
  comment: string
  repeatedHighCount: number
}

export interface OperatorAction {
  id: string
  incidentId: string
  routeId: string
  vehicleId: string
  type: 'contact_driver' | 'adjust_stop' | 'reserve_vehicle' | 'status_change'
  summary: string
  author: string
  createdAt: string
  status: ActionStatus
  isMock: boolean
}

export interface WhatIfScenario {
  incidentId: string
  reserveVehicleId: string
  releaseStop: string
  readyInMinutes: number
}

export interface WhatIfResult {
  scenario: WhatIfScenario
  baselineDelaySeconds: number
  expectedDelaySeconds: number
  baselineIntervalMinutes: number
  expectedIntervalMinutes: number
  affectedStopsBefore: number
  affectedStopsAfter: number
  effectRangeSeconds: [number, number]
  calculatedAt: string
}

export interface SystemSnapshot {
  snapshotAt: string
  ingestion: ServiceState
  websocket: ServiceState
  backend: ServiceState
  ml: ServiceState
  map: ServiceState
  lastPacketAt: string
  lastPredictionAt: string
  ttlSeconds: number
  dataSource: string
  modelVersion: string
}

export interface LiveEvent {
  type: 'telemetry' | 'prediction' | 'system'
  at: string
  entityId: string
  summary: string
}

export interface DashboardState {
  routes: Route[]
  vehicles: Vehicle[]
  incidents: Incident[]
  actions: OperatorAction[]
  system: SystemSnapshot
  liveEvents: LiveEvent[]
  simulationPaused: boolean
  tick: number
}
