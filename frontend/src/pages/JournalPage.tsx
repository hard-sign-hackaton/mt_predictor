import { useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useDashboard } from '../store'
import type { ActionStatus } from '../types'
import { actionStatusLabel, formatTime } from '../utils'

export function JournalPage() {
  const { state, dispatch } = useDashboard()
  const navigate = useNavigate()
  const [route, setRoute] = useState('all')
  const [status, setStatus] = useState<ActionStatus | 'all'>('all')
  const filtered = useMemo(() => state.actions.filter((action) => (route === 'all' || action.routeId === route) && (status === 'all' || action.status === status)), [route, state.actions, status])
  return <div className="page"><header className="page-header"><div><p className="eyebrow">Аудит решений</p><h1>Журнал действий</h1></div><Link className="button-link" to="/incidents">← К инцидентам</Link></header><section className="filterbar"><label>Маршрут<select value={route} onChange={(event) => setRoute(event.target.value)}><option value="all">Все</option>{state.routes.map((item) => <option key={item.id}>{item.id}</option>)}</select></label><label>Статус<select value={status} onChange={(event) => setStatus(event.target.value as ActionStatus | 'all')}><option value="all">Все</option>{Object.entries(actionStatusLabel).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label></section><section className="panel table-panel"><table><thead><tr><th>Время</th><th>Инцидент</th><th>Маршрут / ТС</th><th>Действие</th><th>Автор</th><th>Режим</th><th>Статус</th></tr></thead><tbody>{filtered.map((action) => <tr key={action.id}><td>{formatTime(action.createdAt)}</td><td><button className="link-button" onClick={() => navigate(`/journal?incident=${action.incidentId}`)}>{action.incidentId}</button></td><td><Link to={`/routes/${action.routeId}`}>{action.routeId}</Link> / {action.vehicleId}</td><td>{action.summary}</td><td>{action.author}</td><td>{action.isMock ? 'DEMO MOCK' : 'Реальное'}</td><td><select aria-label={`Статус ${action.id}`} value={action.status} onChange={(event) => dispatch({ type: 'UPDATE_ACTION_STATUS', actionId: action.id, status: event.target.value as ActionStatus })}>{Object.entries(actionStatusLabel).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></td></tr>)}</tbody></table>{!filtered.length && <div className="empty-state"><strong>Журнал пуст</strong><span>Зарегистрируйте действие из карточки инцидента.</span></div>}</section></div>
}
