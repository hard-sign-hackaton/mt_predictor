import { Outlet } from 'react-router-dom'
import { useLiveMap } from '../live/liveMapContext'

export function AppShell() {
  const { catalog, connection, vehicles } = useLiveMap()
  return <div className="live-shell">
    <header className="live-header">
      <div><strong>MT Predictor</strong><span>Диспетчерская карта</span></div>
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
