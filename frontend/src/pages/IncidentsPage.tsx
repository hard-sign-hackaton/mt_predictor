import { useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { riskFromDelaySeconds, type RiskLevel } from '../api/risk'
import type { Incident, RoutePattern } from '../api/types'
import { IncidentCard } from '../components/IncidentCard'
import { routeDisplayName, routeDirection, stopDisplayName } from '../domain/vehiclePresentation'
import { useLiveMap } from '../live/liveMapContext'
import { formatIncidentDate, formatIncidentDelay, incidentReason } from '../domain/incidentPresentation'

type RiskFilter = 'all' | RiskLevel
const riskLabel: Record<RiskLevel, string> = { normal: 'Низкая', watch: 'Средняя · WATCH', high: 'Высокая · HIGH' }
const incidentRiskDelay = (incident: Incident) => Math.max(incident.predictedDelaySeconds, incident.currentDelaySeconds ?? incident.predictedDelaySeconds)

function IncidentRows({ incidents, routes, open }: { incidents: Incident[], routes: Map<string, RoutePattern>, open: (id: string) => void }) {
  const { catalog } = useLiveMap(); if (!catalog) return null
  if (!incidents.length) return <div className="incidents-empty"><strong>Нет инцидентов</strong></div>
  return <div className="incidents-table-wrap"><table className="incidents-table"><thead><tr><th>Критичность</th><th>Транспорт</th><th>Маршрут</th><th>Остановка</th><th>Возможная причина</th><th>Прогноз</th><th>План</th><th>Обновлено</th></tr></thead><tbody>{incidents.map((incident) => {
    const risk = riskFromDelaySeconds(incidentRiskDelay(incident), catalog.riskThresholds); const route = routes.get(incident.routePatternId)
    return <tr key={incident.id} tabIndex={0} onClick={() => open(incident.id)} onKeyDown={(event) => { if (event.key === 'Enter') open(incident.id) }}><td><span className={`incident-risk incident-risk--${risk}`}>{riskLabel[risk]}</span></td><td><strong>ТС {incident.unitId}</strong></td><td><strong>{routeDisplayName(route)}</strong><small>{routeDirection(route)}</small></td><td>{stopDisplayName(incident.targetStop)}</td><td>{incidentReason(incident.reason)}</td><td className="incident-delay">{formatIncidentDelay(incident.predictedDelaySeconds)}{incident.currentDelaySeconds !== undefined ? <small>Сейчас: {formatIncidentDelay(incident.currentDelaySeconds)}</small> : null}</td><td>{formatIncidentDate(incident.targetPlannedAt)}</td><td>{formatIncidentDate(incident.updatedAt)}</td></tr>
  })}</tbody></table></div>
}

export function IncidentsPage() {
  const { catalog, incidents, connection } = useLiveMap(); const navigate = useNavigate(); const { incidentId } = useParams()
  const [query, setQuery] = useState(''); const [riskFilter, setRiskFilter] = useState<RiskFilter>('all')
  const routes = useMemo(() => new Map(catalog?.routes.map((route) => [route.id, route]) ?? []), [catalog])
  const filtered = useMemo(() => incidents.filter((incident) => {
    const route = routes.get(incident.routePatternId); const text = `${incident.unitId} ${routeDisplayName(route)} ${routeDirection(route)} ${stopDisplayName(incident.targetStop)} ${incident.reason ?? ''}`.toLocaleLowerCase('ru-RU')
    return text.includes(query.trim().toLocaleLowerCase('ru-RU')) && (!catalog || riskFilter === 'all' || riskFromDelaySeconds(incidentRiskDelay(incident), catalog.riskThresholds) === riskFilter)
  }), [catalog, incidents, query, riskFilter, routes])
  if (!catalog) return <div className="incidents-loading">Загрузка данных инцидентов…</div>
  const active = filtered.filter((item) => item.status === 'active'); const awaiting = filtered.filter((item) => item.status === 'awaiting_result')
  return <main className="incidents-page"><header className="incidents-heading"><div><h1>Инциденты прогнозируемых задержек</h1><p>Текущие прогнозы и риски, ожидающие фактического прибытия.</p></div><div><strong>{incidents.length}</strong><span>незавершённых</span><small>{connection === 'live' ? 'LIVE' : connection}</small></div></header>
    <section className="incidents-filters"><input aria-label="Поиск инцидентов" value={query} onChange={(e) => setQuery(e.target.value)} placeholder="ТС, маршрут или остановка" /><label>Критичность<select value={riskFilter} onChange={(e) => setRiskFilter(e.target.value as RiskFilter)}><option value="all">Все</option><option value="normal">Низкая</option><option value="watch">Средняя</option><option value="high">Высокая</option></select></label></section>
    <section className="incident-group"><h2>Текущие <span>{active.length}</span></h2><IncidentRows incidents={active} routes={routes} open={(id) => navigate(`/incidents/${id}`)} /></section>
    <section className="incident-group"><h2>Ожидают результата <span>{awaiting.length}</span></h2><p>Прогнозное окно прошло; результат появится после прохождения остановки.</p><IncidentRows incidents={awaiting} routes={routes} open={(id) => navigate(`/incidents/${id}`)} /></section>
    {incidentId ? <IncidentCard incidentId={incidentId} fallback={incidents.find((item) => item.id === incidentId)} onClose={() => navigate('/incidents')} /> : null}
  </main>
}
