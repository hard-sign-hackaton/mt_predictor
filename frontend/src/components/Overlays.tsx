import { type FormEvent, useState } from 'react'
import { Link, useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { useDashboard } from '../store'
import type { IncidentStatus, OperatorAction, WhatIfResult, WhatIfScenario } from '../types'
import { formatDelay, formatTime, incidentStatusLabel, secondsAgo } from '../utils'
import { RiskBadge, TelemetryBadge } from './StatusBadge'

function useOverlayNavigation() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const location = useLocation()
  const closeKeys = (...keys: string[]) => {
    const next = new URLSearchParams(params)
    keys.forEach((key) => next.delete(key))
    navigate({ pathname: location.pathname, search: next.toString() })
  }
  const setOverlay = (values: Record<string, string | null>) => {
    const next = new URLSearchParams(params)
    Object.entries(values).forEach(([key, value]) => value === null ? next.delete(key) : next.set(key, value))
    navigate({ pathname: location.pathname, search: next.toString() })
  }
  return { closeKeys, setOverlay }
}

export function OverlayHost() {
  const [params] = useSearchParams()
  if (params.get('whatIf')) return <WhatIfPanel />
  if (params.get('action')) return <ActionModal />
  if (params.get('incident')) return <IncidentDrawer />
  if (params.get('vehicle')) return <VehicleDrawer />
  if (params.get('panel') === 'system') return <SystemDrawer />
  return null
}

function Drawer({ title, children, onClose, wide = false }: { title: string; children: React.ReactNode; onClose: () => void; wide?: boolean }) {
  return <div className="overlay-backdrop" onMouseDown={onClose}>
    <aside className={`drawer ${wide ? 'drawer--wide' : ''}`} onMouseDown={(event) => event.stopPropagation()} aria-label={title}>
      <header className="drawer__header"><h2>{title}</h2><button aria-label="Закрыть" onClick={onClose}>×</button></header>
      <div className="drawer__body">{children}</div>
    </aside>
  </div>
}

function IncidentDrawer() {
  const { state, dispatch } = useDashboard()
  const [params] = useSearchParams()
  const { closeKeys, setOverlay } = useOverlayNavigation()
  const incident = state.incidents.find((item) => item.id === params.get('incident'))
  const [status, setStatus] = useState<IncidentStatus>(incident?.status ?? 'new')
  const [owner, setOwner] = useState(incident?.owner ?? 'Диспетчер 01')
  const [comment, setComment] = useState(incident?.comment ?? '')
  if (!incident) return <Drawer title="Инцидент не найден" onClose={() => closeKeys('incident')}><p>Объект отсутствует в текущем mock-сценарии.</p></Drawer>
  const vehicle = state.vehicles.find((item) => item.id === incident.vehicleId)
  const actions = state.actions.filter((item) => item.incidentId === incident.id)
  const saveStatus = () => dispatch({ type: 'SET_INCIDENT_STATUS', incidentId: incident.id, status, owner, comment, now: new Date().toISOString() })
  return <Drawer title={`${incident.id} · ${incident.title}`} onClose={() => closeKeys('incident')} wide>
    <div className="drawer__lead"><RiskBadge risk={incident.risk} /><span>{incidentStatusLabel[incident.status]}</span><span>обновлён {formatTime(incident.updatedAt)}</span></div>
    <div className="metric-grid">
      <div><small>Маршрут / ТС</small><strong>{incident.routeId} / {incident.vehicleId}</strong></div>
      <div><small>Текущая задержка</small><strong>{formatDelay(incident.currentDelaySeconds)}</strong></div>
      <div><small>Прогноз через {incident.horizonMinutes} мин.</small><strong>{formatDelay(incident.predictedDelaySeconds)}</strong></div>
      <div><small>Уверенность</small><strong>{Math.round(incident.confidence * 100)}%</strong></div>
      <div><small>Текущая → целевая</small><strong>{incident.currentStop} → {incident.targetStop}</strong></div>
      <div><small>Модель</small><strong>{incident.modelVersion}</strong></div>
    </div>
    <section className="detail-section"><h3>Причина / паттерн</h3><p>{incident.reason}</p><small>Получено из mock-контракта ML/backend, UI не генерирует объяснение.</small></section>
    {vehicle && <section className="detail-section"><h3>Данные ТС</h3><p><TelemetryBadge state={vehicle.telemetry} /> Скорость {vehicle.speedKmh} км/ч · пакет {secondsAgo(vehicle.lastTelemetryAt)} сек. назад</p></section>}
    <section className="detail-section"><h3>Ведение инцидента</h3>
      <div className="form-row"><label>Статус<select value={status} onChange={(event) => setStatus(event.target.value as IncidentStatus)}><option value="new">Новый</option><option value="in_progress">В работе</option><option value="closed">Закрыт</option></select></label><label>Ответственный<input value={owner} onChange={(event) => setOwner(event.target.value)} /></label></div>
      <label>Комментарий<textarea value={comment} onChange={(event) => setComment(event.target.value)} placeholder="Контекст решения" /></label>
      <button className="button-primary" onClick={saveStatus}>Сохранить статус</button>
    </section>
    <section className="detail-section"><h3>Действия</h3><div className="action-row">
      <button onClick={() => setOverlay({ action: 'contact_driver' })}>Связаться с водителем</button>
      <button onClick={() => setOverlay({ action: 'adjust_stop' })}>Скорректировать остановку</button>
      <button onClick={() => setOverlay({ whatIf: 'setup' })}>What-if: резервное ТС</button>
      <Link className="button-link" to={`/routes/${incident.routeId}`}>Открыть маршрут</Link>
      <button onClick={() => setOverlay({ vehicle: incident.vehicleId, incident: null })}>Открыть ТС</button>
    </div></section>
    <section className="detail-section"><h3>История действий</h3>{actions.length ? actions.map((action) => <p key={action.id}><strong>{formatTime(action.createdAt)}</strong> · {action.summary} · {action.author}</p>) : <p className="empty-inline">Действий ещё нет.</p>}</section>
  </Drawer>
}

