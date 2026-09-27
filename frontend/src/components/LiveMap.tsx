import { useMemo } from 'react'
import L, { type LatLngExpression } from 'leaflet'
import { CircleMarker, MapContainer, Marker, Polyline, Popup, TileLayer, Tooltip } from 'react-leaflet'
import type { DashboardInit, RoutePattern, VehicleState } from '../api/types'
import { riskFromScheduleDeviation } from '../api/risk'
import { matchStatusLabel, routeDirection, routeDisplayName, stopDisplayName, vehicleName, vehicleRouteName } from '../domain/vehiclePresentation'
import { MapCameraController } from './MapCameraController'

interface LiveMapProps {
  catalog: DashboardInit
  vehicles: VehicleState[]
  selectedRouteId?: string
  selectedVehicleId?: number
  showRoutes: boolean
  showStops: boolean
  showVehicles: boolean
  onSelectRoute: (routeId: string) => void
  onSelectVehicle: (unitId: number) => void
}

const routeStyles = {
  gps_repeated: { color: '#222', dashArray: undefined },
  gps_single: { color: '#5d5d5d', dashArray: '8 6' },
  stops_only: { color: '#8a6b16', dashArray: '4 7' },
} as const

function routePositions(route: RoutePattern): LatLngExpression[] {
  return route.polyline.map((point) => [point.lat, point.lon])
}

const htmlEntities: Record<string, string> = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  "'": '&#39;',
  '"': '&quot;',
}

function escapeHTML(value: string) {
  return value.replace(/[&<>'"]/g, (symbol) => htmlEntities[symbol])
}

function vehicleIcon(vehicle: VehicleState, risk: string, selected: boolean) {
  const label = escapeHTML(vehicleName(vehicle))
  const width = Math.max(62, label.length * 7 + 16)
  return L.divIcon({
    className: 'live-vehicle-icon-wrap',
    html: `<div class="live-vehicle-icon live-vehicle-icon--${risk} ${selected ? 'live-vehicle-icon--selected' : ''}" style="--heading:${vehicle.headingDegrees}deg;width:${width}px"><span>▲</span><b>${label}</b></div>`,
    iconSize: [width, 36],
    iconAnchor: [width / 2, 18],
  })
}

export function LiveMap(props: LiveMapProps) {
  const stopsById = useMemo(() => new Map(props.catalog.stops.map((stop) => [stop.id, stop])), [props.catalog.stops])
  const routesById = useMemo(() => new Map(props.catalog.routes.map((route) => [route.id, route])), [props.catalog.routes])
  const visibleStops = useMemo(() => {
    if (!props.selectedRouteId) return props.catalog.stops
    const route = props.catalog.routes.find((item) => item.id === props.selectedRouteId)
    return route?.stopIds.map((id) => stopsById.get(id)).filter((stop) => stop !== undefined) ?? []
  }, [props.catalog.routes, props.catalog.stops, props.selectedRouteId, stopsById])

  return <MapContainer className="live-map" center={[55.751, 37.618]} zoom={10} minZoom={9} scrollWheelZoom preferCanvas attributionControl={false}>
    <TileLayer
      attribution='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>'
      url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png"
    />
    <MapCameraController catalog={props.catalog} vehicles={props.vehicles} selectedRouteId={props.selectedRouteId} selectedVehicleId={props.selectedVehicleId} />

    {props.showRoutes && props.catalog.routes.map((route) => {
      const selected = route.id === props.selectedRouteId
      const style = routeStyles[route.geometryQuality]
      return <Polyline
        key={route.id}
        positions={routePositions(route)}
        pathOptions={{ color: selected ? '#000' : style.color, weight: selected ? 6 : 3, opacity: selected ? 1 : 0.56, dashArray: style.dashArray }}
        eventHandlers={{ click: () => props.onSelectRoute(route.id) }}
      ><Tooltip sticky>{routeDisplayName(route)}<br />{routeDirection(route)}</Tooltip></Polyline>
    })}

    {props.showStops && visibleStops.map((stop) => <CircleMarker
      key={stop.id}
      center={[stop.position.lat, stop.position.lon]}
      radius={props.selectedRouteId ? 4 : 2}
      pathOptions={{ color: '#111', weight: 1, fillColor: '#fff', fillOpacity: 1 }}
    ><Tooltip>{stopDisplayName(stop)}</Tooltip></CircleMarker>)}

    {props.showVehicles && props.vehicles.filter((vehicle) => vehicle.position).map((vehicle) => {
      const routeLabel = vehicleRouteName(vehicle, routesById)
      const risk = riskFromScheduleDeviation(vehicle.currentDelaySeconds, props.catalog.riskThresholds)
      return <Marker
        key={vehicle.unitId}
        position={[vehicle.position!.lat, vehicle.position!.lon]}
        icon={vehicleIcon(vehicle, risk, vehicle.unitId === props.selectedVehicleId)}
        eventHandlers={{ click: () => props.onSelectVehicle(vehicle.unitId) }}
      ><Popup>
        <strong>{routeLabel}</strong><br />
        скорость: {Math.round(vehicle.speedKmh)} км/ч<br />
        статус: {matchStatusLabel(vehicle.matchStatus)}<br />
        следующая остановка: {stopDisplayName(vehicle.nextStop)}
      </Popup></Marker>
    })}
  </MapContainer>
}
