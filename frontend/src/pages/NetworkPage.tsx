import { useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { MapPlaceholder } from '../components/MapPlaceholder'
import { RiskBadge, TelemetryBadge } from '../components/StatusBadge'
import { useDashboard } from '../store'
import { formatDelay, formatTime } from '../utils'

export function NetworkPage() {
  const { state } = useDashboard()
  const navigate = useNavigate()
  const [queueOpen, setQueueOpen] = useState(true)
  const activeIncidents = useMemo(() => state.incidents.filter((item) => item.status !== 'closed').sort((a, b) => Number(b.risk === 'high') - Number(a.risk === 'high')), [state.incidents])
  const highCount = activeIncidents.filter((item) => item.risk === 'high').length
  const watchCount = activeIncidents.filter((item) => item.risk === 'watch').length
  const latest = activeIncidents[0]

  return <div className="page page--network">
    <header className="page-header"><div><p className="eyebrow">Оперативный контур</p><h1>Сеть / карта диспетчера</h1></div><div className="page-header__actions"><Link className="button-link" to="/incidents">Полная очередь</Link></div></header>
    <section className="ops-summary">
      <div><small>Транспорт</small><strong>{state.vehicles.filter((v) => v.telemetry === 'live').length} online / {state.vehicles.filter((v) => v.telemetry === 'stale').length} stale / {state.vehicles.filter((v) => v.telemetry === 'unmapped').length} unmapped</strong></div>
      <div><small>Риски</small><strong>{highCount} HIGH / {watchCount} WATCH</strong></div>
      <div><small>Ingestion / ML</small><strong>{state.system.ingestion} / {state.system.ml}</strong></div>
      <div className="ops-alert"><small>Последний значимый алерт</small>{latest ? <button onClick={() => navigate(`/?incident=${latest.id}`)}><RiskBadge risk={latest.risk} /> {latest.id} · маршрут {latest.routeId} / ТС {latest.vehicleId} · {formatDelay(latest.predictedDelaySeconds)} через {latest.horizonMinutes} мин.</button> : <strong>Нет активных алертов</strong>}</div>
    </section>
    <div className={`network-grid ${queueOpen ? '' : 'network-grid--collapsed'}`}>
      <MapPlaceholder />
      <aside className="attention-queue">
        <header><div><small>Операционные алерты</small><h2>Требуют внимания · {activeIncidents.length}</h2></div><button onClick={() => setQueueOpen(false)}>Свернуть</button></header>
        <div className="queue-list">{activeIncidents.map((incident) => {
          const vehicle = state.vehicles.find((item) => item.id === incident.vehicleId)
          return <button className="queue-item" key={incident.id} onClick={() => navigate(`/?incident=${incident.id}`)}><span><RiskBadge risk={incident.risk} /><b>{incident.id}</b><small>{formatTime(incident.updatedAt)}</small></span><strong>Маршрут {incident.routeId} · ТС {incident.vehicleId}</strong><span>{incident.currentStop} → {incident.targetStop}</span><span>Сейчас {formatDelay(incident.currentDelaySeconds)} · прогноз {formatDelay(incident.predictedDelaySeconds)} / {incident.horizonMinutes} мин.</span>{vehicle && <TelemetryBadge state={vehicle.telemetry} />}</button>
        })}{!activeIncidents.length && <div className="empty-state"><strong>Активных инцидентов нет</strong><span>Измените demo-сценарий в панели системы или сбросьте данные.</span></div>}</div>
        <section className="technical-alerts"><h3>Технические алерты</h3>{state.system.map !== 'ok' && <p>Карта: {state.system.map} — используется заглушка</p>}{state.vehicles.filter((v) => v.telemetry !== 'live').map((vehicle) => <button key={vehicle.id} onClick={() => navigate(`/?vehicle=${vehicle.id}`)}>ТС {vehicle.id}: {vehicle.telemetry}</button>)}</section>
      </aside>
      {!queueOpen && <button className="queue-restore" onClick={() => setQueueOpen(true)}>Показать очередь · {activeIncidents.length}</button>}
    </div>
  </div>
}