function VehicleDrawer() {
  const { state } = useDashboard()
  const [params] = useSearchParams()
  const { closeKeys, setOverlay } = useOverlayNavigation()
  const vehicle = state.vehicles.find((item) => item.id === params.get('vehicle'))
  if (!vehicle) return <Drawer title="ТС не найдено" onClose={() => closeKeys('vehicle')}><p>Объект отсутствует.</p></Drawer>
  const incident = state.incidents.find((item) => item.vehicleId === vehicle.id && item.status !== 'closed')
  return <Drawer title={`ТС ${vehicle.id} · ${vehicle.unitId}`} onClose={() => closeKeys('vehicle')}>
    <div className="drawer__lead"><RiskBadge risk={vehicle.risk} /><TelemetryBadge state={vehicle.telemetry} /></div>
    <dl className="definition-list"><dt>Маршрут / выход</dt><dd>{vehicle.routeId} / {vehicle.runCode}</dd><dt>Направление</dt><dd>{vehicle.direction}</dd><dt>Скорость / курс</dt><dd>{vehicle.speedKmh} км/ч / {vehicle.heading}°</dd><dt>Ближайшая остановка</dt><dd>{vehicle.nearestStop}</dd><dt>Целевая остановка</dt><dd>{vehicle.targetStop}</dd><dt>Задержка / прогноз</dt><dd>{formatDelay(vehicle.delaySeconds)} / {formatDelay(vehicle.predictedDelaySeconds)}</dd><dt>Телеметрия</dt><dd>{formatTime(vehicle.lastTelemetryAt)} · {secondsAgo(vehicle.lastTelemetryAt)} сек. назад</dd></dl>
    <section className="detail-section"><h3>Последние точки</h3>{vehicle.recentTelemetry.map((point) => <p key={point.at}>{formatTime(point.at)} · {point.speedKmh} км/ч · {point.note}</p>)}</section>
    <div className="action-row"><Link className="button-link" to={`/routes/${vehicle.routeId}`}>Открыть маршрут</Link>{incident && <button onClick={() => setOverlay({ incident: incident.id, vehicle: null })}>Инцидент {incident.id}</button>}<button onClick={() => closeKeys('vehicle')}>Фокус на карте</button></div>
  </Drawer>
}

