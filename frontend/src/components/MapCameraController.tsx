import { useEffect, useRef } from 'react'
import { latLngBounds, type LatLngExpression } from 'leaflet'
import { useMap } from 'react-leaflet'
import type { DashboardInit, RoutePattern, VehicleState } from '../api/types'

interface MapCameraControllerProps {
  catalog: DashboardInit
  vehicles: VehicleState[]
  selectedRouteId?: string
  selectedVehicleId?: number
}

function routePositions(route: RoutePattern): LatLngExpression[] {
  return route.polyline.map((point) => [point.lat, point.lon])
}

// Камера реагирует только на команды пользователя: выбор маршрута или ТС.
// Потоковые координаты обновляют маркеры, но не перезапускают fitBounds/flyTo.
export function MapCameraController({ catalog, vehicles, selectedRouteId, selectedVehicleId }: MapCameraControllerProps) {
  const map = useMap()
  const latestVehicles = useRef(vehicles)

  useEffect(() => {
    latestVehicles.current = vehicles
  }, [vehicles])

  useEffect(() => {
    const route = catalog.routes.find((item) => item.id === selectedRouteId)
    const points = route ? routePositions(route) : catalog.routes.flatMap(routePositions)
    if (points.length > 1) {
      map.fitBounds(latLngBounds(points), { padding: [24, 24], maxZoom: route ? 14 : 11 })
    }
  }, [catalog.routes, map, selectedRouteId])

  useEffect(() => {
    if (selectedVehicleId === undefined) return
    const selectedVehicle = latestVehicles.current.find((vehicle) => vehicle.unitId === selectedVehicleId)
    if (selectedVehicle?.position) {
      map.flyTo([selectedVehicle.position.lat, selectedVehicle.position.lon], Math.max(map.getZoom(), 14))
    }
  }, [map, selectedVehicleId])

  return null
}
