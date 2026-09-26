import { useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { RiskBadge } from '../components/StatusBadge'
import { useDashboard } from '../store'
import type { IncidentStatus } from '../types'
import { formatDelay, formatTime, incidentStatusLabel } from '../utils'

export function IncidentsPage() {
  const { state } = useDashboard()
  const navigate = useNavigate()
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState<IncidentStatus | 'all'>('all')
  const [group, setGroup] = useState<'none' | 'route' | 'risk'>('none')
  const filtered = useMemo(() => state.incidents.filter((incident) => (status === 'all' || incident.status === status) && `${incident.id} ${incident.routeId} ${incident.vehicleId} ${incident.reason}`.toLowerCase().includes(query.toLowerCase())).sort((a, b) => group === 'route' ? a.routeId.localeCompare(b.routeId) : group === 'risk' ? Number(b.risk === 'high') - Number(a.risk === 'high') : b.updatedAt.localeCompare(a.updatedAt)), [group, query, state.incidents, status])
  return <div className="page"><header className="page-header"><div><p className="eyebrow">Контроль ситуаций</p><h1>Инциденты</h1></div><Link className="button-link" to="/journal">Журнал действий →</Link></header>
    <section className="filterbar"><input aria-label="Поиск инцидентов" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Поиск: ID / маршрут / ТС / причина" /><div className="tabs">{(['all', 'new', 'in_progress', 'closed'] as const).map((value) => <button className={status === value ? 'active' : ''} key={value} onClick={() => setStatus(value)}>{value === 'all' ? 'Все' : incidentStatusLabel[value]}</button>)}</div><label>Группировка<select value={group} onChange={(event) => setGroup(event.target.value as typeof group)}><option value="none">Нет</option><option value="route">Маршрут</option><option value="risk">Риск</option></select></label></section>
    <section className="panel table-panel"><table><thead><tr><th>ID / статус</th><th>Приоритет</th><th>Маршрут / ТС</th><th>Прогноз</th><th>Цель</th><th>Причина</th><th>Ответственный</th><th>Обновлён</th></tr></thead><tbody>{filtered.map((incident) => <tr key={incident.id} onClick={() => navigate(`/incidents?incident=${incident.id}`)}><td><b>{incident.id}</b><br /><small>{incidentStatusLabel[incident.status]}</small></td><td><RiskBadge risk={incident.risk} />{incident.repeatedHighCount > 1 && <small className="repeat-flag">×{incident.repeatedHighCount} HIGH</small>}</td><td><Link onClick={(event) => event.stopPropagation()} to={`/routes/${incident.routeId}`}>{incident.routeId}</Link> / {incident.vehicleId}</td><td>{formatDelay(incident.predictedDelaySeconds)}<br /><small>через {incident.horizonMinutes} мин. · {Math.round(incident.confidence * 100)}%</small></td><td>{incident.targetStop}</td><td>{incident.reason}</td><td>{incident.owner}</td><td>{formatTime(incident.updatedAt)}</td></tr>)}</tbody></table>{!filtered.length && <div className="empty-state"><strong>Ничего не найдено</strong><span>Измените поиск или фильтр состояния.</span></div>}</section>
  </div>
}