function ActionModal() {
  const { state, dispatch } = useDashboard()
  const [params] = useSearchParams()
  const { closeKeys } = useOverlayNavigation()
  const incident = state.incidents.find((item) => item.id === params.get('incident'))
  const actionType = params.get('action') === 'adjust_stop' ? 'adjust_stop' : 'contact_driver'
  const [comment, setComment] = useState('')
  if (!incident) return null
  const submit = (event: FormEvent) => {
    event.preventDefault()
    const action: OperatorAction = { id: `ACT-${Date.now()}`, incidentId: incident.id, routeId: incident.routeId, vehicleId: incident.vehicleId, type: actionType, summary: actionType === 'contact_driver' ? `Связаться с водителем. ${comment}` : `Скорректировать стоянку. ${comment}`, author: 'Диспетчер 01', createdAt: new Date().toISOString(), status: 'mock', isMock: true }
    dispatch({ type: 'ADD_ACTION', action })
    closeKeys('action')
  }
  return <div className="modal-backdrop"><form className="modal" onSubmit={submit}><header><h2>{actionType === 'contact_driver' ? 'Связаться с водителем' : 'Корректировка остановки'}</h2><button type="button" onClick={() => closeKeys('action')}>×</button></header><p>Инцидент {incident.id} · ТС {incident.vehicleId}</p><div className="notice">DEMO MOCK — внешняя интеграция не вызывается.</div><label>Комментарий<textarea required value={comment} onChange={(event) => setComment(event.target.value)} placeholder="Что необходимо уточнить или изменить" /></label><p>Автор: Диспетчер 01 · время будет зарегистрировано автоматически</p><footer><button type="button" onClick={() => closeKeys('action')}>Отмена</button><button className="button-primary" type="submit">Зарегистрировать</button></footer></form></div>
}

