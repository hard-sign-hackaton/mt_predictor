import { NavLink, Outlet } from 'react-router-dom'
import { useLiveMap } from '../live/liveMapContext'

export function AppShell() {
  const { catalog, connection, vehicles, incidents } = useLiveMap()
  return <div className="live-shell">
    <header className="live-header">
      <div><strong>MT Predictor</strong><nav className="live-nav"><NavLink to="/" end>Карта</NavLink><NavLink to="/incidents">Инциденты {incidents.length > 0 && <b>{incidents.length}</b>}</NavLink></nav></div>
      <div className="live-header__facts">
        <span>Источник: NDTP</span>
        <span>Каталог: {catalog?.catalogVersion ?? 'загрузка'}</span>
        <span>ТС принято: {vehicles.length}</span>
        <span className={`connection connection--${connection}`}>{connection}</span>
      </div>
    </header>
    <Outlet />
  </div>
}
