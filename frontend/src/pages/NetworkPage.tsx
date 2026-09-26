import { useMemo, useState } from 'react'
import { LiveMap } from '../components/LiveMap'
import { useLiveMap } from '../live/liveMapContext'
import type { GeometryQuality, VehicleState } from '../api/types'
import {
  filterVehicles,
  isOnRoute,
  matchStatusLabel,
  vehicleName,
  type DelayVehicleFilter,
  type RouteVehicleFilter,
} from '../domain/vehiclePresentation'

function freshness(vehicle: VehicleState): 'live' | 'stale' {
  return Date.now() - new Date(vehicle.receivedAt).getTime() > 30_000 ? 'stale' : vehicle.freshness
}

function formatTime(value: string) {
  return new Intl.DateTimeFormat('ru-RU', { hour: '2-digit', minute: '2-digit', second: '2-digit' }).format(new Date(value))
}

function formatDelay(value?: number) {
  if (value === undefined) return 'Нет данных'
  const minutes = Math.round(Math.abs(value) / 60)
  if (value > 0) return `Задержка ${minutes} мин`
  if (value < 0) return `Опережение ${minutes} мин`
  return 'По расписанию'
}

const qualityLabel: Record<GeometryQuality, string> = {
  gps_repeated: 'GPS: несколько проходов',
  gps_single: 'GPS: один проход',
  stops_only: 'Только остановки',
}

