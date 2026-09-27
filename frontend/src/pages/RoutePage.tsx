import { useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { MapPlaceholder } from '../components/MapPlaceholder'
import { RiskBadge, TelemetryBadge } from '../components/StatusBadge'
import { useDashboard } from '../store'
import { formatDelay, formatTime } from '../utils'

export function RoutePage() {
  const { routeId = '47' } = useParams()
  const { state } = useDashboard()
  const navigate = useNavigate()
  const [view, setView] = useState<'map' | 'line' | 'forecast'>('map')
  const [sort, setSort] = useState<'risk' | 'delay'>('risk')
  const route = state.routes.find((item) => item.id === routeId)
  const vehicles = useMemo(() => state.vehicles.filter((item) => item.routeId === routeId).sort((a, b) => sort === 'delay' ? b.predictedDelaySeconds - a.predictedDelaySeconds : Number(b.risk === 'high') - Number(a.risk === 'high')), [routeId, sort, state.vehicles])
  if (!route) return <div className="page"><h1>Маршрут не найден</h1><Link to="/">Вернуться к сети</Link></div>
  return <div className="page">
    <header className="page-header"><div><p className="eyebrow">Маршрут / оперативная детализация</p><h1>{route.name} · {route.direction}</h1></div><Link className="button-link" to={`/?route=${route.id}`}>← К сети</Link></header>
    <section className="ops-summary"><div><small>Риск</small><strong><RiskBadge risk={route.risk} /></strong></div><div><small>На линии / проблемных</small><strong>{route.activeVehicles} / {route.affectedVehicles}</strong></div><div><small>Интервал план / факт</small><strong>{route.plannedIntervalMinutes} / {route.actualIntervalMinutes} мин.</strong></div><div className="ops-alert"><small>Проблемный участок</small><strong>Гидропроект → Панфилова · средняя скорость 9 км/ч · простой</strong></div></section>
    <div className="view-switch"><button className={view === 'map' ? 'active' : ''} onClick={() => setView('map')}>Карта</button><button className={view === 'line' ? 'active' : ''} onClick={() => setView('line')}>Линейная схема</button><button className={view === 'forecast' ? 'active' : ''} onClick={() => setView('forecast')}>Текущая → прогноз</button></div>
    {view === 'map' && <MapPlaceholder routeId={route.id} compact />}
    {view === 'line' && <section className="line-view">{route.stops.map((stop, index) => <div key={stop.stopId}><i /><strong>{stop.stopName}</strong><span>{index < vehicles.length ? `ТС ${vehicles[index].id}` : ''}</span></div>)}</section>}
    {view === 'forecast' && <section className="forecast-view"><div className="forecast-axis">Сейчас <span>граница прогноза</span> +15 минут</div>{vehicles.map((vehicle) => <button key={vehicle.id} onClick={() => navigate(`/routes/${routeId}?vehicle=${vehicle.id}`)}><b>ТС {vehicle.id}</b><span className="forecast-current" style={{ width: `${Math.min(45, vehicle.delaySeconds / 12)}%` }}>{formatDelay(vehicle.delaySeconds)}</span><span className={`forecast-prediction forecast-prediction--${vehicle.risk}`} style={{ width: `${Math.min(48, vehicle.predictedDelaySeconds / 12)}%` }}>{formatDelay(vehicle.predictedDelaySeconds)}</span></button>)}</section>}
    <section className="panel"><header><div><small>Транспорт маршрута</small><h2>ТС на линии</h2></div><label>Сортировка <select value={sort} onChange={(event) => setSort(event.target.value as 'risk' | 'delay')}><option value="risk">По риску</option><option value="delay">По прогнозу</option></select></label></header><div className="table-scroll"><table><thead><tr><th>ТС</th><th>Данные</th><th>Сейчас</th><th>Прогноз 10–15 мин.</th><th>Риск</th><th>Остановка</th><th>Скорость</th><th>Пакет</th></tr></thead><tbody>{vehicles.map((vehicle) => <tr key={vehicle.id} onClick={() => navigate(`/routes/${routeId}?vehicle=${vehicle.id}`)}><td><b>{vehicle.id}</b><br /><small>{vehicle.unitId}</small></td><td><TelemetryBadge state={vehicle.telemetry} /></td><td>{formatDelay(vehicle.delaySeconds)}</td><td>{formatDelay(vehicle.predictedDelaySeconds)}</td><td><RiskBadge risk={vehicle.risk} /></td><td>{vehicle.nearestStop}<br /><small>цель: {vehicle.targetStop}</small></td><td>{vehicle.speedKmh} км/ч</td><td>{formatTime(vehicle.lastTelemetryAt)}</td></tr>)}</tbody></table></div></section>
    <section className="panel"><header><div><small>Ближайшие точки контроля</small><h2>Прогноз прибытия по остановкам</h2></div></header><table><thead><tr><th>Остановка</th><th>План</th><th>Прогноз</th><th>Отклонение</th></tr></thead><tbody>{route.stops.map((stop) => <tr key={stop.stopId}><td>{stop.stopName}</td><td>{formatTime(stop.plannedArrival)}</td><td>{formatTime(stop.predictedArrival)}</td><td>{formatDelay(stop.deviationSeconds)}</td></tr>)}</tbody></table></section>
  </div>
}
