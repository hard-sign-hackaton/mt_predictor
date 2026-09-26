import { NavLink, Outlet, useNavigate, useSearchParams } from 'react-router-dom'
import { useDashboard } from '../store'
import { formatTime } from '../utils'
import { OverlayHost } from './Overlays'

export function AppShell() {
  const { state, dispatch } = useDashboard()
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const degraded = Object.values(state.system).includes('unavailable') || state.system.ingestion !== 'ok' || state.system.ml !== 'ok'

  return (
    <div className="app-shell">
      <header className="topbar">
        <div className="brand">MT Predictor <span>Диспетчерская</span></div>
        <button className={`mode ${degraded ? 'mode--degraded' : ''}`} onClick={() => navigate({ search: '?panel=system' })}>
          {degraded ? 'DEGRADED' : 'LIVE'} · снимок {formatTime(state.system.snapshotAt)}
        </button>
        <div className="topbar__stats">
          <span>{state.vehicles.filter((v) => v.telemetry === 'live').length} online</span>
          <span>{state.vehicles.filter((v) => v.telemetry === 'stale').length} stale</span>
          <span>{state.vehicles.filter((v) => v.telemetry === 'unmapped').length} unmapped</span>
        </div>
        <button onClick={() => dispatch({ type: 'TOGGLE_SIMULATION' })}>{state.simulationPaused ? '▶ Продолжить live' : 'Ⅱ Пауза live'}</button>
        <button onClick={() => dispatch({ type: 'RESET' })}>Сбросить демо</button>
      </header>
      <div className="app-body">
        <nav className="sidebar" aria-label="Основная навигация">
          <NavLink end to="/">Сеть</NavLink>
          <NavLink to="/routes/47">Маршруты</NavLink>
          <NavLink to="/incidents">Инциденты <b>{state.incidents.filter((item) => item.status !== 'closed').length}</b></NavLink>
          <NavLink to="/journal">Журнал</NavLink>
          <button className="sidebar__system" onClick={() => navigate({ search: '?panel=system' })}>Система</button>
        </nav>
        <main className="content"><Outlet /></main>
      </div>
      <footer className="footer">
        <span><i className="legend-dot legend-dot--normal" /> OK</span>
        <span><i className="legend-dot legend-dot--watch" /> WATCH</span>
        <span><i className="legend-dot legend-dot--high" /> HIGH</span>
        <span>Источник: {state.system.dataSource}</span>
        <span>Mock UI · без подключения к backend</span>
      </footer>
      {(params.get('incident') || params.get('vehicle') || params.get('panel') || params.get('action') || params.get('whatIf')) && <OverlayHost />}
    </div>
  )
}