export function NetworkPage() {
  const { catalog, vehicles, version, connection, error } = useLiveMap()
  const [selectedRouteId, setSelectedRouteId] = useState<string>()
  const [selectedVehicleId, setSelectedVehicleId] = useState<number>()
  const [search, setSearch] = useState('')
  const [showRoutes, setShowRoutes] = useState(true)
  const [showStops, setShowStops] = useState(true)
  const [showVehicles, setShowVehicles] = useState(true)
  const [routeFilter, setRouteFilter] = useState<RouteVehicleFilter>('all')
  const [delayFilter, setDelayFilter] = useState<DelayVehicleFilter>('all')

  const filteredRoutes = useMemo(() => catalog?.routes.filter((route) => route.id.toLowerCase().includes(search.toLowerCase())) ?? [], [catalog, search])
  const visibleVehicles = useMemo(() => filterVehicles(vehicles, routeFilter, delayFilter), [delayFilter, routeFilter, vehicles])
  const selectedVehicle = visibleVehicles.find((vehicle) => vehicle.unitId === selectedVehicleId)
  const mappedCount = visibleVehicles.filter(isOnRoute).length
  const liveCount = visibleVehicles.filter((vehicle) => freshness(vehicle) === 'live').length
  const knownDelayCount = vehicles.filter((vehicle) => vehicle.currentDelaySeconds !== undefined).length

  if (!catalog) return <div className="map-loading"><strong>Загрузка каталога карты…</strong><span>{error ?? 'Ожидание backend'}</span></div>

  return <div className="map-workspace">
    <aside className="map-sidebar">
      <section className="control-section">
        <h2>Слои</h2>
        <label><input type="checkbox" checked={showRoutes} onChange={(event) => setShowRoutes(event.target.checked)} /> Маршруты · {catalog.routes.length}</label>
        <label><input type="checkbox" checked={showStops} onChange={(event) => setShowStops(event.target.checked)} /> Остановки · {catalog.stops.length}</label>
        <label><input type="checkbox" checked={showVehicles} onChange={(event) => setShowVehicles(event.target.checked)} /> Транспорт · {visibleVehicles.length}/{vehicles.length}</label>
      </section>

      <section className="control-section vehicle-filters">
        <div className="section-heading"><h2>Фильтр транспорта</h2>{(routeFilter !== 'all' || delayFilter !== 'all') && <button onClick={() => { setRouteFilter('all'); setDelayFilter('all'); setSelectedVehicleId(undefined) }}>Сбросить</button>}</div>
        <label>Положение
          <select aria-label="Положение относительно маршрута" value={routeFilter} onChange={(event) => { setRouteFilter(event.target.value as RouteVehicleFilter); setSelectedVehicleId(undefined) }}>
            <option value="all">Все ТС</option>
            <option value="on_route">Идут по маршруту</option>
            <option value="not_on_route">Не на маршруте</option>
          </select>
        </label>
        <label>Задержка
          <select aria-label="Наличие задержки" value={delayFilter} onChange={(event) => { setDelayFilter(event.target.value as DelayVehicleFilter); setSelectedVehicleId(undefined) }}>
            <option value="all">Все</option>
            <option value="delayed">С задержкой</option>
            <option value="on_time">Без задержки</option>
          </select>
        </label>
        <small className={knownDelayCount ? 'data-note' : 'data-note data-note--missing'}>
          {knownDelayCount ? `Задержка известна для ${knownDelayCount} из ${vehicles.length} ТС` : 'Данные задержки пока не поступают'}
        </small>
      </section>

      <section className="control-section control-section--routes">
        <div className="section-heading"><h2>Паттерны маршрутов</h2>{selectedRouteId && <button onClick={() => setSelectedRouteId(undefined)}>Все</button>}</div>
        <input aria-label="Поиск маршрута" placeholder="Поиск по ID паттерна" value={search} onChange={(event) => setSearch(event.target.value)} />
        <div className="route-list">
          {filteredRoutes.map((route) => <button className={route.id === selectedRouteId ? 'route-row route-row--selected' : 'route-row'} key={route.id} onClick={() => setSelectedRouteId(route.id)}>
            <strong>{route.id.replace('route_pattern_', 'pattern ')}</strong>
            <span>{route.stopIds.length} остановок</span>
            <small>{qualityLabel[route.geometryQuality]}</small>
          </button>)}
        </div>
      </section>
    </aside>

    <main className="map-stage">
      <div className="map-stage__status">
        <span className={`connection connection--${connection}`}>{connection === 'live' ? 'LIVE' : connection.toUpperCase()}</span>
        <span>событие #{version}</span>
        <span>{liveCount} online</span>
        <span>{visibleVehicles.length} показано</span>
        <span>{mappedCount} на маршруте</span>
        {error && <span title={error}>есть ошибка соединения</span>}
      </div>
      <LiveMap
        catalog={catalog}
        vehicles={visibleVehicles}
        selectedRouteId={selectedRouteId}
        selectedVehicleId={selectedVehicleId}
        showRoutes={showRoutes}
        showStops={showStops}
        showVehicles={showVehicles}
        onSelectRoute={(routeId) => { setSelectedRouteId(routeId); setSelectedVehicleId(undefined) }}
        onSelectVehicle={setSelectedVehicleId}
      />
      <div className="map-legend">
        <span><i className="line-sample line-sample--strong" /> несколько GPS-проходов</span>
        <span><i className="line-sample line-sample--single" /> один GPS-проход</span>
        <span><i className="line-sample line-sample--stops" /> только остановки</span>
        <span>Подложка <a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noreferrer">© OpenStreetMap</a></span>
      </div>
    </main>

    <aside className="vehicle-panel">
      <header><h2>Транспорт</h2><span>{visibleVehicles.length}/{vehicles.length}</span></header>
      {selectedVehicle && <section className="vehicle-detail">
        <div className="section-heading"><h3>{vehicleName(selectedVehicle)}</h3><button onClick={() => setSelectedVehicleId(undefined)}>×</button></div>
        <dl>
          <dt>unit_id</dt><dd>{selectedVehicle.unitId}</dd>
          <dt>tr_id</dt><dd>{selectedVehicle.trId ?? 'нет mapping'}</dd>
          <dt>Скорость</dt><dd>{Math.round(selectedVehicle.speedKmh)} км/ч</dd>
          <dt>Курс</dt><dd>{Math.round(selectedVehicle.headingDegrees)}°</dd>
          <dt>Статус</dt><dd>{matchStatusLabel(selectedVehicle.matchStatus)}</dd>
          <dt>Расписание</dt><dd>{formatDelay(selectedVehicle.currentDelaySeconds)}</dd>
          <dt>До маршрута</dt><dd>{selectedVehicle.distanceToRouteMeters !== undefined ? `${Math.round(selectedVehicle.distanceToRouteMeters)} м` : '—'}</dd>
          <dt>Предыдущая</dt><dd>{selectedVehicle.previousStop?.address || selectedVehicle.previousStop?.id || '—'}</dd>
          <dt>Следующая</dt><dd>{selectedVehicle.nextStop?.address || selectedVehicle.nextStop?.id || '—'}</dd>
          <dt>Пакет NDTP</dt><dd>{formatTime(selectedVehicle.eventTime)}</dd>
        </dl>
      </section>}
      <div className="vehicle-list">
        {visibleVehicles.map((vehicle) => <button key={vehicle.unitId} className={vehicle.unitId === selectedVehicleId ? 'vehicle-row vehicle-row--selected' : 'vehicle-row'} onClick={() => setSelectedVehicleId(vehicle.unitId)}>
          <span className={`freshness freshness--${freshness(vehicle)}`} />
          <strong>{vehicleName(vehicle)}</strong>
          <span>{Math.round(vehicle.speedKmh)} км/ч</span>
          <small>{matchStatusLabel(vehicle.matchStatus)} · {formatDelay(vehicle.currentDelaySeconds)}</small>
        </button>)}
        {!vehicles.length && <div className="empty-live"><strong>Пакетов ещё нет</strong><span>Запустите NDTP-эмулятор или replay feeder.</span></div>}
        {vehicles.length > 0 && !visibleVehicles.length && <div className="empty-live"><strong>Нет ТС по фильтру</strong><span>{delayFilter !== 'all' && !knownDelayCount ? 'Backend пока не передаёт задержку.' : 'Измените или сбросьте условия.'}</span></div>}
      </div>
    </aside>
  </div>
}