function WhatIfPanel() {
  const { state, dispatch } = useDashboard()
  const [params] = useSearchParams()
  const { closeKeys, setOverlay } = useOverlayNavigation()
  const navigate = useNavigate()
  const incident = state.incidents.find((item) => item.id === params.get('incident'))
  const [scenario, setScenario] = useState<WhatIfScenario>({ incidentId: incident?.id ?? '', reserveVehicleId: 'reserve-03', releaseStop: 'Метро Сокол', readyInMinutes: 7 })
  const [result, setResult] = useState<WhatIfResult | null>(null)
  if (!incident) return null
  const calculate = (event: FormEvent) => {
    event.preventDefault()
    const improvement = Math.max(60, 240 - scenario.readyInMinutes * 12)
    setResult({ scenario, baselineDelaySeconds: incident.predictedDelaySeconds, expectedDelaySeconds: Math.max(60, incident.predictedDelaySeconds - improvement), baselineIntervalMinutes: 14, expectedIntervalMinutes: 9, affectedStopsBefore: 5, affectedStopsAfter: 2, effectRangeSeconds: [improvement - 40, improvement + 55], calculatedAt: new Date().toISOString() })
    setOverlay({ whatIf: 'result' })
  }
  const register = () => {
    if (!result) return
    dispatch({ type: 'REGISTER_WHAT_IF', result, action: { id: `ACT-${result.calculatedAt}`, incidentId: incident.id, routeId: incident.routeId, vehicleId: incident.vehicleId, type: 'reserve_vehicle', summary: `Рекомендация: выпустить ${scenario.reserveVehicleId} через ${scenario.readyInMinutes} мин. от ${scenario.releaseStop}`, author: 'Диспетчер 01', createdAt: result.calculatedAt, status: 'recommended', isMock: true } })
    navigate('/journal')
  }
  return <Drawer title={`What-if · ${incident.id}`} onClose={() => closeKeys('whatIf')} wide>
    <div className="notice">СЦЕНАРНАЯ ОЦЕНКА · эвристика, не прогноз модели · расписание автоматически не перестраивается.</div>
    <div className="metric-grid"><div><small>Базовый прогноз</small><strong>{formatDelay(incident.predictedDelaySeconds)}</strong></div><div><small>Горизонт</small><strong>{incident.horizonMinutes} минут</strong></div><div><small>Причина</small><strong>{incident.reason}</strong></div></div>
    {!result ? <form onSubmit={calculate} className="what-if-form"><label>Резервное ТС<select value={scenario.reserveVehicleId} onChange={(event) => setScenario({ ...scenario, reserveVehicleId: event.target.value })}><option value="reserve-03">reserve-03 · Парк №2</option><option value="reserve-07">reserve-07 · Парк №1</option></select></label><label>Точка выпуска<select value={scenario.releaseStop} onChange={(event) => setScenario({ ...scenario, releaseStop: event.target.value })}><option>Метро Сокол</option><option>Гидропроект</option><option>Панфилова</option></select></label><label>Готовность, минут<input type="number" min="1" max="30" value={scenario.readyInMinutes} onChange={(event) => setScenario({ ...scenario, readyInMinutes: Number(event.target.value) })} /></label><button className="button-primary">Рассчитать сценарий</button></form> : <section><h3>Результат</h3><table><thead><tr><th>Показатель</th><th>Без действия</th><th>После выпуска</th></tr></thead><tbody><tr><td>Ожидаемая задержка</td><td>{formatDelay(result.baselineDelaySeconds)}</td><td>{formatDelay(result.expectedDelaySeconds)}</td></tr><tr><td>Интервал</td><td>{result.baselineIntervalMinutes} мин.</td><td>{result.expectedIntervalMinutes} мин.</td></tr><tr><td>Затронутые остановки</td><td>{result.affectedStopsBefore}</td><td>{result.affectedStopsAfter}</td></tr></tbody></table><p>Оценочный эффект: сокращение задержки на {formatDelay(result.effectRangeSeconds[0])}…{formatDelay(result.effectRangeSeconds[1])}. Расчёт {formatTime(result.calculatedAt)}.</p><div className="action-row"><button onClick={() => { setResult(null); setOverlay({ whatIf: 'setup' }) }}>Изменить параметры</button><button className="button-primary" onClick={register}>В журнал как рекомендацию</button></div></section>}
  </Drawer>
}

function SystemDrawer() {
  const { state, dispatch } = useDashboard()
  const { closeKeys, setOverlay } = useOverlayNavigation()
  const problematic = state.vehicles.filter((item) => item.telemetry !== 'live')
  return <Drawer title="Состояние системы и данных" onClose={() => closeKeys('panel')}>
    <div className="service-grid">{(['ingestion', 'websocket', 'backend', 'ml', 'map'] as const).map((service) => <div key={service}><span>{service}</span><strong className={`service service--${state.system[service]}`}>{state.system[service]}</strong></div>)}</div>
    <dl className="definition-list"><dt>Последний пакет</dt><dd>{formatTime(state.system.lastPacketAt)}</dd><dt>Последний прогноз</dt><dd>{formatTime(state.system.lastPredictionAt)}</dd><dt>TTL</dt><dd>{state.system.ttlSeconds} сек.</dd><dt>Модель</dt><dd>{state.system.modelVersion}</dd><dt>Источник</dt><dd>{state.system.dataSource}</dd></dl>
    <section className="detail-section"><h3>Проверка состояний</h3><select defaultValue="normal" onChange={(event) => dispatch({ type: 'SET_SYSTEM_DEMO', value: event.target.value as 'normal' | 'degraded' | 'ml_unavailable' | 'map_unavailable' | 'empty' })}><option value="normal">Нормальная работа</option><option value="degraded">Reconnecting / degraded</option><option value="ml_unavailable">ML unavailable</option><option value="map_unavailable">Map unavailable</option><option value="empty">Empty data</option></select></section>
    <section className="detail-section"><h3>Проблемные ТС</h3>{problematic.map((vehicle) => <button className="list-button" key={vehicle.id} onClick={() => setOverlay({ vehicle: vehicle.id, panel: null })}>{vehicle.id} · {vehicle.telemetry}</button>)}</section>
  </Drawer>
}
