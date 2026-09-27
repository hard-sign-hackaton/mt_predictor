import { useMemo, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useDashboard } from '../store'
import type { RiskLevel } from '../types'
import { RiskBadge } from './StatusBadge'

interface MapPlaceholderProps {
  routeId?: string
  compact?: boolean
}

export function MapPlaceholder({ routeId, compact = false }: MapPlaceholderProps) {
  const { state } = useDashboard()
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const [zoom, setZoom] = useState(1)
  const [layersOpen, setLayersOpen] = useState(false)
  const [filtersOpen, setFiltersOpen] = useState(false)
  const [risk, setRisk] = useState<RiskLevel | 'all'>('all')
  const [showRoutes, setShowRoutes] = useState(true)
  const [showVehicles, setShowVehicles] = useState(true)
  const [showStops, setShowStops] = useState(true)

  const routes = useMemo(() => state.routes.filter((route) => (!routeId || route.id === routeId) && (risk === 'all' || route.risk === risk)), [routeId, risk, state.routes])
  const vehicles = state.vehicles.filter((vehicle) => (!routeId || vehicle.routeId === routeId) && (risk === 'all' || vehicle.risk === risk))

  const openEntity = (key: 'vehicle' | 'incident', value: string) => {
    const next = new URLSearchParams(params)
    next.set(key, value)
    navigate({ search: next.toString() })
  }

  return (
    <section className={`map ${compact ? 'map--compact' : ''}`} aria-label="Заглушка географической карты Москвы">
      <div className="map__toolbar">
        <button onClick={() => setFiltersOpen((value) => !value)}>Фильтры</button>
        <button onClick={() => setLayersOpen((value) => !value)}>Слои</button>
        <button onClick={() => setZoom(1)}>Центрировать</button>
      </div>
      {filtersOpen && <div className="map__popover">
        <strong>Риск</strong>
        {(['all', 'high', 'watch', 'normal'] as const).map((value) => <label key={value}><input type="radio" name="risk" checked={risk === value} onChange={() => setRisk(value)} /> {value === 'all' ? 'Все' : value.toUpperCase()}</label>)}
      </div>}
      {layersOpen && <div className="map__popover map__popover--right">
        <strong>Слои</strong>
        <label><input type="checkbox" checked={showRoutes} onChange={(event) => setShowRoutes(event.target.checked)} /> Маршруты</label>
        <label><input type="checkbox" checked={showStops} onChange={(event) => setShowStops(event.target.checked)} /> Остановки</label>
        <label><input type="checkbox" checked={showVehicles} onChange={(event) => setShowVehicles(event.target.checked)} /> Транспорт</label>
      </div>}
      <div className="map__zoom"><button onClick={() => setZoom((value) => Math.min(1.4, value + 0.1))}>+</button><span>{Math.round(zoom * 100)}%</span><button onClick={() => setZoom((value) => Math.max(0.8, value - 0.1))}>−</button></div>
      <div className="map__canvas" style={{ transform: `scale(${zoom})` }}>
        <svg className="map__base" viewBox="0 0 1000 650" preserveAspectRatio="none" aria-hidden="true">
          <path className="river" d="M-30 500 C150 430 260 540 450 480 S780 350 1040 430" />
          {[80, 180, 290, 410, 535].map((y) => <path key={`h-${y}`} className="street" d={`M0 ${y} C240 ${y - 50} 610 ${y + 55} 1000 ${y - 20}`} />)}
          {[120, 310, 520, 740, 900].map((x) => <path key={`v-${x}`} className="street street--minor" d={`M${x} 0 C${x - 90} 190 ${x + 90} 420 ${x - 30} 650`} />)}
          <text x="100" y="115">ХОРОШЁВСКИЙ РАЙОН</text><text x="610" y="150">СОКОЛ</text><text x="655" y="555">БЕГОВОЙ</text>
          {showRoutes && routes.map((route) => <polyline key={route.id} className={`route-line route-line--${route.risk}`} points={route.path.map((point) => `${point.x * 10},${point.y * 6.5}`).join(' ')} />)}
        </svg>
        {showStops && routes.flatMap((route) => route.path.slice(1, -1).map((position, index) => <button key={`${route.id}-${index}`} className="map-stop" style={{ left: `${position.x}%`, top: `${position.y}%` }} title={`Остановка маршрута ${route.id}`} />))}
        {showVehicles && vehicles.map((vehicle) => {
          const incident = state.incidents.find((item) => item.vehicleId === vehicle.id && item.status !== 'closed')
          return <button key={vehicle.id} className={`vehicle-marker vehicle-marker--${vehicle.risk}`} style={{ left: `${vehicle.position.x}%`, top: `${vehicle.position.y}%` }} onClick={() => openEntity('vehicle', vehicle.id)} title={`ТС ${vehicle.id}`}>
            {vehicle.routeId}<small>{vehicle.id}</small>
            {incident && <span className="vehicle-marker__incident" onClick={(event) => { event.stopPropagation(); openEntity('incident', incident.id) }}>!</span>}
          </button>
        })}
      </div>
      <div className="map__caption"><strong>Географическая заглушка Москвы</strong><span>Улицы, районы и координаты условные; подключение провайдера ожидает Q-05/Q-09.</span>{routeId && <RiskBadge risk={routes[0]?.risk ?? 'unknown'} />}</div>
    </section>
  )
}
