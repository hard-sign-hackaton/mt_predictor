import type { DashboardState, StopPrediction } from './types'

const base = '2026-09-26T09:'

const stops47: StopPrediction[] = [
  { stopId: 's1', stopName: 'Метро Сокол', plannedArrival: `${base}40:00+05:00`, predictedArrival: `${base}44:20+05:00`, deviationSeconds: 260 },
  { stopId: 's2', stopName: 'Гидропроект', plannedArrival: `${base}45:00+05:00`, predictedArrival: `${base}50:40+05:00`, deviationSeconds: 340 },
  { stopId: 's3', stopName: 'Панфилова', plannedArrival: `${base}50:00+05:00`, predictedArrival: `${base}57:10+05:00`, deviationSeconds: 430 },
  { stopId: 's4', stopName: 'Пехотная улица', plannedArrival: `${base}56:00+05:00`, predictedArrival: '2026-09-26T10:04:05+05:00', deviationSeconds: 485 },
  { stopId: 's5', stopName: 'Метро Щукинская', plannedArrival: '2026-09-26T10:02:00+05:00', predictedArrival: '2026-09-26T10:10:30+05:00', deviationSeconds: 510 },
]

export const initialState: DashboardState = {
  routes: [
    { id: '47', name: 'Маршрут 47', direction: 'Сокол → Щукинская', risk: 'high', activeVehicles: 12, affectedVehicles: 3, plannedIntervalMinutes: 8, actualIntervalMinutes: 14, stops: stops47, path: [{ x: 9, y: 74 }, { x: 24, y: 55 }, { x: 42, y: 50 }, { x: 63, y: 37 }, { x: 88, y: 20 }] },
    { id: '22', name: 'Маршрут 22', direction: 'Хорошёво → Белорусская', risk: 'watch', activeVehicles: 9, affectedVehicles: 1, plannedIntervalMinutes: 10, actualIntervalMinutes: 12, stops: stops47.map((s, i) => ({ ...s, stopId: `22-${i}`, deviationSeconds: 150 + i * 20 })), path: [{ x: 8, y: 20 }, { x: 27, y: 35 }, { x: 49, y: 34 }, { x: 72, y: 51 }, { x: 92, y: 67 }] },
    { id: '18', name: 'Маршрут 18', direction: 'Динамо → Тверская', risk: 'normal', activeVehicles: 8, affectedVehicles: 0, plannedIntervalMinutes: 9, actualIntervalMinutes: 9, stops: stops47.map((s, i) => ({ ...s, stopId: `18-${i}`, deviationSeconds: 30 + i * 10 })), path: [{ x: 12, y: 86 }, { x: 32, y: 69 }, { x: 55, y: 73 }, { x: 75, y: 62 }, { x: 91, y: 44 }] },
  ],
  vehicles: [
    { id: '4712', unitId: 'unit-8A21', routeId: '47', runCode: '47-08', direction: 'Сокол → Щукинская', speedKmh: 8, heading: 324, delaySeconds: 275, predictedDelaySeconds: 510, risk: 'high', telemetry: 'live', lastTelemetryAt: `${base}37:52+05:00`, nearestStop: 'Гидропроект', targetStop: 'Метро Щукинская', position: { x: 43, y: 49 }, recentTelemetry: [{ at: `${base}37:52+05:00`, speedKmh: 8, note: 'Движение' }, { at: `${base}37:42+05:00`, speedKmh: 0, note: 'Простой 36 сек.' }, { at: `${base}37:15+05:00`, speedKmh: 12, note: 'Движение' }] },
    { id: '4724', unitId: 'unit-9B02', routeId: '47', runCode: '47-11', direction: 'Сокол → Щукинская', speedKmh: 0, heading: 310, delaySeconds: 110, predictedDelaySeconds: 230, risk: 'watch', telemetry: 'stale', lastTelemetryAt: `${base}35:10+05:00`, nearestStop: 'Панфилова', targetStop: 'Пехотная улица', position: { x: 61, y: 38 }, recentTelemetry: [{ at: `${base}35:10+05:00`, speedKmh: 0, note: 'Последний пакет' }] },
    { id: '2241', unitId: 'unit-4C17', routeId: '22', runCode: '22-05', direction: 'Хорошёво → Белорусская', speedKmh: 17, heading: 77, delaySeconds: 95, predictedDelaySeconds: 185, risk: 'watch', telemetry: 'live', lastTelemetryAt: `${base}37:50+05:00`, nearestStop: 'Полежаевская', targetStop: 'Беговая', position: { x: 67, y: 49 }, recentTelemetry: [{ at: `${base}37:50+05:00`, speedKmh: 17, note: 'Движение' }] },
    { id: '1810', unitId: 'unit-1D07', routeId: '18', runCode: '18-03', direction: 'Динамо → Тверская', speedKmh: 24, heading: 92, delaySeconds: 20, predictedDelaySeconds: 35, risk: 'normal', telemetry: 'live', lastTelemetryAt: `${base}37:51+05:00`, nearestStop: 'Динамо', targetStop: 'Маяковская', position: { x: 54, y: 72 }, recentTelemetry: [{ at: `${base}37:51+05:00`, speedKmh: 24, note: 'Движение' }] },
    { id: 'reserve-03', unitId: 'unit-R03', routeId: '47', runCode: 'Резерв', direction: 'Парк', speedKmh: 0, heading: 0, delaySeconds: 0, predictedDelaySeconds: 0, risk: 'unknown', telemetry: 'unmapped', lastTelemetryAt: `${base}34:20+05:00`, nearestStop: 'Парк №2', targetStop: 'Не назначена', position: { x: 18, y: 82 }, recentTelemetry: [{ at: `${base}34:20+05:00`, speedKmh: 0, note: 'В резерве' }] },
  ],
  incidents: [
    { id: 'INC-204', routeId: '47', vehicleId: '4712', status: 'new', risk: 'high', title: 'Рост задержки перед Щукинской', currentStop: 'Гидропроект', targetStop: 'Метро Щукинская', currentDelaySeconds: 275, predictedDelaySeconds: 510, horizonMinutes: 15, confidence: 0.84, reason: 'Простой и снижение средней скорости на участке', modelVersion: 'mock-delay-v3', createdAt: `${base}32:10+05:00`, updatedAt: `${base}37:48+05:00`, owner: 'Не назначен', comment: '', repeatedHighCount: 3 },
    { id: 'INC-198', routeId: '22', vehicleId: '2241', status: 'in_progress', risk: 'watch', title: 'Растущий интервал', currentStop: 'Полежаевская', targetStop: 'Беговая', currentDelaySeconds: 95, predictedDelaySeconds: 185, horizonMinutes: 10, confidence: 0.71, reason: 'Интервал выше планового', modelVersion: 'mock-delay-v3', createdAt: `${base}21:00+05:00`, updatedAt: `${base}36:12+05:00`, owner: 'Диспетчер 01', comment: 'Наблюдаем два расчёта', repeatedHighCount: 0 },
    { id: 'INC-176', routeId: '47', vehicleId: '4724', status: 'closed', risk: 'unknown', title: 'Нет свежей телеметрии', currentStop: 'Панфилова', targetStop: 'Пехотная улица', currentDelaySeconds: 110, predictedDelaySeconds: 230, horizonMinutes: 10, confidence: 0.55, reason: 'Телеметрия старше TTL', modelVersion: 'mock-delay-v3', createdAt: `${base}10:00+05:00`, updatedAt: `${base}35:10+05:00`, owner: 'Диспетчер 02', comment: 'Передано технической смене', repeatedHighCount: 0 },
  ],
  actions: [
    { id: 'ACT-101', incidentId: 'INC-198', routeId: '22', vehicleId: '2241', type: 'contact_driver', summary: 'Связаться с водителем, уточнить дорожную ситуацию', author: 'Диспетчер 01', createdAt: `${base}34:00+05:00`, status: 'completed', isMock: true },
  ],
  system: { snapshotAt: `${base}37:55+05:00`, ingestion: 'ok', websocket: 'ok', backend: 'ok', ml: 'ok', map: 'degraded', lastPacketAt: `${base}37:52+05:00`, lastPredictionAt: `${base}37:48+05:00`, ttlSeconds: 90, dataSource: 'Mock NDTP stream', modelVersion: 'mock-delay-v3' },
  liveEvents: [{ type: 'prediction', at: `${base}37:48+05:00`, entityId: 'INC-204', summary: 'Прогноз повышен до +8:30' }],
  simulationPaused: false,
  tick: 0,
}

export function cloneInitialState(): DashboardState {
  return structuredClone(initialState)
}
